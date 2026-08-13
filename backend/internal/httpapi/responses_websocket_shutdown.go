package httpapi

import (
	"context"
	"sync"

	"github.com/coder/websocket"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

const responsesWebSocketShutdownReason = "server is shutting down"

type responsesWebSocketActiveSession struct {
	cancel context.CancelCauseFunc
	mu     sync.Mutex
	client openai.ResponsesWebSocketConn
	closed bool
}

type responsesWebSocketShutdownCause struct{}

func (responsesWebSocketShutdownCause) Error() string { return responsesWebSocketShutdownReason }

type responsesWebSocketSessionRegistry struct {
	mu       sync.Mutex
	stopping bool
	active   map[*responsesWebSocketActiveSession]struct{}
	drained  chan struct{}
}

func newResponsesWebSocketSessionRegistry() *responsesWebSocketSessionRegistry {
	return &responsesWebSocketSessionRegistry{active: make(map[*responsesWebSocketActiveSession]struct{})}
}

func (s *responsesWebSocketActiveSession) bindClient(client openai.ResponsesWebSocketConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if client == nil {
		return false
	}
	s.client = client
	return true
}

func (s *responsesWebSocketActiveSession) gracefulClose() {
	s.mu.Lock()
	s.closed = true
	client := s.client
	s.mu.Unlock()
	if client != nil {
		_ = client.Close(websocket.StatusGoingAway, responsesWebSocketShutdownReason)
	}
}

func (s *responsesWebSocketActiveSession) closeNow() {
	s.mu.Lock()
	s.closed = true
	client := s.client
	s.mu.Unlock()
	if client != nil {
		_ = client.CloseNow()
	}
}

func (r *responsesWebSocketSessionRegistry) register(cancel context.CancelCauseFunc) (*responsesWebSocketActiveSession, bool) {
	if cancel == nil {
		cancel = func(error) {}
	}
	if r == nil {
		return &responsesWebSocketActiveSession{cancel: cancel}, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopping {
		return nil, false
	}
	session := &responsesWebSocketActiveSession{cancel: cancel}
	r.active[session] = struct{}{}
	return session, true
}

func (r *responsesWebSocketSessionRegistry) unregister(session *responsesWebSocketActiveSession) {
	if session == nil {
		return
	}
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.active, session)
	if r.stopping && len(r.active) == 0 && r.drained != nil {
		close(r.drained)
		r.drained = nil
	}
	r.mu.Unlock()
}

func (r *responsesWebSocketSessionRegistry) shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessions, drained := r.beginShutdown()
	for _, session := range sessions {
		session.cancel(responsesWebSocketShutdownCause{})
	}
	closeDone := make(chan struct{})
	go func() {
		var closeWG sync.WaitGroup
		closeWG.Add(len(sessions))
		for _, session := range sessions {
			go func(session *responsesWebSocketActiveSession) {
				defer closeWG.Done()
				session.gracefulClose()
			}(session)
		}
		closeWG.Wait()
		close(closeDone)
	}()
	select {
	case <-drained:
		return nil
	case <-closeDone:
	case <-ctx.Done():
		forceCloseResponsesWebSocketSessions(sessions)
		return ctx.Err()
	}
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		forceCloseResponsesWebSocketSessions(sessions)
		return ctx.Err()
	}
}

// forceCloseResponsesWebSocketSessions starts every force-close without
// waiting for a potentially broken connection implementation to return. Once
// the shutdown deadline has elapsed, CloseNow itself must not extend it.
func forceCloseResponsesWebSocketSessions(sessions []*responsesWebSocketActiveSession) {
	var started sync.WaitGroup
	started.Add(len(sessions))
	for _, session := range sessions {
		go func(session *responsesWebSocketActiveSession) {
			started.Done()
			session.closeNow()
		}(session)
	}
	started.Wait()
}

func (r *responsesWebSocketSessionRegistry) closeNow() {
	if r == nil {
		return
	}
	sessions, _ := r.beginShutdown()
	for _, session := range sessions {
		session.cancel(responsesWebSocketShutdownCause{})
		session.closeNow()
	}
}

func (r *responsesWebSocketSessionRegistry) beginShutdown() ([]*responsesWebSocketActiveSession, <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopping = true
	if len(r.active) == 0 {
		drained := make(chan struct{})
		close(drained)
		return nil, drained
	}
	if r.drained == nil {
		r.drained = make(chan struct{})
	}
	sessions := make([]*responsesWebSocketActiveSession, 0, len(r.active))
	for session := range r.active {
		sessions = append(sessions, session)
	}
	return sessions, r.drained
}

func (r *responsesWebSocketSessionRegistry) isStopping() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}
