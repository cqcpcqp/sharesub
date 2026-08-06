package application

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type PlanQuotaProbe struct {
	AccountID        string `json:"account_id"`
	OwnerMemberID    string `json:"owner_member_id"`
	AccessToken      string `json:"-"`
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	ProxyURL         string `json:"-"`
}

const automaticQuotaProbeTTL = 10 * time.Minute
const maxPlanDescriptionLength = 2000

func (s *Service) RenamePlan(ctx context.Context, ownerID, planID, name string) (domain.Plan, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(ownerID, "plan.renamed", "plan", planID, map[string]string{"name": name})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.RenamePlan(ctx, planID, ownerID, name, event)
}

func (s *Service) UpdatePlanDescription(ctx context.Context, ownerID, planID, description string) (domain.Plan, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > maxPlanDescriptionLength {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(ownerID, "plan.description_updated", "plan", planID, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.UpdatePlanDescription(ctx, planID, ownerID, description, event)
}

func (s *Service) UpdatePlanStatus(ctx context.Context, ownerID, planID, status string) (domain.Plan, error) {
	if status != domain.StatusActive && status != domain.StatusArchived {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	action := "plan.restored"
	if status == domain.StatusArchived {
		action = "plan.archived"
	}
	event, err := s.newAuditEvent(ownerID, action, "plan", planID, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.UpdatePlanStatus(ctx, planID, ownerID, status, event)
}

func (s *Service) DeletePlan(ctx context.Context, ownerID, planID string) error {
	event, err := s.newAuditEvent(ownerID, "plan.deleted", "plan", planID, nil)
	if err != nil {
		return err
	}
	return s.store.DeletePlan(ctx, planID, ownerID, event)
}

func (s *Service) TransferPlanOwnership(ctx context.Context, ownerID, planID, memberID string) (domain.Plan, error) {
	if strings.TrimSpace(memberID) == "" {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(ownerID, "plan.owner_transferred", "plan", planID, map[string]string{"member_id": memberID})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.TransferPlanOwnership(ctx, planID, ownerID, memberID, event)
}

func (s *Service) RebindPlanAccount(ctx context.Context, ownerID, planID, accountID string) (domain.Plan, error) {
	if strings.TrimSpace(accountID) == "" {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(ownerID, "plan.account_rebound", "plan", planID, map[string]string{"account_id": accountID})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.RebindPlanAccount(ctx, planID, ownerID, accountID, event)
}

func (s *Service) RemovePlanMember(ctx context.Context, actorUserID, planID, memberID string) error {
	if strings.TrimSpace(memberID) == "" {
		return domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(actorUserID, "member.removed", "plan", planID, map[string]string{"member_id": memberID})
	if err != nil {
		return err
	}
	return s.store.RemovePlanMember(ctx, planID, actorUserID, memberID, event)
}

func (s *Service) ListPlanAuditEvents(ctx context.Context, userID, planID string) ([]domain.AuditEvent, error) {
	return s.store.ListPlanAuditEvents(ctx, planID, userID)
}

func (s *Service) ListNotifications(ctx context.Context, userID string) (domain.NotificationList, error) {
	return s.store.ListNotifications(ctx, userID)
}

func (s *Service) UpdateNotification(ctx context.Context, userID, notificationID string, read bool) (domain.Notification, error) {
	return s.store.UpdateNotification(ctx, userID, notificationID, read, s.now())
}

func (s *Service) ReadAllNotifications(ctx context.Context, userID string) (int64, error) {
	return s.store.ReadAllNotifications(ctx, userID, s.now())
}

func (s *Service) PreparePlanQuotaProbe(ctx context.Context, ownerID, planID string) (PlanQuotaProbe, error) {
	credential, err := s.store.PlanQuotaCredential(ctx, planID, ownerID)
	if err != nil {
		return PlanQuotaProbe{}, err
	}
	return s.preparePlanQuotaProbe(ctx, credential)
}

func (s *Service) PreparePlanQuotaProbeForMember(ctx context.Context, userID, planID string) (PlanQuotaProbe, error) {
	credential, err := s.store.PlanQuotaCredentialForMember(ctx, planID, userID)
	if err != nil {
		return PlanQuotaProbe{}, err
	}
	return s.preparePlanQuotaProbe(ctx, credential)
}

func (s *Service) PrepareAutomaticPlanQuotaProbe(ctx context.Context, userID, planID string) (PlanQuotaProbe, bool, error) {
	credential, err := s.store.PlanQuotaCredentialForMember(ctx, planID, userID)
	if err != nil {
		return PlanQuotaProbe{}, false, err
	}
	updatedAt, err := s.store.AccountQuotaUpdatedAt(ctx, credential.AccountID)
	if err != nil {
		return PlanQuotaProbe{}, false, err
	}
	if s.now().Sub(updatedAt) < automaticQuotaProbeTTL {
		return PlanQuotaProbe{AccountID: credential.AccountID}, false, nil
	}
	probe, err := s.preparePlanQuotaProbe(ctx, credential)
	return probe, true, err
}

func (s *Service) preparePlanQuotaProbe(ctx context.Context, credential domain.PlanQuotaCredential) (PlanQuotaProbe, error) {
	scope := credential.AccountOwnerUserID + ":" + credential.ChatGPTAccountID
	accessToken, err := s.security.Decrypt(credential.AccessTokenCiphertext, []byte(scope+":access"))
	if err != nil {
		return PlanQuotaProbe{}, err
	}
	proxyURL := ""
	if len(credential.ProxyURLCiphertext) > 0 {
		proxyURL, err = s.security.Decrypt(credential.ProxyURLCiphertext, []byte(scope+":proxy"))
		if err != nil {
			return PlanQuotaProbe{}, err
		}
	}
	if !credential.TokenExpiresAt.After(s.now().Add(2 * time.Minute)) {
		refreshToken, err := s.security.Decrypt(credential.RefreshTokenCiphertext, []byte(scope+":refresh"))
		if err != nil {
			return PlanQuotaProbe{}, err
		}
		refreshed, err := s.oauth.Refresh(ctx, refreshToken)
		if err != nil {
			_ = s.store.MarkAccountError(ctx, credential.AccountID, err.Error())
			return PlanQuotaProbe{}, domain.ErrAccountUnavailable
		}
		access, err := s.security.Encrypt(refreshed.AccessToken, []byte(scope+":access"))
		if err != nil {
			return PlanQuotaProbe{}, err
		}
		refresh, err := s.security.Encrypt(refreshed.RefreshToken, []byte(scope+":refresh"))
		if err != nil {
			return PlanQuotaProbe{}, err
		}
		if err := s.store.UpdateAccountTokens(ctx, credential.AccountID, access, refresh, refreshed.ExpiresAt); err != nil {
			return PlanQuotaProbe{}, err
		}
		accessToken = refreshed.AccessToken
	}
	return PlanQuotaProbe{AccountID: credential.AccountID, OwnerMemberID: credential.OwnerMemberID, AccessToken: accessToken, ChatGPTAccountID: credential.ChatGPTAccountID, ProxyURL: proxyURL}, nil
}

func (s *Service) RecordManualQuotaSignals(ctx context.Context, ownerID, planID string, signals []domain.QuotaSignal) error {
	credential, err := s.store.PlanQuotaCredential(ctx, planID, ownerID)
	if err != nil {
		return err
	}
	if len(signals) == 0 {
		return domain.ErrInvalidInput
	}
	return s.store.RecordQuotaSignals(ctx, credential.AccountID, credential.OwnerMemberID, signals, "", s.now())
}

func (s *Service) RecordAutomaticQuotaSignals(ctx context.Context, userID, planID string, signals []domain.QuotaSignal) error {
	credential, err := s.store.PlanQuotaCredentialForMember(ctx, planID, userID)
	if err != nil {
		return err
	}
	if len(signals) == 0 {
		return domain.ErrInvalidInput
	}
	return s.store.RecordQuotaSignals(ctx, credential.AccountID, credential.OwnerMemberID, signals, "", s.now())
}

func (s *Service) RecordResetQuotaSignals(ctx context.Context, ownerID, planID string, signals []domain.QuotaSignal) error {
	credential, err := s.store.PlanQuotaCredential(ctx, planID, ownerID)
	if err != nil {
		return err
	}
	if len(signals) == 0 {
		return domain.ErrInvalidInput
	}
	return s.store.RecordQuotaResetSignals(ctx, credential.AccountID, signals, s.now())
}
