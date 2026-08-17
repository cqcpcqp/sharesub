import { computed, reactive, ref, type ComputedRef, type Ref } from 'vue'
import { maxPlanShareBasisPoints } from '../planAllocation'
import type { PlanDetail } from '../types'
import type { createPlanManagementAPI } from './planManagementAPI'

export function usePlanConversion(
  detail: Ref<PlanDetail | null>,
  isShared: ComputedRef<boolean>,
  actionLoading: Ref<string>,
  managementAPI: ReturnType<typeof createPlanManagementAPI>,
  loadPlan: (id: string) => Promise<void>,
  emitChanged: () => void,
  notifySuccess: (message: string) => void,
  notifyError: (error: unknown) => void,
) {
  const showConvertToFixed = ref(false)
  const conversionShareDrafts = reactive<Record<string, number>>({})
  const conversionAllocatedBasisPoints = computed(() => Object.values(conversionShareDrafts)
    .reduce((sum, share) => sum + Math.round(share * 100), 0))
  const canConvertToFixed = computed(() => Boolean(detail.value
    && isShared.value
    && conversionAllocatedBasisPoints.value > 0
    && conversionAllocatedBasisPoints.value <= maxPlanShareBasisPoints))

  function updateConversionShare(memberID: string, value: number) {
    conversionShareDrafts[memberID] = value
  }

  function maxConversionSharePercent(memberID: string) {
    const otherShares = Object.entries(conversionShareDrafts)
      .filter(([id]) => id !== memberID)
      .reduce((sum, [, share]) => sum + Math.round(share * 100), 0)
    return Math.max(0, Math.floor((maxPlanShareBasisPoints - otherShares) / 100))
  }

  function openConvertToFixed() {
    if (!detail.value) return
    for (const memberID of Object.keys(conversionShareDrafts)) delete conversionShareDrafts[memberID]
    for (const member of detail.value.members) conversionShareDrafts[member.id] = 0
    showConvertToFixed.value = true
  }

  async function convertPlanToFixed() {
    if (!detail.value || !canConvertToFixed.value) return
    const planID = detail.value.plan.id
    actionLoading.value = 'convert-fixed'
    try {
      await managementAPI.convertPlanToFixed(planID, detail.value.members.map(member => ({
        member_id: member.id,
        share_basis_points: Math.round(conversionShareDrafts[member.id] * 100),
      })))
      showConvertToFixed.value = false
      await loadPlan(planID)
      emitChanged()
      notifySuccess('Plan 已转换为固定分配')
    } catch (error) {
      notifyError(error)
    } finally {
      actionLoading.value = ''
    }
  }

  return {
    showConvertToFixed,
    conversionShareDrafts,
    conversionAllocatedBasisPoints,
    canConvertToFixed,
    updateConversionShare,
    maxConversionSharePercent,
    openConvertToFixed,
    convertPlanToFixed,
  }
}
