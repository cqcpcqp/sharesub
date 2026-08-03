package postgres

import (
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestSameQuotaWindowAllowsResetCountdownDrift(t *testing.T) {
	reset := time.Date(2026, 8, 8, 7, 32, 25, 0, time.UTC)
	if !sameQuotaWindow(reset, reset.Add(9*time.Second)) {
		t.Fatal("same quota window was split by response-header countdown drift")
	}
	if sameQuotaWindow(reset, reset.Add(7*24*time.Hour)) {
		t.Fatal("different quota cycles were merged")
	}
}

func TestMergeAccountQuotaSignalKeepsCanonicalWindowAndMonotonicUsage(t *testing.T) {
	start := time.Date(2026, 8, 1, 7, 32, 25, 0, time.UTC)
	reset := start.Add(7 * 24 * time.Hour)
	signal := domain.QuotaSignal{WindowType: domain.Window7D, WindowStart: start.Add(9 * time.Second), ResetAt: reset.Add(9 * time.Second), AccountUsedMicros: 11_000_000}

	merged, delta, err := mergeAccountQuotaSignal(start, reset, 12_000_000, signal)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.WindowStart.Equal(start) || !merged.ResetAt.Equal(reset) {
		t.Fatalf("canonical window changed: %+v", merged)
	}
	if merged.AccountUsedMicros != 12_000_000 || delta != 0 {
		t.Fatalf("out-of-order lower usage was accepted: usage=%d delta=%d", merged.AccountUsedMicros, delta)
	}

	signal.AccountUsedMicros = 13_000_000
	merged, delta, err = mergeAccountQuotaSignal(start, reset, 12_000_000, signal)
	if err != nil || merged.AccountUsedMicros != 13_000_000 || delta != 1_000_000 {
		t.Fatalf("increasing usage was not attributed: usage=%d delta=%d err=%v", merged.AccountUsedMicros, delta, err)
	}
}
