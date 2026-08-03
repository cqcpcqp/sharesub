package application

import (
	"net/http"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestParseCodexQuotaHeadersUsesWindowDuration(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "18.25")
	headers.Set("x-codex-primary-reset-after-seconds", "600")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "2.5")
	headers.Set("x-codex-secondary-reset-after-seconds", "120")
	headers.Set("x-codex-secondary-window-minutes", "300")
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)

	signals := ParseCodexQuotaHeaders(headers, now)
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2", len(signals))
	}
	if signals[0].WindowType != domain.Window7D || signals[0].AccountUsedMicros != 18_250_000 {
		t.Fatalf("unexpected primary signal: %#v", signals[0])
	}
	if signals[1].WindowType != domain.Window5H || signals[1].AccountUsedMicros != 2_500_000 {
		t.Fatalf("unexpected secondary signal: %#v", signals[1])
	}
}

func TestParseCodexQuotaHeadersRejectsIncompleteSignal(t *testing.T) {
	headers := http.Header{"X-Codex-Primary-Used-Percent": []string{"10"}}
	if got := ParseCodexQuotaHeaders(headers, time.Now()); len(got) != 0 {
		t.Fatalf("got %#v, want no signals", got)
	}
}

func TestParseCodexQuotaHeadersKeepsCountdownWindowStable(t *testing.T) {
	firstHeaders := http.Header{}
	firstHeaders.Set("x-codex-primary-used-percent", "10")
	firstHeaders.Set("x-codex-primary-reset-after-seconds", "600")
	firstHeaders.Set("x-codex-primary-window-minutes", "300")
	secondHeaders := firstHeaders.Clone()
	secondHeaders.Set("x-codex-primary-reset-after-seconds", "599")

	first := ParseCodexQuotaHeaders(firstHeaders, time.Date(2026, 8, 2, 10, 0, 0, 150_000_000, time.UTC))
	second := ParseCodexQuotaHeaders(secondHeaders, time.Date(2026, 8, 2, 10, 0, 1, 850_000_000, time.UTC))

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("signals = %d and %d, want one each", len(first), len(second))
	}
	if !first[0].ResetAt.Equal(second[0].ResetAt) || !first[0].WindowStart.Equal(second[0].WindowStart) {
		t.Fatalf("countdown window changed: first=%#v second=%#v", first[0], second[0])
	}
}
