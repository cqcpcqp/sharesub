<template>
  <div class="tab-panel members-tab">
    <section class="members-section">
      <div class="section-heading">
        <div>
          <h3>成员</h3>
          <p>{{ isShared ? `${detail.members.length} 位成员共享账号额度，不设置个人上限` : `${detail.members.length} 位成员，固定份额合计 ${allocatedShare}` }}</p>
        </div>
        <NButton v-if="canManage && !isArchived" type="primary" @click="emit('openInvite')">
          <template #icon><UserPlus :size="16" /></template>
          邀请成员
        </NButton>
        <NPopconfirm
          v-if="!canManage && currentMember"
          positive-text="退出 Plan"
          negative-text="取消"
          @positive-click="emit('leavePlan')"
        >
          <template #trigger>
            <NButton secondary type="error" :loading="actionLoading === 'leave-plan'">
              <template #icon><LogOut :size="16" /></template>
              退出 Plan
            </NButton>
          </template>
          退出后，你的 API Key 将停止使用这个 Plan。
        </NPopconfirm>
      </div>

      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>成员</th>
              <th>角色</th>
              <th>{{ isShared ? '额度方式' : '份额' }}</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="member in detail.members" :key="member.id">
              <td>
                <div class="identity-cell">
                  <UserAvatar :size="34" :username="member.username" :src="member.avatar_url" />
                  <div>
                    <strong>{{ member.username }}</strong>
                    <small v-if="member.email">{{ member.email }}</small>
                  </div>
                </div>
              </td>
              <td>
                <div class="member-role-tags">
                  <NTag size="small" :bordered="false" :type="member.role === 'owner' ? 'success' : 'default'">{{ member.role === 'owner' ? '房主' : '成员' }}</NTag>
                  <NTag v-if="!isShared && member.share_basis_points === 0" size="small" :bordered="false" type="warning">仅查看</NTag>
                </div>
              </td>
              <td>
                <SharePicker
                  v-if="canManage && !isShared"
                  :model-value="shareDrafts[member.id]"
                  compact
                  :aria-label="`${member.username} 的份额`"
                  @update:model-value="emit('updateShareDraft', member.id, $event)"
                />
                <span v-else>{{ isShared ? '共享使用' : formatShareBasisPoints(member.share_basis_points) }}</span>
              </td>
              <td v-if="canManage">
                <div class="member-actions">
                  <NPopconfirm
                    v-if="isSettingMemberToViewOnly(member)"
                    positive-text="设为仅查看"
                    negative-text="取消"
                    @positive-click="emit('saveShare', member)"
                  >
                    <template #trigger>
                      <NButton secondary class="icon-button" title="保存份额" aria-label="保存份额" :loading="actionLoading === `share-${member.id}`">
                        <template #icon><Save :size="17" /></template>
                      </NButton>
                    </template>
                    设为 0% 后，{{ member.username }} 仍是 Plan 成员并可查看 Plan；{{ isPublicRecruitMember(member.id) ? '仍会占用 1 个公开招募名额。' : '不会再通过此 Plan 发起请求。' }}
                  </NPopconfirm>
                  <NButton
                    v-else-if="!isShared"
                    secondary
                    class="icon-button"
                    title="保存份额"
                    aria-label="保存份额"
                    :loading="actionLoading === `share-${member.id}`"
                    @click="emit('saveShare', member)"
                  >
                    <template #icon><Save :size="17" /></template>
                  </NButton>
                  <NPopconfirm
                    v-if="member.role !== 'owner'"
                    positive-text="移除成员"
                    negative-text="取消"
                    @positive-click="emit('removeMember', member)"
                  >
                    <template #trigger>
                      <NButton
                        quaternary
                        type="error"
                        class="icon-button"
                        title="移除成员"
                        aria-label="移除成员"
                        :loading="actionLoading === `remove-${member.id}`"
                      >
                        <template #icon><UserMinus :size="17" /></template>
                      </NButton>
                    </template>
                    移除 {{ member.username }} 后，其 API Key 将停止使用这个 Plan{{ isPublicRecruitMember(member.id) ? '，并释放 1 个公开招募名额' : '' }}。
                  </NPopconfirm>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="canManage && detail.applications.length" class="applications-section">
      <div class="section-heading">
        <div><h3>加入申请</h3><p>公开招募名额剩余 {{ availablePublicSlots }} 个</p></div>
      </div>
      <div class="application-list">
        <div v-for="application in detail.applications" :key="application.id" class="application-row">
          <div class="identity-cell">
            <UserAvatar :size="34" :username="application.username" :src="application.avatar_url" />
            <div>
              <strong>{{ application.username }}</strong>
              <small>{{ application.email }}</small>
              <p v-if="application.message">{{ application.message }}</p>
            </div>
          </div>
          <StatusBadge :value="application.status" />
          <div v-if="application.status === 'pending'" class="row-actions">
            <NButton quaternary type="success" class="icon-button" title="批准" aria-label="批准" :disabled="applicationReviewBusy(application.id)" :loading="actionLoading === `application-approve-${application.id}`" @click="emit('review', application.id, 'approve')">
              <template #icon><Check :size="17" /></template>
            </NButton>
            <NPopconfirm positive-text="拒绝申请" negative-text="取消" @positive-click="emit('review', application.id, 'reject')">
              <template #trigger>
                <NButton quaternary type="error" class="icon-button" title="拒绝" aria-label="拒绝" :disabled="applicationReviewBusy(application.id)" :loading="actionLoading === `application-reject-${application.id}`">
                  <template #icon><X :size="17" /></template>
                </NButton>
              </template>
              拒绝后对方仍可重新提交申请。
            </NPopconfirm>
          </div>
        </div>
      </div>
    </section>

    <section v-if="canManage && detail.invites.length" class="invites-section">
      <div class="section-heading">
        <div><h3>邀请记录</h3><p>待接受的邀请可以随时撤销</p></div>
      </div>
      <div class="invite-list">
        <div v-for="invite in detail.invites" :key="invite.id" class="invite-row">
          <div class="invite-recipient">
            <strong>一次性邀请链接</strong>
            <small>{{ formatPlanAuditDate(invite.created_at) }} 创建 · 有效期至 {{ formatPlanAuditDate(invite.expires_at) }}</small>
          </div>
          <span>{{ isShared ? '共享使用' : formatShareBasisPoints(invite.share_basis_points) }}</span>
          <StatusBadge :value="invite.status" />
          <NPopconfirm
            v-if="invite.status === 'pending'"
            positive-text="撤销邀请"
            negative-text="取消"
            @positive-click="emit('revokeInvite', invite.id)"
          >
            <template #trigger>
              <NButton quaternary type="error" class="icon-button" title="撤销邀请" aria-label="撤销邀请" :loading="actionLoading === `invite-${invite.id}`">
                <template #icon><Link2Off :size="17" /></template>
              </NButton>
            </template>
            撤销后，这条邀请链接将立即失效。
          </NPopconfirm>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { NButton, NPopconfirm, NTag } from 'naive-ui'
