import { computed, watch } from 'vue'
import { APIRequestError, api } from '../api'
import { allocationShareBasisPoints, formatShareBasisPoints, maxPlanShareBasisPoints, planApprovedPublicMemberCount, planAvailablePublicSlotCount, planPublicationShareBasisPoints, planReservedShareBasisPoints } from '../planAllocation'
import type { Account, Member, PerformancePeriod, Plan, PlanAllocationMode, PlanDetail, User } from '../types'
import { createPlanManagementAPI } from './planManagementAPI'
import { usePlanConversion } from './usePlanConversion'
import { formatPlanAuditDate, formatPlanAuditMetadata, planAuditActionLabels, planAuditMetadataLabels, planRequestErrorMessage } from './planViewPresentation'
import { createPlanViewState } from './planViewState'
import { useQuotaResetVoting } from './useQuotaResetVoting'

const automaticQuotaRefreshes = new Map<string, number>()
const automaticQuotaRefreshTTL = 5 * 60 * 1000

export interface PlansViewProps { accounts: Account[]; plans: Plan[]; user: User; initialPlanId: string; invitePlanId: string; adminMode?: boolean }
export interface PlansViewEmit {
  (event: 'changed'): void
  (event: 'inviteOpened'): void
  (event: 'deleted'): void
  (event: 'message', type: 'success' | 'error', text: string): void
}

