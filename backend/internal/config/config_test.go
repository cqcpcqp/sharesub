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
		config.AuditEventRetention != 365*24*time.Hour || config.GatewayMaxConcurrency != 8 {
		t.Fatalf("resource defaults = %+v", config)
	}
}

func TestLoadRejectsInvalidResourceLimits(t *testing.T) {
	for name, value := range map[string]string{
		"SHARESUB_CLEANUP_INTERVAL":         "never",
		"SHARESUB_GATEWAY_METRIC_RETENTION": "167h59m59s",
		"SHARESUB_GATEWAY_MAX_CONCURRENCY":  "0",
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
