package application

import (
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type accountTrafficController struct {
	mu     sync.Mutex
	states map[string]*accountTrafficState
}

type accountTrafficState struct {
	activeRequests int
	minuteStart    time.Time
	minuteRequests int
}

func newAccountTrafficController() *accountTrafficController {
	return &accountTrafficController{states: make(map[string]*accountTrafficState)}
}

func (c *accountTrafficController) acquire(accountID string, maxConcurrency, rpmLimit int, now time.Time) (func(), error) {
	c.mu.Lock()
	state := c.states[accountID]
	if state == nil {
		state = &accountTrafficState{}
		c.states[accountID] = state
	}
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
