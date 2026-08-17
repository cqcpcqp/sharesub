package application

import (
	"context"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type adminStore struct {
	Store
	users               []domain.AdminUser
	accounts            []domain.AdminAccount
	plans               []domain.AdminPlan
	account             domain.Account
	updatedAccount      domain.Account
	accountEvent        domain.AuditEvent
	planOwnerID         string
	planAccountID       string
	planEvent           domain.AuditEvent
	planQueryUserID     string
	planMemberID        string
	planAllocations     []domain.MemberShareAllocation
	reviewPlanID        string
	reviewApplicationID string
	updatedUserID       string
	updatedStatus       string
	metricsStarted      time.Time
	adminPlanLookups    int
	bootstrapUser       domain.User
	bootstrapMade       bool
	passwordUserID      string
	passwordHash        string
	passwordMust        bool
	passwordSessionHash []byte
}

type adminRuntimeStatusProvider struct{ status domain.AdminRuntimeStatus }

func (p adminRuntimeStatusProvider) Snapshot(context.Context) domain.AdminRuntimeStatus {
	return p.status
}

func (s *adminStore) AdminListUsers(context.Context) ([]domain.AdminUser, error) { return s.users, nil }
func (s *adminStore) AdminUpdateUserStatus(_ context.Context, userID, status string) (domain.User, error) {
	s.updatedUserID, s.updatedStatus = userID, status
	return domain.User{ID: userID, Email: "member@example.com", Status: status}, nil
}
func (s *adminStore) AdminOverview(_ context.Context, metricsStart time.Time) (domain.AdminOverview, error) {
	s.metricsStarted = metricsStart
	return domain.AdminOverview{UserCount: 3}, nil
}
func (s *adminStore) AdminListAccounts(context.Context) ([]domain.AdminAccount, error) {
	return s.accounts, nil
}
func (s *adminStore) AccountByID(context.Context, string) (domain.Account, error) {
	return s.account, nil
}
func (s *adminStore) UpdateAccountConfig(_ context.Context, _ string, account domain.Account, event domain.AuditEvent) (domain.Account, error) {
	s.updatedAccount = account
	s.accountEvent = event
	for index := range s.accounts {
		if s.accounts[index].ID == account.ID {
			s.accounts[index].Account = account
			return account, nil
		}
	}
	return account, nil
}
func (s *adminStore) AdminUpdateAccountStatus(_ context.Context, accountID, status string, event domain.AuditEvent) (domain.AdminAccount, error) {
	s.accountEvent = event
	for index := range s.accounts {
		if s.accounts[index].ID == accountID {
			s.accounts[index].Status = status
			return s.accounts[index], nil
		}
	}
	return domain.AdminAccount{}, domain.ErrNotFound
}
func (s *adminStore) RecordAuditEvent(_ context.Context, event domain.AuditEvent) error {
	s.planEvent = event
	return nil
}
func (s *adminStore) AdminListPlans(context.Context, time.Time) ([]domain.AdminPlan, error) {
	return s.plans, nil
}
func (s *adminStore) AdminPlanByID(_ context.Context, planID string) (domain.Plan, error) {
	s.adminPlanLookups++
	for _, plan := range s.plans {
		if plan.ID == planID {
			return plan.Plan, nil
		}
	}
	return domain.Plan{}, domain.ErrNotFound
}
func (s *adminStore) PlanBinding(_ context.Context, planID, ownerID string) (domain.Plan, error) {
	for _, plan := range s.plans {
		if plan.ID == planID && plan.OwnerUserID == ownerID {
			return plan.Plan, nil
		}
	}
	return domain.Plan{}, domain.ErrNotFound
}
func (s *adminStore) PlanDetail(_ context.Context, planID, userID string, _ time.Time, _ time.Time) (domain.PlanDetail, error) {
	s.planQueryUserID = userID
	return domain.PlanDetail{Plan: domain.Plan{ID: planID, OwnerUserID: userID}}, nil
}
func (s *adminStore) TransferPlanOwnership(_ context.Context, planID, ownerID, memberID string, event domain.AuditEvent) (domain.Plan, error) {
	s.planOwnerID, s.planMemberID, s.planEvent = ownerID, memberID, event
	return domain.Plan{ID: planID, OwnerUserID: "new-owner"}, nil
}
func (s *adminStore) UpdateMemberShare(_ context.Context, planID, ownerID, memberID string, share int, event domain.AuditEvent) (domain.Member, error) {
	s.planOwnerID, s.planMemberID, s.planEvent = ownerID, memberID, event
	return domain.Member{ID: memberID, PlanID: planID, ShareBasisPoints: share}, nil
}
func (s *adminStore) ConvertPlanToFixed(_ context.Context, planID, ownerID string, allocations []domain.MemberShareAllocation, event domain.AuditEvent) (domain.Plan, error) {
	s.planOwnerID, s.planAllocations, s.planEvent = ownerID, allocations, event
	return domain.Plan{ID: planID, OwnerUserID: ownerID, AllocationMode: domain.AllocationFixed}, nil
}
func (s *adminStore) ReviewJoinApplication(_ context.Context, ownerID, expectedPlanID, applicationID string, approve bool, _ string, _ time.Time, event domain.AuditEvent) (domain.JoinApplication, error) {
	s.planOwnerID, s.reviewPlanID, s.reviewApplicationID, s.planEvent = ownerID, expectedPlanID, applicationID, event
	return domain.JoinApplication{ID: applicationID, PlanID: expectedPlanID}, nil
}
func (s *adminStore) RebindPlanAccount(_ context.Context, planID, ownerID, accountID string, _ []domain.QuotaSignal, _ time.Time, event domain.AuditEvent) (domain.Plan, error) {
	s.planOwnerID, s.planAccountID, s.planEvent = ownerID, accountID, event
	return domain.Plan{ID: planID, OwnerUserID: ownerID, AccountID: accountID}, nil
}
func (s *adminStore) EnsureBootstrapAdmin(_ context.Context, user domain.User) (bool, error) {
	s.bootstrapUser = user
	return s.bootstrapMade, nil
}
func (s *adminStore) UpdatePassword(_ context.Context, userID, hash string, mustChange bool, currentSessionHash []byte) (domain.User, error) {
	s.passwordUserID, s.passwordHash, s.passwordMust, s.passwordSessionHash = userID, hash, mustChange, currentSessionHash
	return domain.User{ID: userID, Email: BootstrapAdminEmail, Role: domain.RoleAdmin, Status: domain.StatusActive}, nil
}

func TestPersistedAdminIdentityAndAuthorization(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store := &adminStore{users: []domain.AdminUser{{User: domain.User{ID: "admin", Email: "ADMIN@example.com", Role: domain.RoleAdmin}}, {User: domain.User{ID: "member", Email: "member@example.com", Role: domain.RoleUser}}}}
	service := NewService(store, nil, nil, 0, "", "")
	service.SetRuntimeStatusProvider(adminRuntimeStatusProvider{status: domain.AdminRuntimeStatus{JobsStatus: "healthy"}})
	service.now = func() time.Time { return now }
	admin := service.decorateUser(domain.User{ID: "admin", Email: "admin@example.com", Role: domain.RoleAdmin})
	member := service.decorateUser(domain.User{ID: "member", Email: "member@example.com", Role: domain.RoleUser})
	if !admin.IsAdmin || member.IsAdmin {
		t.Fatalf("admin = %+v, member = %+v", admin, member)
	}
	overview, err := service.AdminOverview(context.Background(), admin)
	if err != nil || overview.UserCount != 3 || overview.Runtime.JobsStatus != "healthy" || !store.metricsStarted.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("overview = %+v, start = %s, error = %v", overview, store.metricsStarted, err)
	}
	if _, err := service.AdminOverview(context.Background(), member); err != domain.ErrForbidden {
		t.Fatalf("member admin overview error = %v", err)
	}
	users, err := service.AdminListUsers(context.Background(), admin)
	if err != nil || !users[0].IsAdmin || users[1].IsAdmin {
		t.Fatalf("users = %+v, error = %v", users, err)
	}
}

