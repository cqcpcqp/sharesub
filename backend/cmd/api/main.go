package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/config"
	"github.com/sharesub/sharesub/backend/internal/httpapi"
	"github.com/sharesub/sharesub/backend/internal/openai"
	"github.com/sharesub/sharesub/backend/internal/operations"
	"github.com/sharesub/sharesub/backend/internal/postgres"
	"github.com/sharesub/sharesub/backend/internal/security"
	"github.com/sharesub/sharesub/backend/internal/tencentmail"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		logger.Error("apply migrations", "error", err)
		os.Exit(1)
	}
	securityManager, err := security.New(cfg.TokenPepper, cfg.CredentialKey)
	if err != nil {
		logger.Error("initialize security", "error", err)
		os.Exit(1)
	}
	proxyFunc := http.ProxyFromEnvironment
	if cfg.OutboundProxy != "" {
		proxyURL, err := url.Parse(cfg.OutboundProxy)
		if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			logger.Error("parse SHARESUB_OUTBOUND_PROXY", "error", err)
			os.Exit(1)
		}
		proxyFunc = http.ProxyURL(proxyURL)
	}
	httpClient := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 proxyFunc,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second,
		},
	}
	oauthClient := openai.NewOAuthClient(cfg.OutboundProxy)
	gateway := openai.NewGateway(httpClient)
	defer gateway.Close()
	runtimeMonitor := operations.NewMonitor(store)
	runtimeMonitor.RegisterJob("codex_version_sync", "Codex 版本同步", true)
	runtimeMonitor.RegisterJob("resource_cleanup", "资源清理", true)
	runtimeMonitor.RegisterJob("token_refresh", "Token 自动刷新", cfg.TokenRefreshEnabled)
	go runtimeMonitor.RunCPUSampler(ctx)
	go openai.RunCodexVersionSync(ctx, httpClient, 6*time.Hour, func(duration time.Duration, syncErr error) {
		if syncErr != nil {
			runtimeMonitor.RecordJobFailure("codex_version_sync", syncErr, duration)
			logger.Warn("sync official Codex version", "error", syncErr, "effective_version", openai.EffectiveCodexVersion())
			return
		}
		runtimeMonitor.RecordJobSuccess("codex_version_sync", "当前版本 "+openai.EffectiveCodexVersion(), duration)
	})
	var emailSender application.EmailVerificationSender
	if cfg.EmailDeliveryProvider == "tencent_ses" {
		emailSender, err = tencentmail.New(tencentmail.Config{
			SecretID: cfg.TencentSESSecretID, SecretKey: cfg.TencentSESSecretKey, Region: cfg.TencentSESRegion,
			FromEmail: cfg.TencentSESFromEmail, FromName: cfg.TencentSESFromName, TemplateID: cfg.TencentSESTemplateID,
		})
		if err != nil {
			logger.Error("initialize Tencent SES email sender", "error", err)
			os.Exit(1)
		}
	}
	app := application.NewServiceWithEmailVerification(store, securityManager, oauthClient, cfg.SessionTTL, cfg.OAuthRedirect, cfg.PublicURL, emailSender, cfg.EmailVerificationTTL, cfg.EmailResendCooldown, gateway)
	app.SetRuntimeStatusProvider(runtimeMonitor)
	bootstrapAdmin, err := app.EnsureBootstrapAdmin(ctx)
	if err != nil {
		logger.Error("bootstrap admin", "error", err)
		os.Exit(1)
	}
	if bootstrapAdmin != nil {
		logger.Warn("bootstrap admin created; change this temporary password after login", "email", bootstrapAdmin.Email, "temporary_password", bootstrapAdmin.TemporaryPassword)
	}
	api := httpapi.New(app, gateway, logger, httpapi.ResponsesWebSocketConfig{
		FirstMessageTimeout:           cfg.ResponsesWSFirstMessageTimeout,
		InterTurnIdleTimeout:          cfg.ResponsesWSInterTurnIdleTimeout,
		MaxSessionDuration:            cfg.ResponsesWSMaxSessionDuration,
		MaxConnectionsPerAPIKey:       cfg.ResponsesWSMaxConnectionsPerAPIKey,
		OutboundProxyURL:              cfg.OutboundProxy,
		DialTimeout:                   cfg.ResponsesWSDialTimeout,
		ReadTimeout:                   cfg.ResponsesWSReadTimeout,
		WriteTimeout:                  cfg.ResponsesWSWriteTimeout,
		UpstreamDrainTimeout:          cfg.ResponsesWSUpstreamDrainTimeout,
		ClientReadLimitBytes:          cfg.ResponsesWSClientReadLimitBytes,
		UpstreamReadLimitBytes:        cfg.ResponsesWSUpstreamReadLimitBytes,
		ReplayMemoryLimitBytes:        cfg.ResponsesWSReplayMemoryLimitBytes,
		MaxRequestsPerMinutePerAPIKey: cfg.GatewayMaxRequestsPerMinutePerAPIKey,
		FirstOutputTimeout:            cfg.GatewayFirstOutputTimeout,
	})
	defer api.Close()
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: api.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10,
	}
	retention := postgres.RetentionPolicy{
		GatewayMetrics: cfg.GatewayMetricRetention,
		AuditEvents:    cfg.AuditEventRetention, ReadNotifications: cfg.ReadNotificationRetention,
		TerminalRecords: cfg.TerminalRecordRetention,
	}
	go runResourceCleanup(ctx, store, retention, cfg.CleanupInterval, logger, runtimeMonitor)
	if cfg.TokenRefreshEnabled {
		go runTokenRefresh(ctx, app, cfg, logger, runtimeMonitor)
	}

	go func() {
		logger.Info("ShareSub API listening", "address", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve API", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := api.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown Responses WebSocket sessions", "error", err)
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown API", "error", err)
	}
}

