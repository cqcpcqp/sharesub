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
	if config.GatewayMaxRequestsPerMinutePerAPIKey != 300 || config.GatewayFirstOutputTimeout != 2*time.Minute {
		t.Fatalf("gateway protection defaults = %+v", config)
	}
}

func TestLoadResponsesWSDefaults(t *testing.T) {
	setRequiredEnvironment(t)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.ResponsesWSFirstMessageTimeout != 30*time.Second ||
		config.ResponsesWSInterTurnIdleTimeout != 5*time.Minute ||
		config.ResponsesWSMaxSessionDuration != time.Hour ||
		config.ResponsesWSMaxConnectionsPerAPIKey != 64 ||
		config.ResponsesWSDialTimeout != 10*time.Second ||
		config.ResponsesWSReadTimeout != 15*time.Minute ||
		config.ResponsesWSWriteTimeout != 2*time.Minute ||
		config.ResponsesWSUpstreamDrainTimeout != 1200*time.Millisecond ||
		config.ResponsesWSClientReadLimitBytes != 64<<20 ||
		config.ResponsesWSUpstreamReadLimitBytes != 16<<20 ||
		config.ResponsesWSReplayMemoryLimitBytes != 64<<20 {
		t.Fatalf("Responses WebSocket defaults = %+v", config)
	}
}

func TestLoadResponsesWSOverrides(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SHARESUB_RESPONSES_WS_FIRST_MESSAGE_TIMEOUT", "12s")
	t.Setenv("SHARESUB_RESPONSES_WS_INTER_TURN_IDLE_TIMEOUT", "2m")
	t.Setenv("SHARESUB_RESPONSES_WS_MAX_SESSION_DURATION", "45m")
	t.Setenv("SHARESUB_RESPONSES_WS_MAX_CONNECTIONS_PER_API_KEY", "17")
	t.Setenv("SHARESUB_RESPONSES_WS_DIAL_TIMEOUT", "8s")
	t.Setenv("SHARESUB_RESPONSES_WS_READ_TIMEOUT", "4m")
	t.Setenv("SHARESUB_RESPONSES_WS_WRITE_TIMEOUT", "45s")
	t.Setenv("SHARESUB_RESPONSES_WS_UPSTREAM_DRAIN_TIMEOUT", "900ms")
	t.Setenv("SHARESUB_RESPONSES_WS_CLIENT_READ_LIMIT_BYTES", "33554432")
	t.Setenv("SHARESUB_RESPONSES_WS_UPSTREAM_READ_LIMIT_BYTES", "8388608")
	t.Setenv("SHARESUB_RESPONSES_WS_REPLAY_MEMORY_LIMIT_BYTES", "25165824")

	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.ResponsesWSFirstMessageTimeout != 12*time.Second ||
		config.ResponsesWSInterTurnIdleTimeout != 2*time.Minute ||
		config.ResponsesWSMaxSessionDuration != 45*time.Minute ||
		config.ResponsesWSMaxConnectionsPerAPIKey != 17 ||
		config.ResponsesWSDialTimeout != 8*time.Second ||
		config.ResponsesWSReadTimeout != 4*time.Minute ||
		config.ResponsesWSWriteTimeout != 45*time.Second ||
		config.ResponsesWSUpstreamDrainTimeout != 900*time.Millisecond ||
		config.ResponsesWSClientReadLimitBytes != 32<<20 ||
		config.ResponsesWSUpstreamReadLimitBytes != 8<<20 ||
		config.ResponsesWSReplayMemoryLimitBytes != 24<<20 {
		t.Fatalf("Responses WebSocket overrides = %+v", config)
	}
}

func TestLoadRejectsInvalidResponsesWSReplayMemoryLimit(t *testing.T) {
	for _, value := range []string{"0", "-1", "not-a-number", "9223372036854775808"} {
		t.Run(value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("SHARESUB_RESPONSES_WS_REPLAY_MEMORY_LIMIT_BYTES", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SHARESUB_RESPONSES_WS_REPLAY_MEMORY_LIMIT_BYTES") {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidResourceLimits(t *testing.T) {
	for name, value := range map[string]string{
		"SHARESUB_CLEANUP_INTERVAL":                            "never",
		"SHARESUB_GATEWAY_METRIC_RETENTION":                    "167h59m59s",
		"SHARESUB_TOKEN_REFRESH_INTERVAL":                      "never",
		"SHARESUB_TOKEN_REFRESH_CONCURRENCY":                   "0",
		"SHARESUB_TOKEN_REFRESH_ENABLED":                       "sometimes",
		"SHARESUB_RESPONSES_WS_FIRST_MESSAGE_TIMEOUT":          "never",
		"SHARESUB_RESPONSES_WS_INTER_TURN_IDLE_TIMEOUT":        "0s",
		"SHARESUB_RESPONSES_WS_MAX_SESSION_DURATION":           "-1m",
		"SHARESUB_RESPONSES_WS_MAX_CONNECTIONS_PER_API_KEY":    "0",
		"SHARESUB_RESPONSES_WS_DIAL_TIMEOUT":                   "0s",
		"SHARESUB_RESPONSES_WS_READ_TIMEOUT":                   "never",
		"SHARESUB_RESPONSES_WS_WRITE_TIMEOUT":                  "-1s",
		"SHARESUB_RESPONSES_WS_UPSTREAM_DRAIN_TIMEOUT":         "0s",
		"SHARESUB_RESPONSES_WS_CLIENT_READ_LIMIT_BYTES":        "0",
		"SHARESUB_RESPONSES_WS_UPSTREAM_READ_LIMIT_BYTES":      "9223372036854775808",
		"SHARESUB_GATEWAY_MAX_REQUESTS_PER_MINUTE_PER_API_KEY": "0",
		"SHARESUB_GATEWAY_FIRST_OUTPUT_TIMEOUT":                "0s",
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

func TestLoadTencentSESConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SHARESUB_EMAIL_DELIVERY_PROVIDER", "tencent_ses")
	t.Setenv("SHARESUB_TENCENT_SES_SECRET_ID", "secret-id")
	t.Setenv("SHARESUB_TENCENT_SES_SECRET_KEY", "secret-key")
	t.Setenv("SHARESUB_TENCENT_SES_REGION", "ap-hongkong")
	t.Setenv("SHARESUB_TENCENT_SES_FROM_EMAIL", "no-reply@notify.underelay.com")
	t.Setenv("SHARESUB_TENCENT_SES_FROM_NAME", "ShareSub")
	t.Setenv("SHARESUB_TENCENT_SES_TEMPLATE_ID", "212354")
	t.Setenv("SHARESUB_EMAIL_VERIFICATION_TTL", "45m")
	t.Setenv("SHARESUB_EMAIL_RESEND_COOLDOWN", "90s")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.EmailDeliveryProvider != "tencent_ses" || config.TencentSESTemplateID != 212354 || config.EmailVerificationTTL != 45*time.Minute || config.EmailResendCooldown != 90*time.Second {
		t.Fatalf("Tencent SES configuration = %+v", config)
	}
}

func TestLoadRequiresCompleteTencentSESConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SHARESUB_EMAIL_DELIVERY_PROVIDER", "tencent_ses")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SHARESUB_TENCENT_SES_SECRET_ID") {
		t.Fatalf("Load() error = %v", err)
	}
}
