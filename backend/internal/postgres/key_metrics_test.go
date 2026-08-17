package postgres

import (
	"context"
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

func TestBindingQuotaSignalsRequireWeeklyAndAcceptOptionalFiveHourWindow(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	fiveHour := domain.QuotaSignal{WindowType: domain.Window5H, WindowStart: now, ResetAt: now.Add(5 * time.Hour)}
	sevenDay := domain.QuotaSignal{WindowType: domain.Window7D, WindowStart: now, ResetAt: now.Add(7 * 24 * time.Hour)}

	for _, test := range []struct {
		name    string
		signals []domain.QuotaSignal
		want    bool
	}{
		{name: "complete", signals: []domain.QuotaSignal{fiveHour, sevenDay}, want: true},
		{name: "weekly only", signals: []domain.QuotaSignal{sevenDay}, want: true},
		{name: "missing 7d", signals: []domain.QuotaSignal{fiveHour}},
		{name: "duplicate 5h", signals: []domain.QuotaSignal{fiveHour, fiveHour}},
		{name: "unknown window", signals: []domain.QuotaSignal{fiveHour, {WindowType: "30d"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := hasRequiredBindingQuotaSignals(test.signals); got != test.want {
				t.Fatalf("hasRequiredBindingQuotaSignals() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestOrderedBindingQuotaSignalsUsesStableWindowOrder(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	sevenDay := domain.QuotaSignal{WindowType: domain.Window7D, WindowStart: now.Add(-24 * time.Hour), ResetAt: now.Add(6 * 24 * time.Hour), AccountUsedMicros: 27_000_000}
	fiveHour := domain.QuotaSignal{WindowType: domain.Window5H, WindowStart: now.Add(-time.Hour), ResetAt: now.Add(4 * time.Hour), AccountUsedMicros: 13_000_000}

	ordered, ok := orderedBindingQuotaSignals([]domain.QuotaSignal{sevenDay, fiveHour})
	if !ok {
		t.Fatal("complete windows were rejected")
	}
	if ordered[0] != fiveHour || ordered[1] != sevenDay {
		t.Fatalf("ordered windows = %+v, want 5h then 7d", ordered)
	}
}

func TestRecordResetQuotaSignalsRejectsMissingWeeklyWindowBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	err := store.RecordQuotaResetSignals(context.Background(), "plan", "account", 1, []domain.QuotaSignal{{
		WindowType: domain.Window5H,
	}}, time.Now())
	if err != domain.ErrInvalidInput {
		t.Fatalf("RecordQuotaResetSignals() error = %v, want invalid input", err)
	}
}

func TestMergeAccountQuotaSignalKeepsCanonicalWindowAndMonotonicUsage(t *testing.T) {
	start := time.Date(2026, 8, 1, 7, 32, 25, 0, time.UTC)
	reset := start.Add(7 * 24 * time.Hour)
	signal := domain.QuotaSignal{WindowType: domain.Window7D, WindowStart: start.Add(9 * time.Second), ResetAt: reset.Add(9 * time.Second), AccountUsedMicros: 11_000_000}

	merged, err := mergeAccountQuotaSignal(start, reset, 12_000_000, signal)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.WindowStart.Equal(start) || !merged.ResetAt.Equal(reset) {
		t.Fatalf("canonical window changed: %+v", merged)
	}
	if merged.AccountUsedMicros != 12_000_000 {
		t.Fatalf("out-of-order lower usage was accepted: usage=%d", merged.AccountUsedMicros)
	}

	signal.AccountUsedMicros = 13_000_000
	merged, err = mergeAccountQuotaSignal(start, reset, 12_000_000, signal)
	if err != nil || merged.AccountUsedMicros != 13_000_000 {
		t.Fatalf("higher usage did not update the snapshot: usage=%d err=%v", merged.AccountUsedMicros, err)
	}
}

func TestMergeAccountQuotaSignalUsesNewNaturalWindow(t *testing.T) {
	oldStart := time.Date(2026, 8, 1, 5, 0, 0, 0, time.UTC)
	oldReset := oldStart.Add(5 * time.Hour)
	signal := domain.QuotaSignal{
		WindowType:        domain.Window5H,
		WindowStart:       oldReset,
		ResetAt:           oldReset.Add(5 * time.Hour),
		AccountUsedMicros: 2_000_000,
	}

	merged, err := mergeAccountQuotaSignal(oldStart, oldReset, 80_000_000, signal)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.WindowStart.Equal(signal.WindowStart) || !merged.ResetAt.Equal(signal.ResetAt) || merged.AccountUsedMicros != signal.AccountUsedMicros {
		t.Fatalf("new natural window was not adopted: %+v", merged)
	}
}

func TestMergeAccountQuotaSignalRejectsLateOldWindow(t *testing.T) {
	newStart := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	newReset := newStart.Add(5 * time.Hour)
	lateOldSignal := domain.QuotaSignal{
		WindowType:        domain.Window5H,
		WindowStart:       newStart.Add(-5 * time.Hour),
		ResetAt:           newReset.Add(-5 * time.Hour),
		AccountUsedMicros: 95_000_000,
	}

	merged, err := mergeAccountQuotaSignal(newStart, newReset, 2_000_000, lateOldSignal)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.WindowStart.Equal(newStart) || !merged.ResetAt.Equal(newReset) || merged.AccountUsedMicros != 2_000_000 {
		t.Fatalf("late old window replaced the current snapshot: %+v", merged)
	}
}

func TestReconcileAccountQuotaSignalLetsProbeReplaceLaterDifferentBucket(t *testing.T) {
	oldStart := time.Date(2026, 8, 17, 0, 55, 0, 0, time.UTC)
	oldReset := time.Date(2026, 8, 24, 0, 55, 0, 0, time.UTC)
	probedAt := time.Date(2026, 8, 17, 11, 29, 0, 0, time.UTC)
	globalSignal := domain.QuotaSignal{
		WindowType:        domain.Window7D,
		WindowStart:       time.Date(2026, 8, 13, 11, 29, 0, 0, time.UTC),
		ResetAt:           time.Date(2026, 8, 20, 11, 29, 0, 0, time.UTC),
		AccountUsedMicros: 48_000_000,
	}

	passive, authoritative, _, accepted, err := reconcileAccountQuotaSignal(oldStart, oldReset, 19_000_000, false, time.Time{}, globalSignal, false, probedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted || authoritative || !passive.ResetAt.Equal(oldReset) || passive.AccountUsedMicros != 19_000_000 {
		t.Fatalf("passive late signal was not protected: %+v", passive)
	}

	probed, authoritative, authoritativeAt, accepted, err := reconcileAccountQuotaSignal(oldStart, oldReset, 19_000_000, false, time.Time{}, globalSignal, true, probedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted || !authoritative || !authoritativeAt.Equal(probedAt) || probed != globalSignal {
		t.Fatalf("authoritative probe did not replace the old bucket: %+v", probed)
	}
}

func TestReconcileAccountQuotaSignalProtectsAuthoritativeBucketFromPassiveSignal(t *testing.T) {
	probedAt := time.Date(2026, 8, 17, 11, 29, 0, 0, time.UTC)
	globalStart := time.Date(2026, 8, 13, 11, 29, 0, 0, time.UTC)
	globalReset := time.Date(2026, 8, 20, 11, 29, 0, 0, time.UTC)
	routedSignal := domain.QuotaSignal{
		WindowType:        domain.Window7D,
		WindowStart:       time.Date(2026, 8, 17, 0, 55, 0, 0, time.UTC),
		ResetAt:           time.Date(2026, 8, 24, 0, 55, 0, 0, time.UTC),
		AccountUsedMicros: 19_000_000,
	}

	_, authoritative, authoritativeAt, accepted, err := reconcileAccountQuotaSignal(globalStart, globalReset, 48_000_000, true, probedAt, routedSignal, false, probedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if accepted || !authoritative || !authoritativeAt.Equal(probedAt) {
		t.Fatalf("passive routed bucket replaced authoritative snapshot: accepted=%v authoritative=%v at=%s", accepted, authoritative, authoritativeAt)
	}
}

func TestReconcileAccountQuotaSignalRejectsOlderProbeCompletingLate(t *testing.T) {
	newerProbeAt := time.Date(2026, 8, 17, 11, 30, 0, 0, time.UTC)
	oldStart := time.Date(2026, 8, 13, 11, 30, 0, 0, time.UTC)
	oldReset := time.Date(2026, 8, 20, 11, 30, 0, 0, time.UTC)
	lateSignal := domain.QuotaSignal{
		WindowType:        domain.Window7D,
		WindowStart:       oldStart.Add(-time.Minute),
		ResetAt:           oldReset.Add(-time.Minute),
		AccountUsedMicros: 20_000_000,
	}

	_, authoritative, authoritativeAt, accepted, err := reconcileAccountQuotaSignal(oldStart, oldReset, 40_000_000, true, newerProbeAt, lateSignal, true, newerProbeAt.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if accepted || !authoritative || !authoritativeAt.Equal(newerProbeAt) {
		t.Fatalf("older probe was accepted: accepted=%v authoritative=%v at=%s", accepted, authoritative, authoritativeAt)
	}
}
