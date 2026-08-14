import { APIRequestError } from '../api'
import { formatShareBasisPoints } from '../planAllocation'

export const planAuditActionLabels: Record<string, string> = {
  'plan.created': '创建了 Plan',
  'plan.renamed': '更新了 Plan 名称',
  'plan.description_updated': '更新了 Plan 描述',
  'plan.publication_updated': '更新了大厅发布设置',
  'plan.archived': '归档了 Plan',
  'plan.restored': '恢复了 Plan',
  'plan.deleted': '删除了 Plan',
  'plan.owner_transferred': '转让了所有权',
  'plan.account_rebound': '更换了 OpenAI 账号',
  'plan.account_bound': '绑定了 OpenAI 账号',
  'plan.quota_refreshed': '刷新了额度',
  'plan.quota_reset': '重置了额度窗口',
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

export const planAuditMetadataLabels: Record<string, string> = {
  name: '名称',
  account_id: '账号 ID',
  application_id: '申请 ID',
  invite_id: '邀请 ID',
  member_id: '成员 ID',
  email: '邮箱',
  visibility: '可见性',
  public_slots: '公开招募名额',
  public_share_basis_points: '每人份额',
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

export function formatPlanAuditDate(value: string) {
  return dateFormatter.format(new Date(value))
}

export function formatPlanAuditMetadata(key: string | number, value: string | number) {
  if (key === 'share_basis_points' || key === 'public_share_basis_points') return formatShareBasisPoints(Number(value))
  if (key === 'visibility') return value === 'public' ? '公开' : '私密'
  return String(value)
}

export function planRequestErrorMessage(value: unknown) {
  if (value instanceof APIRequestError) {
    if (value.code === 'account_already_bound') return '这个 OpenAI 账号已绑定其他 Plan，请先删除或更换其中一个 Plan'
    if (value.code === 'share_exceeded') return '分配份额已超过 100%，请刷新 Plan 后减少成员、邀请或公开招募预留额度'
    return value.message
  }
  return value instanceof Error ? value.message : String(value)
}
