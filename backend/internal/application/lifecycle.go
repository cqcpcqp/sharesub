package application

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type PlanQuotaProbe struct {
	PlanID                   string    `json:"-"`
	AccountID                string    `json:"account_id"`
	AccountBindingGeneration int64     `json:"-"`
	StartedAt                time.Time `json:"-"`
	AccessToken              string    `json:"-"`
	ChatGPTAccountID         string    `json:"chatgpt_account_id"`
	ProxyURL                 string    `json:"-"`
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
	_, release, err := s.quiescePlanBinding(ctx, ownerID, planID)
	if err != nil {
		return err
	}
	defer release()
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
	_, release, err := s.quiescePlanBinding(ctx, ownerID, planID, accountID)
	if err != nil {
		return domain.Plan{}, err
	}
	defer release()
	account, signals, observedAt, err := s.probeAccountQuota(ctx, ownerID, accountID)
	if err != nil {
		return domain.Plan{}, err
	}
	event, err := s.newAuditEvent(ownerID, "plan.account_rebound", "plan", planID, map[string]string{"account_id": accountID})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.RebindPlanAccount(ctx, planID, ownerID, account.ID, signals, observedAt, event)
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

func (s *Service) PrepareAutomaticPlanQuotaProbe(ctx context.Context, userID, planID string) (PlanQuotaProbe, bool, func(), error) {
	credential, err := s.store.PlanQuotaCredentialForMember(ctx, planID, userID)
	if err != nil {
		return PlanQuotaProbe{}, false, nil, err
	}
	updatedAt, err := s.store.AccountQuotaUpdatedAt(ctx, credential.AccountID)
	if err != nil {
		return PlanQuotaProbe{}, false, nil, err
	}
	if s.now().Sub(updatedAt) < automaticQuotaProbeTTL {
		return quotaProbeForCredential(credential, s.now()), false, func() {}, nil
	}
	probe, release, err := s.reserveQuotaProbe(ctx, userID, credential)
	return probe, true, release, err
}

func (s *Service) ReservePlanQuotaProbe(ctx context.Context, ownerID, planID string) (PlanQuotaProbe, func(), error) {
	credential, err := s.store.PlanQuotaCredential(ctx, planID, ownerID)
	if err != nil {
		return PlanQuotaProbe{}, nil, err
	}
	return s.reserveQuotaProbe(ctx, ownerID, credential)
}

func (s *Service) reserveQuotaProbe(ctx context.Context, userID string, credential domain.PlanQuotaCredential) (PlanQuotaProbe, func(), error) {
	release, err := s.reserveAccountTraffic(ctx, credential.AccountID)
	if err != nil {
		return PlanQuotaProbe{}, nil, err
	}
	current, err := s.store.PlanQuotaCredentialForMember(ctx, credential.PlanID, userID)
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
		accessToken, _, err = s.refreshAccountToken(ctx, domain.Account{
			ID: credential.AccountID, OwnerUserID: credential.AccountOwnerUserID,
			ChatGPTAccountID: credential.ChatGPTAccountID, Status: domain.StatusActive,
			AccessTokenCiphertext:  credential.AccessTokenCiphertext,
			RefreshTokenCiphertext: credential.RefreshTokenCiphertext,
			TokenExpiresAt:         credential.TokenExpiresAt,
		}, 2*time.Minute, true)
		if err != nil {
			return PlanQuotaProbe{}, domain.ErrAccountUnavailable
		}
	}
	probe := quotaProbeForCredential(credential, s.now())
	probe.AccessToken = accessToken
	probe.ChatGPTAccountID = credential.ChatGPTAccountID
	probe.ProxyURL = proxyURL
	return probe, nil
}

func quotaProbeForCredential(credential domain.PlanQuotaCredential, startedAt time.Time) PlanQuotaProbe {
	return PlanQuotaProbe{
		PlanID:                   credential.PlanID,
		AccountID:                credential.AccountID,
		AccountBindingGeneration: credential.AccountBindingGeneration,
		StartedAt:                startedAt,
	}
}

func validQuotaProbeBinding(probe PlanQuotaProbe) bool {
	return strings.TrimSpace(probe.PlanID) != "" &&
		strings.TrimSpace(probe.AccountID) != "" &&
		probe.AccountBindingGeneration >= 0 &&
		!probe.StartedAt.IsZero()
}

func (s *Service) RecordManualQuotaSignals(ctx context.Context, probe PlanQuotaProbe, signals []domain.QuotaSignal) error {
	if !validQuotaProbeBinding(probe) || !hasRequiredQuotaWindows(signals) {
		return domain.ErrInvalidInput
	}
	return s.store.RecordProbedAccountQuotaSignals(ctx, probe.PlanID, probe.AccountID, probe.AccountBindingGeneration, signals, probe.StartedAt)
}

