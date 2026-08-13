package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestAccountTrafficControllerEvictsInactiveStates(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	release, err := controller.acquire("old-account", 1, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if len(controller.states) != 1 {
		t.Fatalf("traffic states = %d, want 1", len(controller.states))
	}
	later := now.Add(accountTrafficStateTTL + accountTrafficCleanupPeriod)
	release, err = controller.acquire("new-account", 1, 0, later)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if controller.states["old-account"] != nil || controller.states["new-account"] == nil {
		t.Fatalf("traffic states were not pruned: %+v", controller.states)
	}
}

func TestAccountTrafficControllerQuiesceWaitsAndRejectsNewRequests(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	releaseRequest, err := controller.reserve("account", now)
	if err != nil {
		t.Fatal(err)
	}
	quiesced := make(chan func(), 1)
	go func() {
		release, quiesceErr := controller.quiesce(context.Background(), "account")
		if quiesceErr != nil {
			quiesced <- nil
			return
		}
		quiesced <- release
	}()

	deadline := time.Now().Add(time.Second)
	for {
		controller.mu.Lock()
		quiescing := controller.states["account"].quiescing
		controller.mu.Unlock()
		if quiescing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("account never entered quiescing state")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := controller.acquire("account", 0, 0, now); err != domain.ErrAccountUnavailable {
		t.Fatalf("acquire while quiescing error = %v", err)
	}
	select {
	case <-quiesced:
		t.Fatal("quiesce completed before active request released")
	default:
	}
	releaseRequest()
	releaseQuiesce := <-quiesced
	if releaseQuiesce == nil {
		t.Fatal("quiesce failed")
	}
	if _, err := controller.acquire("account", 0, 0, now); err != domain.ErrAccountUnavailable {
		t.Fatalf("acquire before quiesce release error = %v", err)
	}
	releaseQuiesce()
	release, err := controller.acquire("account", 0, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestAccountTrafficControllerReservationDoesNotConsumeRPM(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	releaseReservation, err := controller.reserve("account", now)
	if err != nil {
		t.Fatal(err)
	}

	controller.mu.Lock()
	state := controller.states["account"]
	activeRequests := state.activeRequests
	minuteRequests := state.minuteRequests
	controller.mu.Unlock()
	if activeRequests != 1 || minuteRequests != 0 {
		t.Fatalf("reserved traffic state = active %d, minute %d; want 1/0", activeRequests, minuteRequests)
	}

	releaseRequest, err := controller.acquire("account", 0, 1, now)
	if err != nil {
		t.Fatalf("first RPM-counted request was rejected after reservation: %v", err)
	}
	releaseRequest()
	if _, err := controller.acquire("account", 0, 1, now); err != domain.ErrAccountRateLimited {
		t.Fatalf("second RPM-counted request error = %v, want rate limited", err)
	}

	releaseReservation()
	controller.mu.Lock()
	activeRequests = state.activeRequests
	controller.mu.Unlock()
	if activeRequests != 0 {
		t.Fatalf("active requests after reservation release = %d, want 0", activeRequests)
	}
}

func TestAccountTrafficControllerPreparedRequestCommitsOrRollsBackRPM(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

	commit, release, err := controller.prepare("account", 2, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := controller.prepare("account", 2, 1, now); err != domain.ErrAccountRateLimited {
		t.Fatalf("concurrent prepared request error = %v, want rate limited", err)
	}
	release()

	commit, release, err = controller.prepare("account", 2, 1, now)
	if err != nil {
		t.Fatalf("rolled-back RPM admission was not reusable: %v", err)
	}
	commit()
	release()
	if _, _, err := controller.prepare("account", 2, 1, now); err != domain.ErrAccountRateLimited {
		t.Fatalf("request after RPM commit error = %v, want rate limited", err)
	}
}

func TestAccountTrafficControllerPreparedRequestCommitsToOriginalMinute(t *testing.T) {
	controller := newAccountTrafficController()
	firstMinute := time.Date(2026, 8, 13, 12, 0, 59, 0, time.UTC)
	secondMinute := firstMinute.Add(time.Second)

	commitFirst, releaseFirst, err := controller.prepare("account", 0, 1, firstMinute)
	if err != nil {
		t.Fatal(err)
	}
	commitSecond, releaseSecond, err := controller.prepare("account", 0, 1, secondMinute)
	if err != nil {
		t.Fatalf("next-minute admission was blocked by a prior-minute preflight: %v", err)
	}

	commitFirst()
	releaseFirst()
	commitSecond()
	releaseSecond()
	if _, _, err := controller.prepare("account", 0, 1, secondMinute); err != domain.ErrAccountRateLimited {
		t.Fatalf("second-minute RPM was not consumed exactly once: %v", err)
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()
	state := controller.states["account"]
	if state == nil || len(state.pendingMinuteRequests) != 0 {
		t.Fatalf("pending RPM admissions leaked: %+v", state)
	}
}

func TestAccountTrafficControllerCleanupKeepsQuiescingState(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	releaseQuiesce, err := controller.quiesce(context.Background(), "account")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseQuiesce()

	controller.mu.Lock()
	controller.states["account"].lastSeen = now.Add(-accountTrafficStateTTL)
	controller.nextCleanup = now
	controller.mu.Unlock()

	releaseOther, err := controller.acquire("other-account", 0, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	releaseOther()
	if _, err := controller.acquire("account", 0, 0, now); err != domain.ErrAccountUnavailable {
		t.Fatalf("quiescing account was evicted during cleanup: %v", err)
	}
}

func TestAccountTrafficControllerCanceledQuiesceReleasesEveryAccount(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	releaseRequest, err := controller.acquire("second", 0, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, quiesceErr := controller.quiesce(ctx, "first", "second")
		done <- quiesceErr
	}()

	deadline := time.Now().Add(time.Second)
	for {
		controller.mu.Lock()
		firstQuiescing := controller.states["first"] != nil && controller.states["first"].quiescing
		secondQuiescing := controller.states["second"] != nil && controller.states["second"].quiescing
		controller.mu.Unlock()
		if firstQuiescing && secondQuiescing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("accounts never entered quiescing state")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("quiesce error = %v", err)
	}
	releaseRequest()
	for _, accountID := range []string{"first", "second"} {
		release, err := controller.acquire(accountID, 0, 0, now)
		if err != nil {
			t.Fatalf("account %q remained quiesced: %v", accountID, err)
		}
		release()
	}
}

func TestAccountTrafficControllerQuiesceBindingRereadsChangedTuple(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	releaseOld, err := controller.reserve("old-account", now)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	plan := domain.Plan{ID: "plan", AccountID: "old-account", AccountBindingGeneration: 1}
	firstRead := make(chan struct{})
	reads := 0
	binding := func(context.Context) (domain.Plan, error) {
		mu.Lock()
		defer mu.Unlock()
		reads++
		if reads == 1 {
			close(firstRead)
		}
		return plan, nil
	}

	type result struct {
		plan    domain.Plan
		release func()
		err     error
	}
	done := make(chan result, 1)
	go func() {
		got, release, quiesceErr := controller.quiesceBinding(context.Background(), "plan", binding)
		done <- result{plan: got, release: release, err: quiesceErr}
	}()
	<-firstRead
	mu.Lock()
	plan.AccountID = "new-account"
	plan.AccountBindingGeneration = 2
	mu.Unlock()
	releaseOld()

	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer got.release()
	if got.plan.AccountID != "new-account" || got.plan.AccountBindingGeneration != 2 {
		t.Fatalf("quiesced binding = %+v, want new-account generation 2", got.plan)
	}
	releaseOldAfterRetry, err := controller.reserve("old-account", now)
	if err != nil {
		t.Fatalf("old binding remained quiesced after tuple retry: %v", err)
	}
	releaseOldAfterRetry()
	if _, err := controller.reserve("new-account", now); err != domain.ErrAccountUnavailable {
		t.Fatalf("new binding was not held after tuple retry: %v", err)
	}
}
