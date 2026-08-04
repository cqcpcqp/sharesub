package postgres

import (
	"testing"
	"time"
)

func TestGatewayMetricCutoffKeepsPartialUTCDay(t *testing.T) {
	now := time.Date(2026, 8, 4, 18, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	got := gatewayMetricCutoff(now, 7*24*time.Hour)
	want := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("gateway metric cutoff = %s, want %s", got, want)
	}
}
