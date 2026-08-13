package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const minimumGatewayMetricRetention = 7 * 24 * time.Hour

type Config struct {
	HTTPAddr                           string
	DatabaseURL                        string
	PublicURL                          string
	OAuthRedirect                      string
	OutboundProxy                      string
	SessionTTL                         time.Duration
	TokenPepper                        []byte
	CredentialKey                      []byte
	CleanupInterval                    time.Duration
	GatewayMetricRetention             time.Duration
	AuditEventRetention                time.Duration
	ReadNotificationRetention          time.Duration
	TerminalRecordRetention            time.Duration
	TokenRefreshEnabled                bool
	TokenRefreshInterval               time.Duration
	TokenRefreshBeforeExpiry           time.Duration
	TokenRefreshBatchSize              int
	TokenRefreshConcurrency            int
	TokenRefreshMaxRetries             int
	ResponsesWSFirstMessageTimeout     time.Duration
	ResponsesWSInterTurnIdleTimeout    time.Duration
	ResponsesWSMaxSessionDuration      time.Duration
	ResponsesWSMaxConnectionsPerAPIKey int
	ResponsesWSDialTimeout             time.Duration
	ResponsesWSReadTimeout             time.Duration
	ResponsesWSWriteTimeout            time.Duration
	ResponsesWSUpstreamDrainTimeout    time.Duration
	ResponsesWSClientReadLimitBytes    int64
	ResponsesWSUpstreamReadLimitBytes  int64
}