export function usePlansView(props: PlansViewProps, emit: PlansViewEmit) {
  const {
    detail, planLoading, quotaRefreshing, quotaResetCredits, quotaResetCreditsLoading, quotaResetting,
    quotaResetVote, quotaResetVoteLoading, quotaResetVoteAction,
    performanceLoading, performancePeriod, actionLoading, activeTab, auditEvents, auditLoading,
    showCreate, showConnectAccount, showInviteComposer, inviteSecret, showDeleteConfirmOne, showDeleteConfirmTwo,
    deleteNameDraft, renameDraft, descriptionDraft, transferMemberID, rebindAccountID,
    createForm, inviteForm, publication, shareDrafts,
  } = createPlanViewState()

  const resourceOwnerID = computed(() => detail.value?.plan.owner_user_id ?? props.user.id)
  const availableAccounts = computed(() => props.accounts.filter(account =>
    account.owner_user_id === resourceOwnerID.value
    && account.status === 'active'
    && !props.plans.some(plan => plan.account_id === account.id),
  ))
  const accountOptions = computed(() => availableAccounts.value.map(account => ({ label: `${account.email} · ${account.plan_type}`, value: account.id })))
  const planOptions = computed(() => props.plans.map(plan => ({ label: `${plan.name}${plan.status === 'archived' ? ' · 已归档' : ''}`, value: plan.id })))
  const isActualOwner = computed(() => detail.value?.plan.owner_user_id === props.user.id)
  const canManage = computed(() => Boolean(isActualOwner.value || props.adminMode))
  const isShared = computed(() => detail.value?.plan.allocation_mode === 'shared')
  const isArchived = computed(() => detail.value?.plan.status === 'archived')
  const isAccountBound = computed(() => detail.value?.account !== null)
  const owner = computed(() => detail.value?.members.find(member => member.role === 'owner'))
  const currentMember = computed(() => detail.value?.members.find(member => member.user_id === props.user.id))
  const allocatedShare = computed(() => formatShareBasisPoints(detail.value?.members.reduce((sum, member) => sum + member.share_basis_points, 0) ?? 0))
  const reservedShares = computed(() => detail.value
    ? planReservedShareBasisPoints(detail.value)
    : { members: 0, pendingInvites: 0, publicSlots: 0, total: 0, remaining: 0 })
  const remainingInviteSharePercent = computed(() => Math.floor(reservedShares.value.remaining / 100))
  const canCreateInvite = computed(() => Boolean(detail.value && (isShared.value || (
    Number.isInteger(inviteForm.share)
    && inviteForm.share >= 0
    && inviteForm.share <= remainingInviteSharePercent.value
  ))))
  const approvedPublicMembers = computed(() => detail.value ? planApprovedPublicMemberCount(detail.value) : 0)
  const availablePublicSlots = computed(() => detail.value ? planAvailablePublicSlotCount(detail.value) : 0)
  const publicationAvailablePublicSlots = computed(() => {
    if (!detail.value || publication.visibility !== 'public' || !Number.isInteger(publication.slots)) return 0
    return planAvailablePublicSlotCount(detail.value, publication.slots!)
  })
  const publicationReservedShares = computed(() => {
    if (!detail.value) return { members: 0, pendingInvites: 0, publicSlots: 0, total: 0, remaining: 0 }
    const slots = publication.visibility === 'public' && Number.isInteger(publication.slots) ? publication.slots! : 0
    const shareBasisPoints = publication.visibility === 'public'
      ? allocationShareBasisPoints(detail.value.plan.allocation_mode, publication.share)
      : 0
    return planPublicationShareBasisPoints(detail.value, slots, shareBasisPoints)
  })
  const maxPublicSeatSharePercent = computed(() => {
    if (!detail.value || isShared.value || publication.visibility !== 'public' || !Number.isInteger(publication.slots)) return 100
    const seatsToReserve = Math.max(0, publication.slots! - approvedPublicMembers.value)
    if (seatsToReserve === 0) return 100
    const committed = publicationReservedShares.value.members + publicationReservedShares.value.pendingInvites
    return Math.max(0, Math.min(100, Math.floor((maxPlanShareBasisPoints - committed) / seatsToReserve / 100)))
  })
  const publicationCapacityExceeded = computed(() => publicationReservedShares.value.total > maxPlanShareBasisPoints)
  const canRename = computed(() => Boolean(detail.value && renameDraft.value.trim() && renameDraft.value.trim() !== detail.value.plan.name))
  const canUpdateDescription = computed(() => Boolean(detail.value && descriptionDraft.value.trim() !== detail.value.plan.description))
  const canSavePublication = computed(() => Boolean(detail.value && (publication.visibility === 'private' || (
    Number.isInteger(publication.slots)
    && publication.slots! >= 1
    && publication.slots! <= 100
    && publication.slots! >= approvedPublicMembers.value
    && !publicationCapacityExceeded.value
  ))))
  const canConfirmDelete = computed(() => Boolean(detail.value && deleteNameDraft.value.trim() === detail.value.plan.name))
  const transferMemberOptions = computed(() => detail.value!.members
    .filter(member => member.role === 'member')
    .map(member => ({ label: `${member.username} · ${member.email}`, value: member.id })))
  const rebindAccountOptions = computed(() => props.accounts
    .filter(account => account.owner_user_id === resourceOwnerID.value
      && account.status === 'active'
      && account.id !== detail.value!.account?.id
      && !props.plans.some(plan => plan.id !== detail.value!.plan.id && plan.account_id === account.id))
    .map(account => ({ label: `${account.email} · ${account.plan_type}`, value: account.id })))

  const actionLabels = planAuditActionLabels
  const metadataLabels = planAuditMetadataLabels
  const managementAPI = createPlanManagementAPI(props.adminMode)
  const conversion = usePlanConversion(detail, isShared, actionLoading, managementAPI, loadPlan, () => emit('changed'), notifySuccess, notifyError)
  const quotaResetVoting = useQuotaResetVoting({
    adminMode: props.adminMode, userID: props.user.id, detail, credits: quotaResetCredits,
    vote: quotaResetVote, loading: quotaResetVoteLoading, action: quotaResetVoteAction, api: managementAPI,
    queryCredits: queryQuotaResetCredits, notifySuccess, notifyError: notifyErrorWithContext,
    notifyFailure: message => emit('message', 'error', message),
  })
  const canStartQuotaResetVote = quotaResetVoting.canStart
  const quotaResetVoteDisabledReason = quotaResetVoting.disabledReason

  let planRequestSequence = 0
  let performanceRequestSequence = 0
  let auditRequestSequence = 0
  let quotaResetCreditsRequestSequence = 0
  const quotaResettingAccountIDs = new Set<string>()
  let consumedInitialPlanID = ''
  let consumedInvitePlanID = ''

  watch(
    [() => props.initialPlanId, () => props.plans.map(plan => plan.id).join(',')],
    ([initialPlanID, planIDs]) => {
      if (!initialPlanID) consumedInitialPlanID = ''
      if (initialPlanID && initialPlanID !== consumedInitialPlanID) {
        consumedInitialPlanID = initialPlanID
        if (props.plans.some(plan => plan.id === initialPlanID)) {
          if (detail.value?.plan.id !== initialPlanID) void loadPlan(initialPlanID)
          return
        }
      }
      if (!planIDs) {
        planRequestSequence += 1
        planLoading.value = false
        detail.value = null
        clearQuotaResetState()
        return
      }
      if (detail.value && props.plans.some(plan => plan.id === detail.value!.plan.id)) return
      void loadPlan(props.plans[0].id)
    },
    { immediate: true },
  )

  watch([showInviteComposer, remainingInviteSharePercent], ([show, remaining]) => {
    if (show && !isShared.value && inviteForm.share > remaining) inviteForm.share = remaining
  })

  watch(
    [() => props.invitePlanId, () => props.plans.map(plan => plan.id).join(',')],
    async ([invitePlanID]) => {
      if (!invitePlanID) {
        consumedInvitePlanID = ''
        return
      }
      if (invitePlanID === consumedInvitePlanID || !props.plans.some(plan => plan.id === invitePlanID)) return
      consumedInvitePlanID = invitePlanID
      activeTab.value = 'members'
      if (detail.value?.plan.id !== invitePlanID) await loadPlan(invitePlanID)
      if (detail.value?.plan.id === invitePlanID && canManage.value && detail.value.plan.status !== 'archived') {
        showInviteComposer.value = true
      }
      emit('inviteOpened')
    },
    { immediate: true },
  )

  async function loadPlan(id: string) {
    const requestSequence = ++planRequestSequence
    const changingPlan = detail.value?.plan.id !== id
    const previousAccountID = detail.value?.account?.id ?? ''
    planLoading.value = true
    if (changingPlan) {
      detail.value = null
      clearQuotaResetState()
      auditEvents.value = []
      auditRequestSequence += 1
      performanceRequestSequence += 1
    }
    try {
      const value = await managementAPI.plan(id)
      if (requestSequence !== planRequestSequence) return
      if (!changingPlan && previousAccountID !== (value.account?.id ?? '')) clearQuotaResetState()
      detail.value = value
      syncDetail(value)
      if (!props.adminMode && value.account && value.plan.status !== 'archived') void quotaResetVoting.load(id)
      if (activeTab.value === 'activity') void loadAudit(id)
      if (performancePeriod.value !== '24h') void loadPerformance(performancePeriod.value)
      if (value.account && value.plan.status !== 'archived') void refreshQuotaAutomatically(id, requestSequence)
    } catch (error) {
      if (requestSequence === planRequestSequence) notifyError(error)
    } finally {
      if (requestSequence === planRequestSequence) planLoading.value = false
    }
  }

  async function loadPerformance(period: PerformancePeriod) {
    performancePeriod.value = period
    if (!detail.value) return
    const planID = detail.value.plan.id
    const requestSequence = ++performanceRequestSequence
    performanceLoading.value = true
    try {
      const value = await managementAPI.planPerformance(planID, period)
      if (requestSequence !== performanceRequestSequence || detail.value?.plan.id !== planID) return
      detail.value.insights.performance = value
      detail.value.insights.model_usage = value.model_usage
      detail.value.insights.token_trend = value.token_trend
      detail.value.insights.recent_usage = value.recent_usage
    } catch (error) {
      if (requestSequence === performanceRequestSequence) notifyError(error)
    } finally {
      if (requestSequence === performanceRequestSequence) performanceLoading.value = false
    }
  }

  async function refreshQuotaAutomatically(planID: string, requestSequence: number) {
    for (const [cachedPlanID, refreshedAt] of automaticQuotaRefreshes) {
      if (Date.now() - refreshedAt >= automaticQuotaRefreshTTL) automaticQuotaRefreshes.delete(cachedPlanID)
    }
    const lastRefresh = automaticQuotaRefreshes.get(planID)
    if (lastRefresh !== undefined && Date.now() - lastRefresh < automaticQuotaRefreshTTL) return
    automaticQuotaRefreshes.set(planID, Date.now())
    quotaRefreshing.value = true
    try {
      await managementAPI.refreshPlanQuota(planID, true)
      const value = await managementAPI.plan(planID)
      if (requestSequence !== planRequestSequence) return
      if (performancePeriod.value !== '24h' && detail.value?.plan.id === planID) {
        value.insights.performance = detail.value.insights.performance
        value.insights.model_usage = detail.value.insights.model_usage
        value.insights.token_trend = detail.value.insights.token_trend
        value.insights.recent_usage = detail.value.insights.recent_usage
      }
      detail.value = value
      syncDetail(value)
    } catch (error) {
      automaticQuotaRefreshes.delete(planID)
      console.error('Failed to refresh Plan quota automatically:', error)
    } finally {
      if (requestSequence === planRequestSequence) quotaRefreshing.value = false
    }
  }

  function syncDetail(value: PlanDetail) {
    for (const memberID of Object.keys(shareDrafts)) delete shareDrafts[memberID]
    for (const member of value.members) shareDrafts[member.id] = Math.round(member.share_basis_points / 100)
    publication.visibility = value.plan.visibility
    publication.slots = value.plan.visibility === 'private' ? 1 : value.plan.public_slots
    publication.share = value.plan.allocation_mode === 'shared' ? 0 : value.plan.visibility === 'private' ? 10 : Math.round(value.plan.public_share_basis_points / 100)
    renameDraft.value = value.plan.name
    descriptionDraft.value = value.plan.description
    quotaResetting.value = Boolean(value.account && quotaResettingAccountIDs.has(value.account.id))
    transferMemberID.value = null
    rebindAccountID.value = null
  }

  function setPublicationVisibility(value: boolean) {
    publication.visibility = value ? 'public' : 'private'
  }
  function updateRenameDraft(value: string) { renameDraft.value = value }
  function updateDescriptionDraft(value: string) { descriptionDraft.value = value }
  function updateDeleteNameDraft(value: string) { deleteNameDraft.value = value }
  function updatePublicationSlots(value: number | null) { publication.slots = value }
  function updatePublicationShare(value: number) { publication.share = value }
  function updateRebindAccount(value: string | null) { rebindAccountID.value = value }
  function updateTransferMember(value: string | null) { transferMemberID.value = value }
  function updateCreateName(value: string) { createForm.name = value }
  function updateCreateAccount(value: string | null) { createForm.accountID = value ?? '' }
  function updateCreateAllocationMode(value: PlanAllocationMode) { createForm.allocationMode = value }
  function updateCreateShare(value: number) { createForm.share = value }
  function updateInviteShare(value: number) { inviteForm.share = value }
  function handleTabChange(name: string | number) {
    if (name === 'activity' && detail.value) void loadAudit(detail.value.plan.id)
  }

  async function loadAudit(planID: string) {
    const requestSequence = ++auditRequestSequence
    auditLoading.value = true
    try {
      const value = await managementAPI.planAuditEvents(planID)
      if (requestSequence === auditRequestSequence) auditEvents.value = value
    } catch (error) {
      if (requestSequence === auditRequestSequence) notifyError(error)
    } finally {
      if (requestSequence === auditRequestSequence) auditLoading.value = false
    }
  }

  function openCreate() {
    createForm.name = ''
    createForm.accountID = availableAccounts.value[0]?.id ?? ''
    createForm.allocationMode = 'fixed'
    createForm.share = 20
    showCreate.value = true
  }

  async function createPlan() {
    actionLoading.value = 'create-plan'
    try {
      const value = await api.createPlan({
        account_id: createForm.accountID,
        name: createForm.name.trim(),
        allocation_mode: createForm.allocationMode,
        owner_share_basis_points: allocationShareBasisPoints(createForm.allocationMode, createForm.share),
      })
      detail.value = value
      clearQuotaResetState()
      syncDetail(value)
      showCreate.value = false
      if (!value.account) activeTab.value = 'account'
      emit('changed')
      notifySuccess(value.account ? 'Plan 已创建' : 'Plan 已创建，接下来可绑定 OpenAI 账号')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function refreshQuota() {
    if (!detail.value) return
    const planID = detail.value.plan.id
    quotaRefreshing.value = true
    try {
      await managementAPI.refreshPlanQuota(planID)
      await loadPlan(planID)
      notifySuccess('账号额度与重置时间已更新')
    } catch (error) {
      notifyError(error)
    } finally {
      quotaRefreshing.value = false
    }
  }

  function clearQuotaResetState() {
    quotaResetCreditsRequestSequence += 1
    quotaResetCredits.value = null
    quotaResetCreditsLoading.value = false
    quotaResetting.value = false
    quotaResetVoting.clear()
  }

  function canManageQuotaReset() {
    return Boolean(detail.value
      && detail.value.account
      && canManage.value
      && detail.value.plan.status !== 'archived')
  }

  function canQueryQuotaReset() {
    return Boolean(detail.value?.account && detail.value.plan.status !== 'archived')
  }

  async function queryQuotaResetCredits() {
    if (!canQueryQuotaReset() || !detail.value?.account) return
    const planID = detail.value.plan.id
    const accountID = detail.value.account.id
    const requestSequence = ++quotaResetCreditsRequestSequence
    quotaResetCreditsLoading.value = true
    try {
      const value = await managementAPI.planQuotaResetCredits(planID)
      if (requestSequence !== quotaResetCreditsRequestSequence
        || detail.value?.plan.id !== planID
        || detail.value.account?.id !== accountID) return
      quotaResetCredits.value = value
    } catch (error) {
      if (requestSequence === quotaResetCreditsRequestSequence) {
        notifyErrorWithContext('查询重置次数失败', error)
      }
    } finally {
      if (requestSequence === quotaResetCreditsRequestSequence) quotaResetCreditsLoading.value = false
    }
  }

  async function resetQuota() {
    if (!canManageQuotaReset()
      || !detail.value?.account
      || quotaResetCredits.value === null
      || quotaResetCredits.value.available_count === 0
      || quotaResettingAccountIDs.has(detail.value.account.id)) return
    const planID = detail.value.plan.id
    const accountID = detail.value.account.id
    quotaResettingAccountIDs.add(accountID)
    quotaResetting.value = true
    try {
      const result = await managementAPI.resetPlanQuota(planID)
      notifySuccess(`已使用 1 次重置机会，OpenAI 已重置 ${result.windows_reset} 个额度窗口`)
      if (detail.value?.plan.id !== planID || detail.value.account?.id !== accountID) return
      quotaResetCredits.value = null
      if (result.quota_refreshed) {
        await loadPlan(planID)
      } else {
        emit('message', 'error', '重置已成功，但最新额度暂未同步；可稍后使用“查询额度”更新显示，请勿重复重置。')
      }
      await queryQuotaResetCredits()
    } catch (error) {
      if (detail.value?.plan.id === planID && detail.value.account?.id === accountID) quotaResetCredits.value = null
      notifyErrorWithContext('重置请求未能确认结果，请先重新查询剩余次数', error)
    } finally {
      quotaResettingAccountIDs.delete(accountID)
      if (detail.value?.account?.id === accountID) quotaResetting.value = false
    }
  }

  async function sendInvite() {
    if (!detail.value || !canCreateInvite.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'create-invite'
    try {
      const value = await managementAPI.invite(planID, allocationShareBasisPoints(detail.value.plan.allocation_mode, inviteForm.share))
      showInviteComposer.value = false
      inviteSecret.value = value.invite_url
      localStorage.setItem(`sharesub.onboarding.invite.${planID}`, 'done')
      await loadPlan(planID)
      notifySuccess('邀请链接已生成')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function revokeInvite(inviteID: string) {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = `invite-${inviteID}`
    try {
      await managementAPI.revokeInvite(planID, inviteID)
      await loadPlan(planID)
      notifySuccess('邀请已撤销')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function savePublication() {
    if (!detail.value || !canSavePublication.value) return
    const planID = detail.value.plan.id
    const publicSlots = publication.visibility === 'public' ? publication.slots! : 0
    actionLoading.value = 'publication'
    try {
      await managementAPI.updatePublication(planID, {
        visibility: publication.visibility,
        public_slots: publicSlots,
        public_share_basis_points: publication.visibility === 'public' ? allocationShareBasisPoints(detail.value.plan.allocation_mode, publication.share) : 0,
      })
      await loadPlan(planID)
      emit('changed')
      notifySuccess(publication.visibility === 'public' ? 'Plan 已上架大厅' : 'Plan 已设为私密')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function saveShare(member: Member) {
    if (!detail.value) return
    const planID = detail.value.plan.id
    const original = Math.round(member.share_basis_points / 100)
    actionLoading.value = `share-${member.id}`
    try {
      await managementAPI.updateMember(planID, member.id, Math.round(shareDrafts[member.id] * 100))
      await loadPlan(planID)
      notifySuccess('成员份额已更新')
    } catch (error) {
      shareDrafts[member.id] = original
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function removeMember(member: Member) {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = `remove-${member.id}`
    try {
      await managementAPI.removeMember(planID, member.id)
      await loadPlan(planID)
      emit('changed')
      notifySuccess(`${member.username} 已被移出 Plan`)
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function leavePlan() {
    if (!detail.value || !currentMember.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'leave-plan'
    try {
      await managementAPI.removeMember(planID, currentMember.value.id)
      planRequestSequence += 1
      detail.value = null
      clearQuotaResetState()
      emit('changed')
      notifySuccess('你已退出 Plan')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function review(id: string, decision: 'approve' | 'reject') {
    if (!detail.value || applicationReviewBusy(id)) return
    const planID = detail.value.plan.id
    actionLoading.value = `application-${decision}-${id}`
    try {
      await managementAPI.reviewApplication(planID, id, decision)
      await loadPlan(planID)
      emit('changed')
      notifySuccess(decision === 'approve' ? '申请已批准' : '申请已拒绝')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  function applicationReviewBusy(id: string) {
    return actionLoading.value === `application-approve-${id}` || actionLoading.value === `application-reject-${id}`
  }

  async function renamePlan() {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'rename'
    try {
      await managementAPI.renamePlan(planID, renameDraft.value.trim())
      await loadPlan(planID)
      emit('changed')
      notifySuccess('Plan 名称已更新')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function updatePlanDescription() {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'description'
    try {
      await managementAPI.updatePlanDescription(planID, descriptionDraft.value.trim())
      await loadPlan(planID)
      emit('changed')
      notifySuccess('Plan 描述已更新')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function updatePlanStatus(status: 'active' | 'archived') {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = `status-${status}`
    try {
      await managementAPI.updatePlanStatus(planID, status)
      await loadPlan(planID)
      emit('changed')
      notifySuccess(status === 'archived' ? 'Plan 已归档' : 'Plan 已恢复')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function transferOwnership() {
    if (!detail.value || !transferMemberID.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'transfer'
    try {
      await managementAPI.transferPlanOwnership(planID, transferMemberID.value)
      await loadPlan(planID)
      emit('changed')
      notifySuccess('Plan 所有权已转让')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function rebindAccount() {
    if (!detail.value || !rebindAccountID.value) return
    const planID = detail.value.plan.id
    const firstBinding = !detail.value.account
    actionLoading.value = 'rebind'
    try {
      await managementAPI.rebindPlanAccount(planID, rebindAccountID.value)
      await loadPlan(planID)
      emit('changed')
      notifySuccess(firstBinding ? 'OpenAI 账号已绑定到 Plan' : 'Plan 已切换到新的 OpenAI 账号')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function handleConnectedAccount(account: Account) {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'connect-account'
    try {
      await managementAPI.rebindPlanAccount(planID, account.id)
      await loadPlan(planID)
      emit('changed')
      notifySuccess('OpenAI 账号已接入并绑定到 Plan')
    } catch (error) {
      emit('changed')
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  function continueDelete() {
    showDeleteConfirmOne.value = false
    showDeleteConfirmTwo.value = true
    deleteNameDraft.value = ''
  }

  function closeDeleteDialogs() {
    showDeleteConfirmOne.value = false
    showDeleteConfirmTwo.value = false
    deleteNameDraft.value = ''
  }

  async function deletePlan() {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'delete'
    try {
      await managementAPI.deletePlan(planID)
      planRequestSequence += 1
      auditRequestSequence += 1
      detail.value = null
      clearQuotaResetState()
      closeDeleteDialogs()
      emit('changed')
      emit('deleted')
      notifySuccess('Plan 已永久删除')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  async function copyInvite() {
    try {
      await navigator.clipboard.writeText(inviteSecret.value)
      notifySuccess('邀请链接已复制')
    } catch (error) {
      notifyError(error)
    }
  }

  function formatDate(value: string) { return formatPlanAuditDate(value) }
  function formatMetadata(key: string | number, value: string | number) { return formatPlanAuditMetadata(key, value) }
  function notifySuccess(message: string) { emit('message', 'success', message) }
  function notifyError(value: unknown) { emit('message', 'error', planRequestErrorMessage(value)) }

  function notifyErrorWithContext(context: string, value: unknown) {
    const message = value instanceof APIRequestError && value.code === 'quota_reset_credits_rate_limited' ? `${value.message}，请 ${Math.max(1, value.retryAfterSeconds ?? 10)} 秒后再试` : value instanceof Error ? value.message : String(value)
    emit('message', 'error', `${context}：${message}`)
  }

  return {
    detail, planLoading, quotaRefreshing, quotaResetCredits, quotaResetCreditsLoading, quotaResetting, quotaResetVote, quotaResetVoteLoading, quotaResetVoteAction,
    performanceLoading, performancePeriod, actionLoading, activeTab, auditEvents, auditLoading,
    availableAccounts, loadPlan, loadAudit, loadPerformance,
    showCreate, showConnectAccount, showInviteComposer, inviteSecret, showDeleteConfirmOne, showDeleteConfirmTwo,
    deleteNameDraft, renameDraft, descriptionDraft, transferMemberID, rebindAccountID, createForm, inviteForm,
    publication, shareDrafts, accountOptions, planOptions, isActualOwner, canManage, isShared, isArchived, isAccountBound, owner,
    currentMember, allocatedShare, reservedShares, remainingInviteSharePercent, canCreateInvite, approvedPublicMembers, availablePublicSlots, publicationAvailablePublicSlots,
    canStartQuotaResetVote, quotaResetVoteDisabledReason, publicationReservedShares, maxPublicSeatSharePercent, publicationCapacityExceeded,
    canRename, canUpdateDescription, canSavePublication,
    canConfirmDelete, transferMemberOptions, rebindAccountOptions, actionLabels, metadataLabels,
    setPublicationVisibility, updateRenameDraft, updateDescriptionDraft, updateDeleteNameDraft, updatePublicationSlots,
    updatePublicationShare, updateRebindAccount, updateTransferMember, updateCreateName,
    updateCreateAccount, updateCreateAllocationMode, updateCreateShare, updateInviteShare,
    handleTabChange, openCreate, createPlan, refreshQuota, queryQuotaResetCredits, resetQuota, loadQuotaResetVote: quotaResetVoting.load, startQuotaResetVote: quotaResetVoting.start, castQuotaResetVote: quotaResetVoting.cast, sendInvite, revokeInvite, savePublication,
    saveShare, removeMember, leavePlan, review, renamePlan, updatePlanDescription, updatePlanStatus,
    transferOwnership, rebindAccount, handleConnectedAccount, continueDelete, closeDeleteDialogs, deletePlan, copyInvite,
    formatDate, formatMetadata,
    ...conversion,
  }
}
