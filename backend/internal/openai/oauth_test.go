package openai

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/imroc/req/v3"
)

func TestOAuthTokenRequestsUseCodexCredentialIdentity(t *testing.T) {
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"codex@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acct_1","chatgpt_plan_type":"pro"}}`))
	idToken := "header." + claims + ".signature"

	for _, test := range []struct {
		name      string
		invoke    func(*OAuthClient) error
		grantType string
	}{
		{
			name: "exchange",
			invoke: func(client *OAuthClient) error {
				_, err := client.Exchange(context.Background(), "code", "verifier", "http://localhost/callback")
				return err
			},
			grantType: "authorization_code",
		},
		{
			name: "refresh",
			invoke: func(client *OAuthClient) error {
				_, err := client.Refresh(context.Background(), "refresh-token")
				return err
			},
			grantType: "refresh_token",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewOAuthClient("")
			client.client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
				return func(request *http.Request) (*http.Response, error) {
					if request.Header.Get("Originator") != codexDefaultOriginator || request.Header.Get("User-Agent") != codexProbeUserAgent {
						t.Fatalf("credential identity = %#v", request.Header)
					}
					if request.Header.Get("Version") != "" {
						t.Fatalf("credential request Version = %q", request.Header.Get("Version"))
					}
					if err := request.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if request.Form.Get("grant_type") != test.grantType {
						t.Fatalf("grant_type = %q", request.Form.Get("grant_type"))
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","id_token":"` + idToken + `","expires_in":3600}`)),
					}, nil
				}
			})
			if err := test.invoke(client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAuthorizationURL(t *testing.T) {
	client := NewOAuthClient("")
	raw := client.AuthorizationURL("state", "challenge", "http://localhost/callback")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.String() == "" || query.Get("client_id") != clientID || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected authorization URL: %s", raw)
	}
	if query.Get("codex_cli_simplified_flow") != "true" {
		t.Fatal("Codex simplified flow must be enabled")
	}
}

func TestSubscriptionExpiresAt(t *testing.T) {
	client := NewOAuthClient("")
	client.client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
		return func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != subscriptionURL+"?account_id=chatgpt-account" {
				t.Fatalf("subscription URL = %q", request.URL.String())
			}
			if request.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"plan_type":"pro","active_until":"2026-09-06T10:00:00Z","will_renew":true,"id":"subscription"}`)),
			}, nil
		}
	})

	expiresAt, err := client.SubscriptionExpiresAt(context.Background(), "access-token", "chatgpt-account", "")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	if expiresAt == nil || !expiresAt.Equal(want) {
		t.Fatalf("subscription expiry = %v, want %v", expiresAt, want)
	}
}

func TestSubscriptionExpiresAtAllowsNoPaidSubscription(t *testing.T) {
	client := NewOAuthClient("")
	client.client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
		return func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"plan_type":"free","active_until":"","will_renew":false,"id":""}`)),
			}, nil
		}
	})

	expiresAt, err := client.SubscriptionExpiresAt(context.Background(), "access-token", "chatgpt-account", "")
	if err != nil || expiresAt != nil {
		t.Fatalf("subscription expiry = %v, error = %v", expiresAt, err)
	}
}
