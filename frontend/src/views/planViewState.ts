import { reactive, ref } from 'vue'
import type { AuditEvent, PerformancePeriod, PlanAllocationMode, PlanDetail, QuotaResetCredits } from '../types'

export function createPlanViewState() {
  const detail = ref<PlanDetail | null>(null)
  const planLoading = ref(false)
  const quotaRefreshing = ref(false)
  const quotaResetCredits = ref<QuotaResetCredits | null>(null)
  const quotaResetCreditsLoading = ref(false)
  const quotaResetting = ref(false)
  const performanceLoading = ref(false)
  const performancePeriod = ref<PerformancePeriod>('24h')
  const actionLoading = ref('')
  const activeTab = ref('overview')
  const auditEvents = ref<AuditEvent[]>([])
  const auditLoading = ref(false)
  const showCreate = ref(false)
  const showConnectAccount = ref(false)
  const showInviteComposer = ref(false)
  const inviteSecret = ref('')
  const showDeleteConfirmOne = ref(false)
  const showDeleteConfirmTwo = ref(false)
  const deleteNameDraft = ref('')
  const renameDraft = ref('')
  const descriptionDraft = ref('')
  const transferMemberID = ref<string | null>(null)
  const rebindAccountID = ref<string | null>(null)
  const createForm = reactive<{ name: string; accountID: string; allocationMode: PlanAllocationMode; share: number }>({
    name: '', accountID: '', allocationMode: 'fixed', share: 20,
  })
  const inviteForm = reactive({ share: 10 })
  const publication = reactive<{ visibility: 'private' | 'public'; slots: number | null; share: number }>({ visibility: 'private', slots: 1, share: 10 })
  const shareDrafts = reactive<Record<string, number>>({})

  return {
    detail, planLoading, quotaRefreshing, quotaResetCredits, quotaResetCreditsLoading, quotaResetting,
    performanceLoading, performancePeriod, actionLoading, activeTab, auditEvents, auditLoading,
    showCreate, showConnectAccount, showInviteComposer, inviteSecret, showDeleteConfirmOne, showDeleteConfirmTwo,
    deleteNameDraft, renameDraft, descriptionDraft, transferMemberID, rebindAccountID,
    createForm, inviteForm, publication, shareDrafts,
  }
}
