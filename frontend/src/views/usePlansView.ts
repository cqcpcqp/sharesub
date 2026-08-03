import { computed, reactive, ref, watch } from 'vue'
import { APIRequestError, api } from '../api'
import { allocationShareBasisPoints, formatShareBasisPoints } from '../planAllocation'
import type { Account, AuditEvent, Member, Plan, PlanAllocationMode, PlanDetail, User } from '../types'

export interface PlansViewProps { accounts: Account[]; plans: Plan[]; user: User; initialPlanId: string; invitePlanId: string }
export interface PlansViewEmit {
  (event: 'changed'): void
  (event: 'inviteOpened'): void
  (event: 'message', type: 'success' | 'error', text: string): void
}

export function usePlansView(props: PlansViewProps, emit: PlansViewEmit) {
  const detail = ref<PlanDetail | null>(null)
  const planLoading = ref(false)
  const quotaRefreshing = ref(false)
  const actionLoading = ref('')
  const activeTab = ref('overview')
  const auditEvents = ref<AuditEvent[]>([])
  const auditLoading = ref(false)
  const showCreate = ref(false)
  const showInviteComposer = ref(false)
  const inviteSecret = ref('')
  const showDeleteConfirmOne = ref(false)
  const showDeleteConfirmTwo = ref(false)
  const deleteNameDraft = ref('')
  const renameDraft = ref('')
  const transferMemberID = ref<string | null>(null)
  const rebindAccountID = ref<string | null>(null)

  const createForm = reactive<{ name: string; accountID: string; allocationMode: PlanAllocationMode; share: number }>({
    name: '',
    accountID: '',
    allocationMode: 'fixed',
    share: 20,
  })
  const inviteForm = reactive({ share: 10 })
  const publication = reactive<{ visibility: 'private' | 'public'; slots: number | null; share: number }>({ visibility: 'private', slots: 1, share: 10 })
  const shareDrafts = reactive<Record<string, number>>({})

  const availableAccounts = computed(() => props.accounts.filter(account =>
    account.owner_user_id === props.user.id
    && account.status === 'active'
    && !props.plans.some(plan => plan.account_id === account.id),
  ))
  const accountOptions = computed(() => availableAccounts.value.map(account => ({ label: `${account.email} · ${account.plan_type}`, value: account.id })))
  const planOptions = computed(() => props.plans.map(plan => ({ label: `${plan.name}${plan.status === 'archived' ? ' · 已归档' : ''}`, value: plan.id })))
  const isOwner = computed(() => detail.value?.plan.owner_user_id === props.user.id)
  const isShared = computed(() => detail.value?.plan.allocation_mode === 'shared')
  const isArchived = computed(() => detail.value?.plan.status === 'archived')
  const owner = computed(() => detail.value?.members.find(member => member.role === 'owner'))
  const currentMember = computed(() => detail.value?.members.find(member => member.user_id === props.user.id))
  const allocatedShare = computed(() => formatShareBasisPoints(detail.value?.members.reduce((sum, member) => sum + member.share_basis_points, 0) ?? 0))
  const approvedApplications = computed(() => detail.value?.applications.filter(item => item.status === 'approved').length ?? 0)
  const availablePublicSlots = computed(() => Math.max(0, (detail.value?.plan.public_slots ?? 0) - approvedApplications.value))
  const canRename = computed(() => Boolean(detail.value && renameDraft.value.trim() && renameDraft.value.trim() !== detail.value.plan.name))
  const canSavePublication = computed(() => publication.visibility === 'private' || (Number.isInteger(publication.slots) && publication.slots! >= 1 && publication.slots! <= 100))
  const canConfirmDelete = computed(() => Boolean(detail.value && deleteNameDraft.value.trim() === detail.value.plan.name))
  const transferMemberOptions = computed(() => detail.value!.members
    .filter(member => member.role === 'member')
    .map(member => ({ label: `${member.username} · ${member.email}`, value: member.id })))
  const rebindAccountOptions = computed(() => props.accounts
    .filter(account => account.owner_user_id === props.user.id
      && account.status === 'active'
      && account.id !== detail.value!.account.id
      && !props.plans.some(plan => plan.id !== detail.value!.plan.id && plan.account_id === account.id))
    .map(account => ({ label: `${account.email} · ${account.plan_type}`, value: account.id })))

  const actionLabels: Record<string, string> = {
    'plan.created': '创建了 Plan',
    'plan.renamed': '更新了 Plan 名称',
    'plan.publication_updated': '更新了大厅发布设置',
    'plan.archived': '归档了 Plan',
    'plan.restored': '恢复了 Plan',
    'plan.deleted': '删除了 Plan',
    'plan.owner_transferred': '转让了所有权',
    'plan.account_rebound': '更换了 OpenAI 账号',
    'application.created': '提交了加入申请',
    'application.approved': '批准了加入申请',
    'application.rejected': '拒绝了加入申请',
    'invite.created': '创建了邀请链接',
    'invite.accepted': '接受邀请并加入',
    'invite.revoked': '撤销了邀请链接',
    'member.share_updated': '更新了成员份额',
    'member.removed': '移除了成员',
    'member.left': '退出了 Plan',
  }

  const metadataLabels: Record<string, string> = {
    name: '名称',
    account_id: '账号 ID',
    application_id: '申请 ID',
    invite_id: '邀请 ID',
    member_id: '成员 ID',
    email: '邮箱',
    visibility: '可见性',
    public_slots: '公开席位',
    public_share_basis_points: '每席份额',
    share_basis_points: '成员份额',
  }

  const dateFormatter = new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })

  let planRequestSequence = 0
  let auditRequestSequence = 0
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
        return
      }
      if (detail.value && props.plans.some(plan => plan.id === detail.value!.plan.id)) return
      void loadPlan(props.plans[0].id)
    },
    { immediate: true },
  )

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
      if (detail.value?.plan.id === invitePlanID && detail.value.plan.owner_user_id === props.user.id && detail.value.plan.status !== 'archived') {
        showInviteComposer.value = true
      }
      emit('inviteOpened')
    },
    { immediate: true },
  )

  async function loadPlan(id: string) {
    const requestSequence = ++planRequestSequence
    const changingPlan = detail.value?.plan.id !== id
    planLoading.value = true
    if (changingPlan) {
      detail.value = null
      auditEvents.value = []
      auditRequestSequence += 1
    }
    try {
      const value = await api.plan(id)
      if (requestSequence !== planRequestSequence) return
      detail.value = value
      syncDetail(value)
      if (activeTab.value === 'activity') void loadAudit(id)
    } catch (error) {
      if (requestSequence === planRequestSequence) notifyError(error)
    } finally {
      if (requestSequence === planRequestSequence) planLoading.value = false
    }
  }

  function syncDetail(value: PlanDetail) {
    for (const member of value.members) shareDrafts[member.id] = Math.round(member.share_basis_points / 100)
    publication.visibility = value.plan.visibility
    publication.slots = value.plan.visibility === 'private' ? 1 : value.plan.public_slots
    publication.share = value.plan.allocation_mode === 'shared' ? 0 : value.plan.visibility === 'private' ? 10 : Math.round(value.plan.public_share_basis_points / 100)
    renameDraft.value = value.plan.name
    transferMemberID.value = null
    rebindAccountID.value = null
  }

  function setPublicationVisibility(value: boolean) {
    publication.visibility = value ? 'public' : 'private'
  }
  function updateRenameDraft(value: string) { renameDraft.value = value }
  function updateDeleteNameDraft(value: string) { deleteNameDraft.value = value }
  function updatePublicationSlots(value: number | null) { publication.slots = value }
  function updatePublicationShare(value: number) { publication.share = value }
  function updateRebindAccount(value: string | null) { rebindAccountID.value = value }
  function updateTransferMember(value: string | null) { transferMemberID.value = value }
  function updateCreateName(value: string) { createForm.name = value }
  function updateCreateAccount(value: string) { createForm.accountID = value }
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
      const value = await api.planAuditEvents(planID)
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
      syncDetail(value)
      showCreate.value = false
      emit('changed')
      notifySuccess('Plan 已创建')
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
      await api.refreshPlanQuota(planID)
      await loadPlan(planID)
      notifySuccess('账号额度与重置时间已更新')
    } catch (error) {
      notifyError(error)
    } finally {
      quotaRefreshing.value = false
    }
  }

  async function sendInvite() {
    if (!detail.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'create-invite'
    try {
      const value = await api.invite(planID, allocationShareBasisPoints(detail.value.plan.allocation_mode, inviteForm.share))
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
      await api.revokeInvite(planID, inviteID)
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
      await api.updatePublication(planID, {
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
      await api.updateMember(planID, member.id, Math.round(shareDrafts[member.id] * 100))
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
      await api.removeMember(planID, member.id)
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
      await api.removeMember(planID, currentMember.value.id)
      planRequestSequence += 1
      detail.value = null
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
      await api.reviewApplication(id, decision)
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
      await api.renamePlan(planID, renameDraft.value.trim())
      await loadPlan(planID)
      emit('changed')
      notifySuccess('Plan 名称已更新')
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
      await api.updatePlanStatus(planID, status)
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
      await api.transferPlanOwnership(planID, transferMemberID.value)
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
    actionLoading.value = 'rebind'
    try {
      await api.rebindPlanAccount(planID, rebindAccountID.value)
      await loadPlan(planID)
      emit('changed')
      notifySuccess('Plan 已切换到新的 OpenAI 账号')
    } catch (error) {
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
      await api.deletePlan(planID)
      planRequestSequence += 1
      auditRequestSequence += 1
      detail.value = null
      closeDeleteDialogs()
      emit('changed')
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

  function formatDate(value: string) {
    return dateFormatter.format(new Date(value))
  }

  function formatMetadata(key: string | number, value: string | number) {
    if (key === 'share_basis_points' || key === 'public_share_basis_points') return formatShareBasisPoints(Number(value))
    if (key === 'visibility') return value === 'public' ? '公开' : '私密'
    return String(value)
  }

  function notifySuccess(message: string) {
    emit('message', 'success', message)
  }

  function notifyError(value: unknown) {
    const message = value instanceof APIRequestError && value.code === 'account_already_bound'
      ? '这个 OpenAI 账号已绑定其他 Plan，请先删除或更换其中一个 Plan'
      : value instanceof Error ? value.message : String(value)
    emit('message', 'error', message)
  }

  return {
    detail, planLoading, quotaRefreshing, actionLoading, activeTab, auditEvents, auditLoading,
    availableAccounts, loadPlan, loadAudit,
    showCreate, showInviteComposer, inviteSecret, showDeleteConfirmOne, showDeleteConfirmTwo,
    deleteNameDraft, renameDraft, transferMemberID, rebindAccountID, createForm, inviteForm,
    publication, shareDrafts, accountOptions, planOptions, isOwner, isShared, isArchived, owner,
    currentMember, allocatedShare, availablePublicSlots, canRename, canSavePublication,
    canConfirmDelete, transferMemberOptions, rebindAccountOptions, actionLabels, metadataLabels,
    setPublicationVisibility, updateRenameDraft, updateDeleteNameDraft, updatePublicationSlots,
    updatePublicationShare, updateRebindAccount, updateTransferMember, updateCreateName,
    updateCreateAccount, updateCreateAllocationMode, updateCreateShare, updateInviteShare,
    handleTabChange, openCreate, createPlan, refreshQuota, sendInvite, revokeInvite, savePublication,
    saveShare, removeMember, leavePlan, review, applicationReviewBusy, renamePlan, updatePlanStatus,
    transferOwnership, rebindAccount, continueDelete, closeDeleteDialogs, deletePlan, copyInvite,
    formatDate, formatMetadata,
  }
}