func runTokenRefresh(ctx context.Context, app *application.Service, cfg config.Config, logger *slog.Logger, monitor *operations.Monitor) {
	refresh := func() {
		startedAt := time.Now()
		result, err := app.RefreshExpiringAccountTokens(ctx, cfg.TokenRefreshBeforeExpiry, cfg.TokenRefreshBatchSize, cfg.TokenRefreshConcurrency, cfg.TokenRefreshMaxRetries)
		if err != nil {
			monitor.RecordJobFailure("token_refresh", err, time.Since(startedAt))
			if ctx.Err() == nil {
				logger.Error("refresh OpenAI account tokens", "error", err)
			}
			return
		}
		resultSummary := fmt.Sprintf("扫描 %d，刷新 %d，失败 %d", result.Scanned, result.Refreshed, result.Failed)
		if result.Failed > 0 {
			monitor.RecordJobWarning("token_refresh", fmt.Sprintf("%d 个账号刷新失败", result.Failed), resultSummary, time.Since(startedAt))
		} else {
			monitor.RecordJobSuccess("token_refresh", resultSummary, time.Since(startedAt))
		}
		logger.Info("OpenAI account token refresh completed", "scanned", result.Scanned, "refreshed", result.Refreshed, "skipped", result.Skipped, "failed", result.Failed)
	}
	refresh()
	ticker := time.NewTicker(cfg.TokenRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func runResourceCleanup(ctx context.Context, store *postgres.Store, policy postgres.RetentionPolicy, interval time.Duration, logger *slog.Logger, monitor *operations.Monitor) {
	cleanup := func() {
		startedAt := time.Now()
		result, err := store.CleanupResources(ctx, time.Now(), policy)
		if err != nil {
			monitor.RecordJobFailure("resource_cleanup", err, time.Since(startedAt))
			if ctx.Err() == nil {
				logger.Error("clean expired resources", "error", err)
			}
			return
		}
		monitor.RecordJobSuccess("resource_cleanup", fmt.Sprintf("网关指标 %d，会话 %d", result.GatewayMetrics, result.Sessions), time.Since(startedAt))
		logger.Info("resource cleanup completed", "gateway_metrics", result.GatewayMetrics, "audit_events", result.AuditEvents, "read_notifications", result.ReadNotifications, "sessions", result.Sessions, "oauth_flows", result.OAuthFlows, "invites", result.Invites, "applications", result.Applications, "api_keys", result.APIKeys)
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
