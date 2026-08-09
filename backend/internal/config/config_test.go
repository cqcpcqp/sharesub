package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("SHARESUB_DATABASE_URL", "postgres://example")
	t.Setenv("SHARESUB_TOKEN_PEPPER", key)
	t.Setenv("SHARESUB_CREDENTIAL_KEY", key)
}

func TestLoadResourceDefaults(t *testing.T) {
	setRequiredEnvironment(t)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.CleanupInterval != 6*time.Hour || config.GatewayMetricRetention != 90*24*time.Hour ||
		config.AuditEventRetention != 365*24*time.Hour ||
		!config.TokenRefreshEnabled || config.TokenRefreshInterval != 5*time.Minute ||
		config.TokenRefreshBeforeExpiry != 30*time.Minute || config.TokenRefreshBatchSize != 200 ||
		config.TokenRefreshConcurrency != 4 || config.TokenRefreshMaxRetries != 3 {
		t.Fatalf("resource defaults = %+v", config)
	}
}

func TestLoadRejectsInvalidResourceLimits(t *testing.T) {
	for name, value := range map[string]string{
		"SHARESUB_CLEANUP_INTERVAL":          "never",
		"SHARESUB_GATEWAY_METRIC_RETENTION":  "167h59m59s",
		"SHARESUB_TOKEN_REFRESH_INTERVAL":    "never",
		"SHARESUB_TOKEN_REFRESH_CONCURRENCY": "0",
		"SHARESUB_TOKEN_REFRESH_ENABLED":     "sometimes",
	} {
		t.Run(name, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv(name, value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}
