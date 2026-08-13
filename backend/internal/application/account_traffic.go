package application

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type accountTrafficController struct {
	mu          sync.Mutex
	changed     chan struct{}
	states      map[string]*accountTrafficState
	nextCleanup time.Time
}

type accountTrafficState struct {
	activeRequests        int
	quiescing             bool
	idle                  chan struct{}
	minuteStart           time.Time
	minuteRequests        int
	pendingMinuteRequests map[int64]int
	lastSeen              time.Time
}

const (
	accountTrafficStateTTL      = 30 * time.Minute
	accountTrafficCleanupPeriod = 10 * time.Minute
)

func newAccountTrafficController() *accountTrafficController {
	return &accountTrafficController{changed: make(chan struct{}), states: make(map[string]*accountTrafficState)}
}

func (c *accountTrafficController) acquire(accountID string, maxConcurrency, rpmLimit int, now time.Time) (func(), error) {
	commit, release, err := c.prepareRequest(accountID, maxConcurrency, rpmLimit, now, true)
	if err != nil {
		return nil, err
	}
	commit()
	return release, nil
}

func (c *accountTrafficController) reserve(accountID string, now time.Time) (func(), error) {
	_, release, err := c.prepareRequest(accountID, 0, 0, now, false)
	return release, err
}

// prepare reserves concurrency and an RPM admission atomically, but does not
// consume the RPM until commit is called. Releasing before commit rolls the
// tentative RPM admission back, which keeps credential preflight failures from
// being counted as requests while still preventing concurrent over-admission.
func (c *accountTrafficController) prepare(accountID string, maxConcurrency, rpmLimit int, now time.Time) (commit, release func(), err error) {
	return c.prepareRequest(accountID, maxConcurrency, rpmLimit, now, true)
}

func (c *accountTrafficController) prepareRequest(accountID string, maxConcurrency, rpmLimit int, now time.Time, countRPM bool) (commit, release func(), err error) {
	c.mu.Lock()
	if c.nextCleanup.IsZero() || !now.Before(c.nextCleanup) {
		for id, cached := range c.states {
			if !cached.quiescing && cached.activeRequests == 0 && now.Sub(cached.lastSeen) >= accountTrafficStateTTL {
				delete(c.states, id)
			}
		}
		c.nextCleanup = now.Add(accountTrafficCleanupPeriod)
	}
	state := c.states[accountID]
	if state == nil {
		state = &accountTrafficState{idle: closedSignal(), pendingMinuteRequests: make(map[int64]int)}
		c.states[accountID] = state
	}
	if state.pendingMinuteRequests == nil {
		state.pendingMinuteRequests = make(map[int64]int)
	}
	if state.quiescing {
		c.mu.Unlock()
		return nil, nil, domain.ErrAccountUnavailable
	}
	state.lastSeen = now
	minuteStart := now.Truncate(time.Minute)
	minuteKey := minuteStart.Unix()
	if countRPM {
		if !state.minuteStart.Equal(minuteStart) {
			state.minuteStart = minuteStart
			state.minuteRequests = 0
		}
	}
	if maxConcurrency > 0 && state.activeRequests >= maxConcurrency {
		c.mu.Unlock()
		return nil, nil, domain.ErrAccountConcurrency
	}
	if countRPM && rpmLimit > 0 && state.minuteRequests+state.pendingMinuteRequests[minuteKey] >= rpmLimit {
		c.mu.Unlock()
		return nil, nil, domain.ErrAccountRateLimited
	}
	if state.activeRequests == 0 {
		state.idle = make(chan struct{})
	}
	state.activeRequests++
	if countRPM {
		state.pendingMinuteRequests[minuteKey]++
	}
	c.signalChangedLocked()
	c.mu.Unlock()

	committed := false
	released := false
	decrementPending := func() {
		if !countRPM {
			return
		}
		state.pendingMinuteRequests[minuteKey]--
		if state.pendingMinuteRequests[minuteKey] == 0 {
			delete(state.pendingMinuteRequests, minuteKey)
		}
	}
	commit = func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if committed || released {
			return
		}
		committed = true
		decrementPending()
		// A preflight that crosses a minute boundary consumed an admission in
		// its original minute. Do not charge it to the newer active window.
		if countRPM && state.minuteStart.Equal(minuteStart) {
			state.minuteRequests++
		}
	}
	release = func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if released {
			return
		}
		released = true
		if !committed {
			decrementPending()
		}
		state.activeRequests--
		if state.activeRequests == 0 {
			close(state.idle)
		}
		c.signalChangedLocked()
	}
	return commit, release, nil
}

func (c *accountTrafficController) quiesce(ctx context.Context, accountIDs ...string) (func(), error) {
	unique := make(map[string]struct{}, len(accountIDs))
	ids := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID == "" {
			continue
		}
		if _, exists := unique[accountID]; exists {
			continue
		}
		unique[accountID] = struct{}{}
		ids = append(ids, accountID)
	}
	sort.Strings(ids)

	locked := make([]*accountTrafficState, 0, len(ids))
	for _, accountID := range ids {
		for {
			c.mu.Lock()
			state := c.states[accountID]
			if state == nil {
				state = &accountTrafficState{idle: closedSignal()}
				c.states[accountID] = state
			}
			if !state.quiescing {
				state.quiescing = true
				c.signalChangedLocked()
				idle := state.idle
				c.mu.Unlock()
				select {
				case <-idle:
					locked = append(locked, state)
				case <-ctx.Done():
					c.releaseQuiesced(locked)
					c.mu.Lock()
					state.quiescing = false
					c.signalChangedLocked()
					c.mu.Unlock()
					return nil, ctx.Err()
				}
				break
			}
			changed := c.changed
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				c.releaseQuiesced(locked)
				return nil, ctx.Err()
			case <-changed:
			}
		}
	}
	var once sync.Once
	return func() { once.Do(func() { c.releaseQuiesced(locked) }) }, nil
}

func (c *accountTrafficController) quiesceBinding(ctx context.Context, planID string, binding func(context.Context) (domain.Plan, error), additionalAccountIDs ...string) (domain.Plan, func(), error) {
	for {
		plan, err := binding(ctx)
		if err != nil {
			return domain.Plan{}, nil, err
		}
		accountIDs := append([]string{plan.AccountID}, additionalAccountIDs...)
		release, err := c.quiesce(ctx, accountIDs...)
		if err != nil {
			return domain.Plan{}, nil, domain.ErrAccountUnavailable
		}
		current, err := binding(ctx)
		if err != nil {
			release()
			return domain.Plan{}, nil, err
		}
		if current.ID == planID && current.AccountID == plan.AccountID && current.AccountBindingGeneration == plan.AccountBindingGeneration {
			return current, release, nil
		}
		release()
	}
}

func (c *accountTrafficController) releaseQuiesced(states []*accountTrafficState) {
	c.mu.Lock()
	for _, state := range states {
		state.quiescing = false
	}
	c.signalChangedLocked()
	c.mu.Unlock()
}

func (c *accountTrafficController) signalChangedLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func closedSignal() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