func Load() (Config, error) {
	ttl := 30 * 24 * time.Hour
	if raw := os.Getenv("SHARESUB_SESSION_TTL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHARESUB_SESSION_TTL: %w", err)
		}
		ttl = parsed
	}
	credentialKey, err := decodeKey("SHARESUB_CREDENTIAL_KEY")
	if err != nil {
		return Config{}, err
	}
	pepper, err := decodeKey("SHARESUB_TOKEN_PEPPER")
	if err != nil {
		return Config{}, err
	}
	databaseURL := os.Getenv("SHARESUB_DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("SHARESUB_DATABASE_URL is required")
	}
	cleanupInterval, err := positiveDurationEnv("SHARESUB_CLEANUP_INTERVAL", 6*time.Hour)
	if err != nil {
		return Config{}, err
	}
	gatewayMetricRetention, err := positiveDurationEnv("SHARESUB_GATEWAY_METRIC_RETENTION", 90*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if gatewayMetricRetention < minimumGatewayMetricRetention {
		return Config{}, fmt.Errorf("SHARESUB_GATEWAY_METRIC_RETENTION must be at least %s", minimumGatewayMetricRetention)
	}
	auditEventRetention, err := positiveDurationEnv("SHARESUB_AUDIT_EVENT_RETENTION", 365*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	readNotificationRetention, err := positiveDurationEnv("SHARESUB_READ_NOTIFICATION_RETENTION", 90*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	terminalRecordRetention, err := positiveDurationEnv("SHARESUB_TERMINAL_RECORD_RETENTION", 90*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	tokenRefreshEnabled, err := boolEnv("SHARESUB_TOKEN_REFRESH_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	tokenRefreshInterval, err := positiveDurationEnv("SHARESUB_TOKEN_REFRESH_INTERVAL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	tokenRefreshBeforeExpiry, err := positiveDurationEnv("SHARESUB_TOKEN_REFRESH_BEFORE_EXPIRY", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	tokenRefreshBatchSize, err := positiveIntEnv("SHARESUB_TOKEN_REFRESH_BATCH_SIZE", 200)
	if err != nil {
		return Config{}, err
	}
	tokenRefreshConcurrency, err := positiveIntEnv("SHARESUB_TOKEN_REFRESH_CONCURRENCY", 4)
	if err != nil {
		return Config{}, err
	}
	tokenRefreshMaxRetries, err := positiveIntEnv("SHARESUB_TOKEN_REFRESH_MAX_RETRIES", 3)
	if err != nil {
		return Config{}, err
	}
	responsesWSFirstMessageTimeout, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_FIRST_MESSAGE_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	responsesWSInterTurnIdleTimeout, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_INTER_TURN_IDLE_TIMEOUT", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	responsesWSMaxSessionDuration, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_MAX_SESSION_DURATION", time.Hour)
	if err != nil {
		return Config{}, err
	}
	responsesWSMaxConnectionsPerAPIKey, err := positiveIntEnv("SHARESUB_RESPONSES_WS_MAX_CONNECTIONS_PER_API_KEY", 64)
	if err != nil {
		return Config{}, err
	}
	responsesWSDialTimeout, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_DIAL_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	responsesWSReadTimeout, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_READ_TIMEOUT", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	responsesWSWriteTimeout, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_WRITE_TIMEOUT", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	responsesWSUpstreamDrainTimeout, err := positiveDurationEnv("SHARESUB_RESPONSES_WS_UPSTREAM_DRAIN_TIMEOUT", 1200*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	responsesWSClientReadLimitBytes, err := positiveInt64Env("SHARESUB_RESPONSES_WS_CLIENT_READ_LIMIT_BYTES", 64<<20)
	if err != nil {
		return Config{}, err
	}
	responsesWSUpstreamReadLimitBytes, err := positiveInt64Env("SHARESUB_RESPONSES_WS_UPSTREAM_READ_LIMIT_BYTES", 16<<20)
	if err != nil {
		return Config{}, err
	}
	return Config{
		HTTPAddr:                           envOr("SHARESUB_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:                        databaseURL,
		PublicURL:                          envOr("SHARESUB_PUBLIC_URL", "http://127.0.0.1:5173"),
		OAuthRedirect:                      envOr("SHARESUB_OAUTH_REDIRECT_URI", "http://localhost:1455/auth/callback"),
		OutboundProxy:                      os.Getenv("SHARESUB_OUTBOUND_PROXY"),
		SessionTTL:                         ttl,
		TokenPepper:                        pepper,
		CredentialKey:                      credentialKey,
		CleanupInterval:                    cleanupInterval,
		GatewayMetricRetention:             gatewayMetricRetention,
		AuditEventRetention:                auditEventRetention,
		ReadNotificationRetention:          readNotificationRetention,
		TerminalRecordRetention:            terminalRecordRetention,
		TokenRefreshEnabled:                tokenRefreshEnabled,
		TokenRefreshInterval:               tokenRefreshInterval,
		TokenRefreshBeforeExpiry:           tokenRefreshBeforeExpiry,
		TokenRefreshBatchSize:              tokenRefreshBatchSize,
		TokenRefreshConcurrency:            tokenRefreshConcurrency,
		TokenRefreshMaxRetries:             tokenRefreshMaxRetries,
		ResponsesWSFirstMessageTimeout:     responsesWSFirstMessageTimeout,
		ResponsesWSInterTurnIdleTimeout:    responsesWSInterTurnIdleTimeout,
		ResponsesWSMaxSessionDuration:      responsesWSMaxSessionDuration,
		ResponsesWSMaxConnectionsPerAPIKey: responsesWSMaxConnectionsPerAPIKey,
		ResponsesWSDialTimeout:             responsesWSDialTimeout,
		ResponsesWSReadTimeout:             responsesWSReadTimeout,
		ResponsesWSWriteTimeout:            responsesWSWriteTimeout,
		ResponsesWSUpstreamDrainTimeout:    responsesWSUpstreamDrainTimeout,
		ResponsesWSClientReadLimitBytes:    responsesWSClientReadLimitBytes,
		ResponsesWSUpstreamReadLimitBytes:  responsesWSUpstreamReadLimitBytes,
	}, nil
}

func boolEnv(name string, fallback bool) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func positiveInt64Env(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func positiveDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func decodeKey(name string) ([]byte, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must decode to exactly 32 bytes", name)
	}
	return key, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
