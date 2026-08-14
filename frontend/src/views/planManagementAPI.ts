import { api } from '../api'
import { adminAPI } from '../api/admin'

export function createPlanManagementAPI(adminMode = false) {
  return adminMode ? {
    plan: adminAPI.adminPlan,
    planPerformance: adminAPI.adminPlanPerformance,
    planAuditEvents: adminAPI.adminPlanAuditEvents,
    refreshPlanQuota: (id: string, _automatic = false) => adminAPI.adminRefreshPlanQuota(id),
    planQuotaResetCredits: adminAPI.adminPlanQuotaResetCredits,
    resetPlanQuota: adminAPI.adminResetPlanQuota,
    invite: adminAPI.adminInvite,
    revokeInvite: adminAPI.adminRevokeInvite,
    updatePublication: (id: string, payload: { visibility: string; public_slots: number; public_share_basis_points: number }) => adminAPI.adminUpdatePlanPublication(id, payload.visibility as 'private' | 'public', payload.public_slots, payload.public_share_basis_points),
    updateMember: adminAPI.adminUpdateMember,
    removeMember: adminAPI.adminRemoveMember,
    reviewApplication: adminAPI.adminReviewApplication,
    renamePlan: (id: string, name: string) => adminAPI.adminUpdatePlan(id, { name }),
    updatePlanDescription: (id: string, description: string) => adminAPI.adminUpdatePlan(id, { description }),
    updatePlanStatus: adminAPI.adminUpdatePlanStatus,
    transferPlanOwnership: adminAPI.adminTransferPlanOwnership,
    rebindPlanAccount: adminAPI.adminRebindPlanAccount,
    deletePlan: adminAPI.adminDeletePlan,
  } : {
    plan: api.plan,
    planPerformance: api.planPerformance,
    planAuditEvents: api.planAuditEvents,
    refreshPlanQuota: api.refreshPlanQuota,
    planQuotaResetCredits: api.planQuotaResetCredits,
    resetPlanQuota: api.resetPlanQuota,
    invite: api.invite,
    revokeInvite: api.revokeInvite,
    updatePublication: api.updatePublication,
    updateMember: api.updateMember,
    removeMember: api.removeMember,
    reviewApplication: (_planID: string, id: string, decision: 'approve' | 'reject') => api.reviewApplication(id, decision),
    renamePlan: api.renamePlan,
    updatePlanDescription: api.updatePlanDescription,
    updatePlanStatus: api.updatePlanStatus,
    transferPlanOwnership: api.transferPlanOwnership,
    rebindPlanAccount: api.rebindPlanAccount,
    deletePlan: api.deletePlan,
  }
}
