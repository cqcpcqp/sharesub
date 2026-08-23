package application

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const quotaResetVoteTTL = 2 * time.Hour

func (s *Service) QuotaResetVote(ctx context.Context, userID, planID string) (domain.QuotaResetVoteState, error) {
	vote, err := s.store.QuotaResetVote(ctx, planID, userID, s.now())
	return domain.QuotaResetVoteState{Vote: vote}, err
}

func (s *Service) CreateQuotaResetVote(ctx context.Context, userID, planID string) (domain.QuotaResetVote, bool, error) {
	id, err := security.NewID()
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	event, err := s.newAuditEvent(userID, "plan.quota_reset_vote_started", "plan", planID, nil)
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	now := s.now()
	vote := domain.QuotaResetVote{ID: id, PlanID: planID, InitiatorUserID: userID, Status: "active", CreatedAt: now, ExpiresAt: now.Add(quotaResetVoteTTL)}
	return s.store.CreateQuotaResetVote(ctx, vote, userID, event)
}

func (s *Service) CastQuotaResetVote(ctx context.Context, userID, planID, voteID, choice string) (domain.QuotaResetVote, bool, error) {
	if choice != "support" && choice != "oppose" {
		return domain.QuotaResetVote{}, false, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(userID, "plan.quota_reset_vote_passed", "plan", planID, nil)
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	return s.store.CastQuotaResetVote(ctx, planID, voteID, userID, choice, s.now(), event)
}

func (s *Service) CompleteQuotaResetVote(ctx context.Context, planID, voteID, status string, windowsReset int, resultCode, actorUserID string) (domain.QuotaResetVote, error) {
	if status != "succeeded" && status != "succeeded_unsynced" && status != "outcome_unknown" && status != "cancelled" {
		return domain.QuotaResetVote{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(actorUserID, "plan.quota_reset", "plan", planID, map[string]any{"source": "member_vote", "vote_id": voteID, "status": status})
	if err != nil {
		return domain.QuotaResetVote{}, err
	}
	return s.store.CompleteQuotaResetVote(ctx, voteID, status, windowsReset, resultCode, s.now(), event)
}

func (s *Service) ReserveManualQuotaReset(ctx context.Context, probe PlanQuotaProbe, actorUserID, reason string) (string, error) {
	operationID, err := security.NewID()
	if err != nil {
		return "", err
	}
	event, err := s.newAuditEvent(actorUserID, "plan.quota_reset_vote_cancelled", "plan", probe.PlanID, map[string]string{"reason": reason})
	if err != nil {
		return "", err
	}
	lease := domain.QuotaResetExecutionLease{OperationID: operationID, PlanID: probe.PlanID, AccountID: probe.AccountID, AccountBindingGeneration: probe.AccountBindingGeneration, AcquiredAt: s.now()}
	if err := s.store.ReserveManualQuotaReset(ctx, lease, reason, event); err != nil {
		return "", err
	}
	return operationID, nil
}

func (s *Service) ReserveVotedQuotaReset(ctx context.Context, probe PlanQuotaProbe, voteID string) (string, error) {
	operationID, err := security.NewID()
	if err != nil {
		return "", err
	}
	lease := domain.QuotaResetExecutionLease{OperationID: operationID, PlanID: probe.PlanID, AccountID: probe.AccountID, AccountBindingGeneration: probe.AccountBindingGeneration, VoteID: voteID, AcquiredAt: s.now()}
	if err := s.store.ReserveVotedQuotaReset(ctx, lease); err != nil {
		return "", err
	}
	return operationID, nil
}

func (s *Service) ReleaseQuotaResetExecution(ctx context.Context, planID, operationID string) error {
	return s.store.ReleaseQuotaResetExecution(ctx, planID, operationID)
}

func (s *Service) QuiescePlanQuotaForMember(ctx context.Context, userID, planID string) (PlanQuotaProbe, func(), error) {
	credential, err := s.store.PlanQuotaCredentialForMember(ctx, planID, userID)
	if err != nil {
		return PlanQuotaProbe{}, nil, err
	}
	release := func() {}
	if s.traffic != nil {
		release, err = s.traffic.quiesce(ctx, credential.AccountID)
		if err != nil {
			return PlanQuotaProbe{}, nil, err
		}
	}
	current, err := s.store.PlanQuotaCredentialForMember(ctx, planID, userID)
	if err != nil {
		release()
		return PlanQuotaProbe{}, nil, err
	}
	if current.AccountID != credential.AccountID || current.AccountBindingGeneration != credential.AccountBindingGeneration {
		release()
		return PlanQuotaProbe{}, nil, domain.ErrConflict
	}
	probe, err := s.preparePlanQuotaProbe(ctx, current)
	if err != nil {
		release()
		return PlanQuotaProbe{}, nil, err
	}
	return probe, release, nil
}
