import { computed, type Ref } from 'vue'
import type { PlanDetail, PlanQuotaResetResult, QuotaResetCredits, QuotaResetVote, QuotaResetVoteChoice, QuotaResetVoteMutationResult, QuotaResetVoteState } from '../types'

interface QuotaResetVoteAPI {
  quotaResetVote(planID: string): Promise<QuotaResetVoteState>
  createQuotaResetVote(planID: string): Promise<QuotaResetVoteMutationResult>
  castQuotaResetVote(planID: string, voteID: string, choice: Exclude<QuotaResetVoteChoice, ''>): Promise<QuotaResetVoteMutationResult>
}

interface QuotaResetVotingOptions {
  adminMode?: boolean
  userID: string
  detail: Ref<PlanDetail | null>
  credits: Ref<QuotaResetCredits | null>
  vote: Ref<QuotaResetVote | null>
  loading: Ref<boolean>
  action: Ref<string>
  api: QuotaResetVoteAPI
  queryCredits(): Promise<void>
  notifySuccess(message: string): void
  notifyError(context: string, error: unknown): void
  notifyFailure(message: string): void
}

export function useQuotaResetVoting(options: QuotaResetVotingOptions) {
  let requestSequence = 0
  const currentMember = computed(() => options.detail.value?.members.find(member => member.user_id === options.userID))
  const votingWeight = computed(() => options.detail.value?.members
    .filter(member => options.detail.value!.plan.allocation_mode === 'shared' || member.share_basis_points > 0)
    .reduce((sum, member) => sum + member.share_basis_points, 0) ?? 0)
  const disabledReason = computed(() => {
    if (options.adminMode) return '管理员不参与成员投票'
    if (!options.detail.value?.account || options.detail.value.plan.status === 'archived') return 'Plan 当前不能发起额度重置投票'
    if (!currentMember.value) return '只有当前有效成员可以发起投票'
    if (options.detail.value.plan.allocation_mode !== 'shared' && currentMember.value.share_basis_points === 0) return '仅查看成员不能参与额度重置投票'
    if (options.detail.value.plan.allocation_mode !== 'shared' && votingWeight.value <= 5000) return '当前可投票份额合计不足以严格超过整个 Plan 的 50%'
    if (options.credits.value?.available_count === 0) return '当前没有可用的重置机会'
    return ''
  })
  const canStart = computed(() => disabledReason.value === ''
    && !options.loading.value
    && options.action.value === ''
    && !['active', 'executing'].includes(options.vote.value?.status ?? ''))

  function clear() {
    requestSequence += 1
    options.vote.value = null
    options.loading.value = false
    options.action.value = ''
  }

  async function load(planID = options.detail.value?.plan.id ?? '', silent = false) {
    if (!planID || options.adminMode || options.detail.value?.plan.id !== planID) return
    const sequence = ++requestSequence
    if (!silent) options.loading.value = true
    try {
      const state = await options.api.quotaResetVote(planID)
      if (sequence === requestSequence && options.detail.value?.plan.id === planID) options.vote.value = state.vote
    } catch (error) {
      if (!silent && sequence === requestSequence) options.notifyError('加载重置投票失败', error)
    } finally {
      if (!silent && sequence === requestSequence) options.loading.value = false
    }
  }

  async function start() {
    if (!options.detail.value || !canStart.value) return
    const planID = options.detail.value.plan.id
    options.action.value = 'start'
    try {
      const result = await options.api.createQuotaResetVote(planID)
      if (options.detail.value?.plan.id !== planID) return
      options.vote.value = result.vote
      options.credits.value = null
      presentMutation(result.reset_result, result.vote.status)
      await options.queryCredits()
    } catch (error) {
      options.notifyError('发起重置投票失败', error)
    } finally {
      if (options.detail.value?.plan.id === planID) options.action.value = ''
    }
  }

  async function cast(choice: Exclude<QuotaResetVoteChoice, ''>) {
    if (!options.detail.value || !options.vote.value?.can_vote || options.vote.value.status !== 'active' || options.action.value) return
    const planID = options.detail.value.plan.id
    const voteID = options.vote.value.id
    options.action.value = choice
    try {
      const result = await options.api.castQuotaResetVote(planID, voteID, choice)
      if (options.detail.value?.plan.id !== planID || result.vote.id !== voteID) return
      options.vote.value = result.vote
      if (result.reset_result) options.credits.value = null
      presentMutation(result.reset_result, result.vote.status)
      if (result.reset_result) await options.queryCredits()
    } catch (error) {
      options.notifyError('提交投票失败', error)
      await load(planID, true)
    } finally {
      if (options.detail.value?.plan.id === planID) options.action.value = ''
    }
  }

  function presentMutation(resetResult: PlanQuotaResetResult | null, status: string) {
    if (status === 'cancelled') return options.notifyFailure('投票已通过，但系统未开始消费重置机会；请刷新 Plan 后重新发起投票。')
    if (status === 'outcome_unknown') return options.notifyFailure('投票已通过，但 OpenAI 重置结果无法确认；请先查询剩余次数。')
    if (!resetResult) return options.notifySuccess('投票已提交')
    if (status === 'succeeded') options.notifySuccess(`投票已通过，系统已重置 ${resetResult.windows_reset} 个额度窗口`)
    else if (status === 'succeeded_unsynced') options.notifyFailure('投票已通过且重置成功，但最新额度暂未同步；请勿重复重置。')
  }

  return { canStart, disabledReason, clear, load, start, cast }
}