func TestAdminUserStatusValidation(t *testing.T) {
	store := &adminStore{}
	service := NewService(store, nil, nil, 0, "", "")
	admin := service.decorateUser(domain.User{ID: "admin", Email: "admin@example.com", Role: domain.RoleAdmin})
	if _, err := service.AdminUpdateUserStatus(context.Background(), admin, admin.ID, domain.StatusDisabled); err != domain.ErrInvalidInput {
		t.Fatalf("self-disable error = %v", err)
	}
	if _, err := service.AdminUpdateUserStatus(context.Background(), admin, "member", "removed"); err != domain.ErrInvalidInput {
		t.Fatalf("invalid status error = %v", err)
	}
	updated, err := service.AdminUpdateUserStatus(context.Background(), admin, "member", domain.StatusDisabled)
	if err != nil || updated.Status != domain.StatusDisabled || store.updatedUserID != "member" || store.updatedStatus != domain.StatusDisabled {
		t.Fatalf("updated = %+v, store = %+v, error = %v", updated, store, err)
	}
}

func TestAdminCanUpdateAnotherUsersAccountConfig(t *testing.T) {
	account := domain.Account{ID: "account", OwnerUserID: "owner", Name: "旧名称", ChatGPTAccountID: "chatgpt", Status: domain.StatusActive}
	store := &adminStore{account: account, accounts: []domain.AdminAccount{{Account: account, OwnerUsername: "房主"}}}
	service := NewService(store, nil, nil, 0, "", "")
	admin := service.decorateUser(domain.User{ID: "admin", Role: domain.RoleAdmin})
	updated, err := service.AdminUpdateAccountConfig(context.Background(), admin, account.ID, AccountConfigInput{Name: "运维名称", Notes: "管理员备注", Status: domain.StatusDisabled})
	if err != nil || updated.Name != "运维名称" || store.updatedAccount.OwnerUserID != "owner" || store.updatedAccount.Status != domain.StatusDisabled || store.accountEvent.ActorUserID != admin.ID {
		t.Fatalf("updated = %+v, stored = %+v, error = %v", updated, store.updatedAccount, err)
	}
	member := service.decorateUser(domain.User{ID: "member", Role: domain.RoleUser})
	if _, err := service.AdminUpdateAccountConfig(context.Background(), member, account.ID, AccountConfigInput{Name: "拒绝", Status: domain.StatusActive}); err != domain.ErrForbidden {
		t.Fatalf("member account update error = %v", err)
	}
}

