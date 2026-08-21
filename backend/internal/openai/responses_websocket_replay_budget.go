package openai

import (
	"math"
	"sync"
)

const defaultWSReplayMemoryLimitBytes int64 = 64 << 20

// responsesWebSocketReplayBudget bounds replay-owned payload memory across
// every Run call sharing one ResponsesWebSocketSession.
type responsesWebSocketReplayBudget struct {
	mu    sync.Mutex
	limit int64
	used  int64
}

type responsesWebSocketReplayReservation struct {
	budget *responsesWebSocketReplayBudget
	bytes  int64
}

func newResponsesWebSocketReplayBudget(limit int64) *responsesWebSocketReplayBudget {
	return &responsesWebSocketReplayBudget{limit: positiveInt64(limit, defaultWSReplayMemoryLimitBytes)}
}

func newResponsesWebSocketReplayReservation(budget *responsesWebSocketReplayBudget) responsesWebSocketReplayReservation {
	return responsesWebSocketReplayReservation{budget: budget}
}

func (r *responsesWebSocketReplayReservation) resize(next int64) bool {
	return r.replaceWith(next)
}

// replaceWith atomically replaces this reservation and consumes the supplied
// reservations. This lets a completed turn transfer its current input and
// collected output into durable history without temporarily double-counting or
// releasing capacity that another session could take between operations.
func (r *responsesWebSocketReplayReservation) replaceWith(next int64, sources ...*responsesWebSocketReplayReservation) bool {
	if r == nil || next < 0 {
		return false
	}
	budget := r.budget
	var uniqueSources []*responsesWebSocketReplayReservation
	for _, source := range sources {
		if source == nil {
			continue
		}
		if source == r || source.budget != budget {
			return false
		}
		duplicate := false
		for _, seen := range uniqueSources {
			if source == seen {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		uniqueSources = append(uniqueSources, source)
	}
	if budget == nil {
		if r.bytes < 0 {
			return false
		}
		for _, source := range uniqueSources {
			if source.bytes < 0 {
				return false
			}
		}
		r.bytes = next
		for _, source := range uniqueSources {
			source.bytes = 0
		}
		return true
	}

	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.limit < 0 || budget.used < 0 || budget.used > budget.limit || r.bytes < 0 {
		return false
	}
	released := r.bytes
	for _, source := range uniqueSources {
		if source.bytes < 0 || released > math.MaxInt64-source.bytes {
			return false
		}
		released += source.bytes
	}
	if released > budget.used {
		return false
	}
	retained := budget.used - released
	if next > budget.limit-retained {
		return false
	}
	budget.used = retained + next
	r.bytes = next
	for _, source := range uniqueSources {
		source.bytes = 0
	}
	return true
}

func (r *responsesWebSocketReplayReservation) release() {
	if r != nil {
		_ = r.resize(0)
	}
}

func (b *responsesWebSocketReplayBudget) usedBytes() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
