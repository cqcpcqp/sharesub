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
	api := httpapi.New(app, openai.NewGateway(httpClient), logger)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}

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
