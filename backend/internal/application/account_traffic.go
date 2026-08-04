package application

import (
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type accountTrafficController struct {
	mu          sync.Mutex
	states      map[string]*accountTrafficState
	nextCleanup time.Time
}

type accountTrafficState struct {
	activeRequests int
	minuteStart    time.Time
	minuteRequests int
	lastSeen       time.Time
}

const (
	accountTrafficStateTTL      = 30 * time.Minute
	accountTrafficCleanupPeriod = 10 * time.Minute
)

func newAccountTrafficController() *accountTrafficController {
	return &accountTrafficController{states: make(map[string]*accountTrafficState)}
}

func (c *accountTrafficController) acquire(accountID string, maxConcurrency, rpmLimit int, now time.Time) (func(), error) {
	c.mu.Lock()
	if c.nextCleanup.IsZero() || !now.Before(c.nextCleanup) {
		for id, cached := range c.states {
			if cached.activeRequests == 0 && now.Sub(cached.lastSeen) >= accountTrafficStateTTL {
				delete(c.states, id)
			}
		}
		c.nextCleanup = now.Add(accountTrafficCleanupPeriod)
	}
	state := c.states[accountID]
	if state == nil {
		state = &accountTrafficState{}
		c.states[accountID] = state
	}
	state.lastSeen = now
	minuteStart := now.Truncate(time.Minute)
	if !state.minuteStart.Equal(minuteStart) {
		state.minuteStart = minuteStart
		state.minuteRequests = 0
	}
	if maxConcurrency > 0 && state.activeRequests >= maxConcurrency {
		c.mu.Unlock()
		return nil, domain.ErrAccountConcurrency
	}
	if rpmLimit > 0 && state.minuteRequests >= rpmLimit {
		c.mu.Unlock()
		return nil, domain.ErrAccountRateLimited
	}
	state.activeRequests++
	state.minuteRequests++
	c.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			state.activeRequests--
			c.mu.Unlock()
		})
	}, nil
}
