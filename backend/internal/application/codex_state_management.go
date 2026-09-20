package application

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type CodexStateStatus struct {
	Enabled   bool                    `json:"enabled"`
	Supported bool                    `json:"supported"`
	Models    []CodexStateModelStatus `json:"models"`
}
type CodexStateModelStatus struct {
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	Result        string     `json:"result"`
	ExpiresAt     *time.Time `json:"expires_at"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	Attempts      int        `json:"attempts"`
}

func (s *Service) SetCodexStateService(state *CodexStateService) {
	state.traffic = s.traffic
	s.codexState = state
}
func (s *Service) AccountCodexState(ctx context.Context, user domain.User, accountID string, refresh bool) (CodexStateStatus, error) {
	a, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return CodexStateStatus{}, err
	}
	if a.OwnerUserID != user.ID && user.Role != domain.RoleAdmin {
		return CodexStateStatus{}, domain.ErrForbidden
	}
	result := CodexStateStatus{Enabled: a.StateEnabled, Supported: statePlan(a.PlanType) != "", Models: make([]CodexStateModelStatus, 0)}
	if s.codexState == nil {
		return result, nil
	}
	if refresh {
		if !a.StateEnabled {
			return result, domain.ErrInvalidInput
		}
		if err = s.codexState.store.RefreshStateJobs(ctx, accountID); err != nil {
			return result, err
		}
	}
	jobs, err := s.codexState.store.ListStateJobs(ctx, accountID)
	if err != nil {
		return result, err
	}
	for _, j := range jobs {
		status := "pending"
		if j.Result != "pending" {
			status = "unavailable"
		}
		if j.Version != "" && j.ExpiresAt != nil && j.ExpiresAt.After(s.now()) && j.Fingerprint == s.codexState.fingerprint(a) {
			status = "ready"
		}
		result.Models = append(result.Models, CodexStateModelStatus{Model: j.Model, Status: status, Result: j.Result, ExpiresAt: j.ExpiresAt, NextAttemptAt: j.NextAttemptAt, Attempts: j.Attempts})
	}
	return result, nil
}
