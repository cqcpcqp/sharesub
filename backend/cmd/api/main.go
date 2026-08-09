package main

import (
	"context"
	"errors"
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
	"github.com/sharesub/sharesub/backend/internal/postgres"
	"github.com/sharesub/sharesub/backend/internal/security"
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
	app := application.NewService(store, securityManager, oauthClient, cfg.SessionTTL, cfg.OAuthRedirect, cfg.PublicURL)
	bootstrapAdmin, err := app.EnsureBootstrapAdmin(ctx)
	if err != nil {
		logger.Error("bootstrap admin", "error", err)
		os.Exit(1)
	}
	if bootstrapAdmin != nil {
		logger.Warn("bootstrap admin created; change this temporary password after login", "email", bootstrapAdmin.Email, "temporary_password", bootstrapAdmin.TemporaryPassword)
	}
	gateway := openai.NewGateway(httpClient)
	defer gateway.Close()
	api := httpapi.New(app, gateway, logger)
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: api.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10,
	}
	retention := postgres.RetentionPolicy{
		GatewayMetrics: cfg.GatewayMetricRetention, QuotaEvents: cfg.QuotaEventRetention,
		AuditEvents: cfg.AuditEventRetention, ReadNotifications: cfg.ReadNotificationRetention,
		TerminalRecords: cfg.TerminalRecordRetention,
	}
	go runResourceCleanup(ctx, store, retention, cfg.CleanupInterval, logger)
	if cfg.TokenRefreshEnabled {
		go runTokenRefresh(ctx, app, cfg, logger)
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
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown API", "error", err)
	}
}

func runTokenRefresh(ctx context.Context, app *application.Service, cfg config.Config, logger *slog.Logger) {
	refresh := func() {
		result, err := app.RefreshExpiringAccountTokens(ctx, cfg.TokenRefreshBeforeExpiry, cfg.TokenRefreshBatchSize, cfg.TokenRefreshConcurrency, cfg.TokenRefreshMaxRetries)
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("refresh OpenAI account tokens", "error", err)
			}
			return
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

func runResourceCleanup(ctx context.Context, store *postgres.Store, policy postgres.RetentionPolicy, interval time.Duration, logger *slog.Logger) {
	cleanup := func() {
		result, err := store.CleanupResources(ctx, time.Now(), policy)
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("clean expired resources", "error", err)
			}
			return
		}
		logger.Info("resource cleanup completed", "gateway_metrics", result.GatewayMetrics, "quota_events", result.QuotaEvents, "audit_events", result.AuditEvents, "read_notifications", result.ReadNotifications, "sessions", result.Sessions, "oauth_flows", result.OAuthFlows, "invites", result.Invites, "applications", result.Applications, "api_keys", result.APIKeys)
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
