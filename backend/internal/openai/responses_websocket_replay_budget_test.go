package openai

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestResponsesWebSocketReplayReservationResizeAndRelease(t *testing.T) {
	budget := newResponsesWebSocketReplayBudget(10)
	reservation := newResponsesWebSocketReplayReservation(budget)

	if !reservation.resize(4) || reservation.bytes != 4 || budget.usedBytes() != 4 {
		t.Fatalf("initial reservation = bytes %d used %d", reservation.bytes, budget.usedBytes())
	}
	if !reservation.resize(10) || reservation.bytes != 10 || budget.usedBytes() != 10 {
		t.Fatalf("exact-limit reservation = bytes %d used %d", reservation.bytes, budget.usedBytes())
	}
	if !reservation.resize(3) || reservation.bytes != 3 || budget.usedBytes() != 3 {
		t.Fatalf("shrunk reservation = bytes %d used %d", reservation.bytes, budget.usedBytes())
	}
	reservation.release()
	if reservation.bytes != 0 || budget.usedBytes() != 0 {
		t.Fatalf("released reservation = bytes %d used %d", reservation.bytes, budget.usedBytes())
	}
}

func TestResponsesWebSocketReplayReservationRejectionDoesNotMutateState(t *testing.T) {
	budget := newResponsesWebSocketReplayBudget(10)
	first := newResponsesWebSocketReplayReservation(budget)
	second := newResponsesWebSocketReplayReservation(budget)
	if !first.resize(7) {
		t.Fatal("reserve first allocation")
	}

	if second.resize(4) {
		t.Fatal("over-budget reservation unexpectedly succeeded")
	}
	if first.bytes != 7 || second.bytes != 0 || budget.usedBytes() != 7 {
		t.Fatalf("state after rejected reservation = first %d second %d used %d", first.bytes, second.bytes, budget.usedBytes())
	}
	if first.resize(11) {
		t.Fatal("over-budget growth unexpectedly succeeded")
	}
	if first.bytes != 7 || second.bytes != 0 || budget.usedBytes() != 7 {
		t.Fatalf("state after rejected growth = first %d second %d used %d", first.bytes, second.bytes, budget.usedBytes())
	}
	if first.resize(-1) {
		t.Fatal("negative reservation unexpectedly succeeded")
	}
	if first.bytes != 7 || budget.usedBytes() != 7 {
		t.Fatalf("state after rejected negative resize = first %d used %d", first.bytes, budget.usedBytes())
	}
}

func TestResponsesWebSocketReplayReservationReplaceWithTransfersAtomically(t *testing.T) {
	budget := newResponsesWebSocketReplayBudget(12)
	history := newResponsesWebSocketReplayReservation(budget)
	current := newResponsesWebSocketReplayReservation(budget)
	output := newResponsesWebSocketReplayReservation(budget)
	other := newResponsesWebSocketReplayReservation(budget)
	for name, reservation := range map[string]*responsesWebSocketReplayReservation{
		"history": &history,
		"current": &current,
		"output":  &output,
		"other":   &other,
	} {
		var size int64
		switch name {
		case "history":
			size = 4
		case "current", "output":
			size = 3
		case "other":
			size = 2
		}
		if !reservation.resize(size) {
			t.Fatalf("reserve %s", name)
		}
	}
	if budget.usedBytes() != 12 {
		t.Fatalf("used before transfer = %d, want 12", budget.usedBytes())
	}

	if !history.replaceWith(9, &current, &output) {
		t.Fatal("atomic replacement unexpectedly failed")
	}
	if history.bytes != 9 || current.bytes != 0 || output.bytes != 0 || other.bytes != 2 || budget.usedBytes() != 11 {
		t.Fatalf("state after transfer = history %d current %d output %d other %d used %d", history.bytes, current.bytes, output.bytes, other.bytes, budget.usedBytes())
	}
}

func TestResponsesWebSocketReplayReservationRejectedReplaceWithIsAtomic(t *testing.T) {
	budget := newResponsesWebSocketReplayBudget(10)
	target := newResponsesWebSocketReplayReservation(budget)
	source := newResponsesWebSocketReplayReservation(budget)
	other := newResponsesWebSocketReplayReservation(budget)
	if !target.resize(3) || !source.resize(2) || !other.resize(5) {
		t.Fatal("prepare full budget")
	}

	if target.replaceWith(6, &source) {
		t.Fatal("over-budget replacement unexpectedly succeeded")
	}
	if target.bytes != 3 || source.bytes != 2 || other.bytes != 5 || budget.usedBytes() != 10 {
		t.Fatalf("state after rejected replacement = target %d source %d other %d used %d", target.bytes, source.bytes, other.bytes, budget.usedBytes())
	}
}

func TestResponsesWebSocketReplayReservationReleaseIsIdempotent(t *testing.T) {
	budget := newResponsesWebSocketReplayBudget(10)
	reservation := newResponsesWebSocketReplayReservation(budget)
	if !reservation.resize(6) {
		t.Fatal("reserve allocation")
	}
	reservation.release()
	reservation.release()
	if reservation.bytes != 0 || budget.usedBytes() != 0 {
		t.Fatalf("state after repeated release = bytes %d used %d", reservation.bytes, budget.usedBytes())
	}
}

func TestResponsesWebSocketReplayBudgetConcurrentReserveRelease(t *testing.T) {
	const (
		limit      = int64(16)
		goroutines = 64
		iterations = 200
	)
	budget := newResponsesWebSocketReplayBudget(limit)
	start := make(chan struct{})
	var wait sync.WaitGroup
	var invariantFailed atomic.Bool
	wait.Add(goroutines)
	for range goroutines {
		go func() {
			defer wait.Done()
			<-start
			for range iterations {
				reservation := newResponsesWebSocketReplayReservation(budget)
				if reservation.resize(1) {
					_ = reservation.resize(2)
					_ = reservation.resize(1)
					reservation.release()
				}
				used := budget.usedBytes()
				if used < 0 || used > limit {
					invariantFailed.Store(true)
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	if invariantFailed.Load() {
		t.Fatal("concurrent reservation violated budget bounds")
	}
	if used := budget.usedBytes(); used != 0 {
		t.Fatalf("used after concurrent release = %d, want 0", used)
	}
}