func (s *Service) RecordAutomaticQuotaSignals(ctx context.Context, probe PlanQuotaProbe, signals []domain.QuotaSignal) error {
	if !validQuotaProbeBinding(probe) || !hasRequiredQuotaWindows(signals) {
		return domain.ErrInvalidInput
	}
	return s.store.RecordProbedAccountQuotaSignals(ctx, probe.PlanID, probe.AccountID, probe.AccountBindingGeneration, signals, probe.StartedAt)
}

func (s *Service) RecordResetQuotaSignals(ctx context.Context, probe PlanQuotaProbe, signals []domain.QuotaSignal) error {
	if !validQuotaProbeBinding(probe) || !hasRequiredQuotaWindows(signals) {
		return domain.ErrInvalidInput
	}
	return s.store.RecordQuotaResetSignals(ctx, probe.PlanID, probe.AccountID, probe.AccountBindingGeneration, signals, s.now())
}

func (s *Service) QuiescePlanQuota(ctx context.Context, ownerID, planID string) (PlanQuotaProbe, func(), error) {
	plan, release, err := s.quiescePlanBinding(ctx, ownerID, planID)
	if err != nil {
		return PlanQuotaProbe{}, nil, err
	}
	credential, err := s.store.PlanQuotaCredential(ctx, planID, ownerID)
	if err != nil {
		release()
		return PlanQuotaProbe{}, nil, err
	}
	if credential.AccountID != plan.AccountID || credential.AccountBindingGeneration != plan.AccountBindingGeneration {
		release()
		return PlanQuotaProbe{}, nil, domain.ErrConflict
	}
	probe, err := s.preparePlanQuotaProbe(ctx, credential)
	if err != nil {
		release()
		return PlanQuotaProbe{}, nil, err
	}
	return probe, release, nil
}

func (s *Service) probeAccountQuota(ctx context.Context, ownerID, accountID string) (domain.Account, []domain.QuotaSignal, time.Time, error) {
	account, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return domain.Account{}, nil, time.Time{}, err
	}
	if account.OwnerUserID != ownerID {
		return domain.Account{}, nil, time.Time{}, domain.ErrForbidden
	}
	if account.Status != domain.StatusActive {
		return domain.Account{}, nil, time.Time{}, domain.ErrAccountUnavailable
	}
	if s.quotaProber == nil {
		return domain.Account{}, nil, time.Time{}, domain.ErrAccountUnavailable
	}
	accessToken, err := s.decryptAccountAccessToken(account)
	if err != nil {
		return domain.Account{}, nil, time.Time{}, err
	}
	if !account.TokenExpiresAt.After(s.now().Add(2 * time.Minute)) {
		accessToken, _, err = s.refreshAccountToken(ctx, account, 2*time.Minute, true)
		if err != nil {
			return domain.Account{}, nil, time.Time{}, domain.ErrAccountUnavailable
		}
	}
	if err := s.hydrateAccountProxy(&account); err != nil {
		return domain.Account{}, nil, time.Time{}, err
	}
	signals, err := s.quotaProber.ProbeQuota(ctx, accessToken, account.ChatGPTAccountID, account.ProxyURL)
	if err != nil {
		return domain.Account{}, nil, time.Time{}, err
	}
	if !hasRequiredQuotaWindows(signals) {
		return domain.Account{}, nil, time.Time{}, domain.ErrAccountUnavailable
	}
	return account, signals, s.now(), nil
}

func hasRequiredQuotaWindows(signals []domain.QuotaSignal) bool {
	if len(signals) == 0 || len(signals) > 2 {
		return false
	}
	has5H := false
	has7D := false
	for _, signal := range signals {
		switch signal.WindowType {
		case domain.Window5H:
			if has5H {
				return false
			}
			has5H = true
		case domain.Window7D:
			if has7D {
				return false
			}
			has7D = true
		default:
			return false
		}
	}
	return has7D
}

func (s *Service) quiesceAccounts(ctx context.Context, accountIDs ...string) (func(), error) {
	if s.traffic == nil {
		return func() {}, nil
	}
	release, err := s.traffic.quiesce(ctx, accountIDs...)
	if err != nil {
		return nil, domain.ErrAccountUnavailable
	}
	return release, nil
}

func (s *Service) reserveAccountTraffic(ctx context.Context, accountID string) (func(), error) {
	if s.traffic == nil {
		return func() {}, nil
	}
	release, err := s.traffic.reserve(accountID, s.now())
	if err != nil {
		return nil, err
	}
	return release, nil
}

func (s *Service) quiescePlanBinding(ctx context.Context, ownerID, planID string, additionalAccountIDs ...string) (domain.Plan, func(), error) {
	if s.traffic == nil {
		plan, err := s.store.PlanBinding(ctx, planID, ownerID)
		return plan, func() {}, err
	}
	plan, release, err := s.traffic.quiesceBinding(ctx, planID, func(ctx context.Context) (domain.Plan, error) {
		return s.store.PlanBinding(ctx, planID, ownerID)
	}, additionalAccountIDs...)
	if err != nil {
		return domain.Plan{}, nil, err
	}
	return plan, release, nil
}