func TestAdminCanBindPlanOwnersAvailableAccount(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	access, err := manager.Encrypt("access", []byte("owner:chatgpt:access"))
	if err != nil {
		t.Fatal(err)
	}
	store := &adminStore{
		plans:   []domain.AdminPlan{{Plan: domain.Plan{ID: "plan", OwnerUserID: "owner"}}},
		account: domain.Account{ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt", AccessTokenCiphertext: access, TokenExpiresAt: now.Add(time.Hour), Status: domain.StatusActive},
	}
	service := NewService(store, manager, nil, 0, "", "", &staticQuotaProber{signals: completeQuotaSignals(now)})
	service.now = func() time.Time { return now }
	admin := service.decorateUser(domain.User{ID: "admin", Role: domain.RoleAdmin})
	updated, err := service.AdminRebindPlanAccount(context.Background(), admin, "plan", "account")
	if err != nil || updated.AccountID != "account" || store.planOwnerID != "owner" || store.planAccountID != "account" || store.planEvent.ActorUserID != admin.ID {
		t.Fatalf("updated = %+v, owner = %q, account = %q, event = %+v, error = %v", updated, store.planOwnerID, store.planAccountID, store.planEvent, err)
	}
}

func TestAdminPlanAccessKeepsOwnerScopeAndAdminAuditActor(t *testing.T) {
	store := &adminStore{plans: []domain.AdminPlan{{Plan: domain.Plan{ID: "plan", OwnerUserID: "owner"}}}}
	service := NewService(store, nil, nil, 0, "", "")
	admin := service.decorateUser(domain.User{ID: "admin", Role: domain.RoleAdmin})
	detail, err := service.AdminPlanDetail(context.Background(), admin, "plan", "UTC")
	if err != nil || detail.Plan.ID != "plan" || store.planQueryUserID != "owner" || store.adminPlanLookups != 1 {
		t.Fatalf("detail = %+v, query user = %q, error = %v", detail, store.planQueryUserID, err)
	}
	updated, err := service.AdminTransferPlanOwnership(context.Background(), admin, "plan", "member")
	if err != nil || updated.OwnerUserID != "new-owner" || store.planOwnerID != "owner" || store.planMemberID != "member" || store.planEvent.ActorUserID != admin.ID {
		t.Fatalf("updated = %+v, owner = %q, member = %q, event = %+v, error = %v", updated, store.planOwnerID, store.planMemberID, store.planEvent, err)
	}
	member, err := service.AdminUpdateMemberShare(context.Background(), admin, "plan", "member", 2500)
	if err != nil || member.ShareBasisPoints != 2500 || store.planOwnerID != "owner" || store.planEvent.ActorUserID != admin.ID {
		t.Fatalf("member = %+v, owner = %q, event = %+v, error = %v", member, store.planOwnerID, store.planEvent, err)
	}
	converted, err := service.AdminConvertPlanToFixed(context.Background(), admin, "plan", []domain.MemberShareAllocation{{MemberID: "member", ShareBasisPoints: 2500}})
	if err != nil || converted.AllocationMode != domain.AllocationFixed || store.planOwnerID != "owner" || len(store.planAllocations) != 1 || store.planEvent.ActorUserID != admin.ID {
		t.Fatalf("converted = %+v, owner = %q, allocations = %+v, event = %+v, error = %v", converted, store.planOwnerID, store.planAllocations, store.planEvent, err)
	}
	application, err := service.AdminReviewJoinApplication(context.Background(), admin, "plan", "application", true)
	if err != nil || application.PlanID != "plan" || store.reviewPlanID != "plan" || store.reviewApplicationID != "application" || store.planOwnerID != "owner" || store.planEvent.ActorUserID != admin.ID {
		t.Fatalf("application = %+v, plan = %q, application id = %q, owner = %q, event = %+v, error = %v", application, store.reviewPlanID, store.reviewApplicationID, store.planOwnerID, store.planEvent, err)
	}
}

func TestBootstrapAdminIsCreatedOnceWithTemporaryPassword(t *testing.T) {
	store := &adminStore{bootstrapMade: true}
	service := NewService(store, nil, nil, 0, "", "")
	credential, err := service.EnsureBootstrapAdmin(context.Background())
	if err != nil || credential == nil || credential.Email != BootstrapAdminEmail || len(credential.TemporaryPassword) < 32 {
		t.Fatalf("credential = %+v, error = %v", credential, err)
	}
	if store.bootstrapUser.Role != domain.RoleAdmin || !store.bootstrapUser.MustChangePassword || !security.CheckPassword(store.bootstrapUser.PasswordHash, credential.TemporaryPassword) {
		t.Fatalf("bootstrap user = %+v", store.bootstrapUser)
	}
	store.bootstrapMade = false
	credential, err = service.EnsureBootstrapAdmin(context.Background())
	if err != nil || credential != nil {
		t.Fatalf("existing admin credential = %+v, error = %v", credential, err)
	}
}

func TestAdminMustChangeTemporaryPassword(t *testing.T) {
	oldPassword := "temporary-password"
	hash, err := security.HashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	store := &adminStore{}
	manager, err := security.New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, manager, nil, 0, "", "")
	admin := domain.User{ID: "admin", Email: BootstrapAdminEmail, PasswordHash: hash, Role: domain.RoleAdmin, MustChangePassword: true}
	currentSessionToken := "ss_session_current"
	updated, err := service.ChangePassword(context.Background(), admin, oldPassword, "a-new-secure-password", currentSessionToken)
	if err != nil || updated.MustChangePassword || !updated.IsAdmin || store.passwordUserID != admin.ID || store.passwordMust || !security.CheckPassword(store.passwordHash, "a-new-secure-password") || !manager.EqualTokenHash(currentSessionToken, store.passwordSessionHash) {
		t.Fatalf("updated = %+v, store = %+v, error = %v", updated, store, err)
	}
}
