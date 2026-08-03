package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	PublicURL     string
	OAuthRedirect string
	OutboundProxy string
	SessionTTL    time.Duration
	TokenPepper   []byte
	CredentialKey []byte
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
	return Config{
		HTTPAddr:      envOr("SHARESUB_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   databaseURL,
		PublicURL:     envOr("SHARESUB_PUBLIC_URL", "http://127.0.0.1:5173"),
		OAuthRedirect: envOr("SHARESUB_OAUTH_REDIRECT_URI", "http://localhost:1455/auth/callback"),
		OutboundProxy: os.Getenv("SHARESUB_OUTBOUND_PROXY"),
		SessionTTL:    ttl,
		TokenPepper:   pepper,
		CredentialKey: credentialKey,
	}, nil
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