import { Check, Link2Off, LogOut, Save, UserMinus, UserPlus, X } from 'lucide-vue-next'
import SharePicker from '../components/SharePicker.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UserAvatar from '../components/UserAvatar.vue'
import { formatShareBasisPoints } from '../planAllocation'
import type { Member, PlanDetail } from '../types'
import { formatPlanAuditDate } from './planViewPresentation'

const props = defineProps<{
  detail: PlanDetail
  canManage: boolean
  isArchived: boolean
  isShared: boolean
  allocatedShare: string
  currentMember?: Member
  actionLoading: string
  shareDrafts: Record<string, number>
  availablePublicSlots: number
}>()

const emit = defineEmits<{
  openInvite: []
  leavePlan: []
  updateShareDraft: [memberID: string, value: number]
  saveShare: [member: Member]
  removeMember: [member: Member]
  review: [id: string, decision: 'approve' | 'reject']
  revokeInvite: [inviteID: string]
}>()

function isSettingMemberToViewOnly(member: Member) {
  return !props.isShared && member.share_basis_points > 0 && props.shareDrafts[member.id] === 0
}

function isPublicRecruitMember(memberID: string) {
  return props.detail.applications.some(application =>
    application.status === 'approved' && application.member_id === memberID,
  )
}

function applicationReviewBusy(id: string) {
  return props.actionLoading === `application-approve-${id}` || props.actionLoading === `application-reject-${id}`
}
</script>

<style scoped src="./PlanMembersTab.css"></style>
