<template>
  <section class="view-content plans-layout">
    <main class="plan-detail-pane">
      <div v-if="!detail && planLoading" class="plan-detail-loading" aria-label="正在加载 Plan">
        <NSpin size="small" />
      </div>

      <div v-else-if="!detail" class="plan-empty-state">
        <div class="plan-empty-actions">
          <NButton type="primary" :disabled="availableAccounts.length === 0" @click="openCreate">
            <template #icon><Plus :size="17" /></template>
            创建 Plan
          </NButton>
        </div>
        <EmptyState title="还没有 Plan" :description="availableAccounts.length ? '创建一个共享空间，然后把邀请链接发给朋友。' : '请先添加一个尚未绑定 Plan 的 OpenAI 账号。'" :icon="Layers3" />
      </div>

      <template v-else>
        <header class="plan-detail-header">
          <div class="plan-heading-main">
            <span class="plan-heading-icon"><Layers3 :size="24" /></span>
            <div>
              <div class="eyebrow">
                <NTag size="small" :bordered="false" :type="isArchived ? 'warning' : 'success'">{{ isArchived ? '已归档' : '正常' }}</NTag>
                <StatusBadge :value="detail.plan.visibility" />
                <span>{{ allocationModeLabel(detail.plan.allocation_mode) }}</span>
                <span>{{ detail.account.plan_type }}</span>
              </div>
              <h2>{{ detail.plan.name }}</h2>
              <p>
                {{ detail.account.name }} · {{ detail.account.email }}
                <template v-if="!isOwner"> · 由 {{ owner!.username }} 提供</template>
              </p>
            </div>
          </div>
          <div class="plan-heading-side">
            <NSelect
              v-if="plans.length > 1"
              class="plan-switcher"
              size="small"
              filterable
              to="body"
              :value="detail.plan.id"
              :options="planOptions"
              :loading="planLoading"
              aria-label="切换 Plan"
              @update:value="loadPlan"
            />
            <span><UsersRound :size="16" />{{ detail.members.length }} 位成员</span>
            <div class="plan-heading-actions">
              <NButton
                secondary
                class="icon-button"
                title="创建 Plan"
                aria-label="创建 Plan"
                :disabled="availableAccounts.length === 0"
                @click="openCreate"
              >
                <template #icon><Plus :size="18" /></template>
              </NButton>
              <NButton
                secondary
                class="icon-button"
                title="刷新 Plan"
                aria-label="刷新 Plan"
                :loading="planLoading"
                @click="loadPlan(detail.plan.id)"
              >
                <template #icon><RefreshCw :size="18" /></template>
              </NButton>
            </div>
          </div>
        </header>

        <NAlert v-if="isArchived" class="archived-alert" type="warning" :show-icon="true">
          这个 Plan 已归档，请求路由已暂停。房主恢复后才能继续使用。
        </NAlert>

        <NTabs v-model:value="activeTab" type="segment" animated class="detail-tabs" @update:value="handleTabChange">
          <NTabPane name="overview">
            <template #tab><span class="tab-label"><ChartNoAxesCombined :size="16" />概览</span></template>
            <div class="tab-panel">
              <PlanInsights
                :insights="detail.insights"
                :members="detail.members"
                :allocation-mode="detail.plan.allocation_mode"
                :can-refresh="isOwner && !isArchived"
                :refreshing="quotaRefreshing"
                @refresh="refreshQuota"
              />
            </div>
          </NTabPane>

          <NTabPane name="account">
            <template #tab><span class="tab-label"><Bot :size="16" />账号配置</span></template>
            <div class="tab-panel"><AccountConfigSummary :account="detail.account" /></div>
          </NTabPane>

          <NTabPane name="members">
            <template #tab>
              <span class="tab-label"><UsersRound :size="16" />成员<span class="tab-count">{{ detail.members.length }}</span></span>
            </template>
            <div class="tab-panel members-tab">
              <section class="members-section">
                <div class="section-heading">
                  <div>
                    <h3>成员</h3>
                    <p>{{ isShared ? `${detail.members.length} 位成员共享账号额度，不设置个人上限` : `${detail.members.length} 位成员，固定份额合计 ${allocatedShare}` }}</p>
                  </div>
                  <NButton v-if="isOwner && !isArchived" type="primary" @click="showInviteComposer = true">
                    <template #icon><UserPlus :size="16" /></template>
                    邀请成员
                  </NButton>
                  <NPopconfirm
                    v-if="!isOwner"
                    positive-text="退出 Plan"
                    negative-text="取消"
                    @positive-click="leavePlan"
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
                        <th v-if="isOwner">操作</th>
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
                        <td><NTag size="small" :bordered="false" :type="member.role === 'owner' ? 'success' : 'default'">{{ member.role === 'owner' ? '房主' : '成员' }}</NTag></td>
                        <td>
                          <SharePicker
                            v-if="isOwner && !isShared"
                            v-model="shareDrafts[member.id]"
                            compact
                            :aria-label="`${member.username} 的份额`"
                          />
                          <span v-else>{{ isShared ? '共享使用' : formatShareBasisPoints(member.share_basis_points) }}</span>
                        </td>
                        <td v-if="isOwner">
                          <div class="member-actions">
                            <NButton
                              v-if="!isShared"
                              secondary
                              class="icon-button"
                              title="保存份额"
                              aria-label="保存份额"
                              :loading="actionLoading === `share-${member.id}`"
                              @click="saveShare(member)"
                            >
                              <template #icon><Save :size="17" /></template>
                            </NButton>
                            <NPopconfirm
                              v-if="member.role !== 'owner'"
                              positive-text="移除成员"
                              negative-text="取消"
                              @positive-click="removeMember(member)"
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
                              移除 {{ member.username }} 后，其 API Key 将停止使用这个 Plan。
                            </NPopconfirm>
                          </div>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>

              <section v-if="isOwner && detail.applications.length" class="applications-section">
                <div class="section-heading">
                  <div><h3>加入申请</h3><p>公开席位剩余 {{ availablePublicSlots }} 个</p></div>
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
                      <NButton quaternary type="success" class="icon-button" title="批准" aria-label="批准" :disabled="applicationReviewBusy(application.id)" :loading="actionLoading === `application-approve-${application.id}`" @click="review(application.id, 'approve')">
                        <template #icon><Check :size="17" /></template>
                      </NButton>
                      <NPopconfirm positive-text="拒绝申请" negative-text="取消" @positive-click="review(application.id, 'reject')">
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

              <section v-if="isOwner && detail.invites.length" class="invites-section">
                <div class="section-heading">
                  <div><h3>邀请记录</h3><p>待接受的邀请可以随时撤销</p></div>
                </div>
                <div class="invite-list">
                  <div v-for="invite in detail.invites" :key="invite.id" class="invite-row">
                    <div class="invite-recipient">
                      <strong>一次性邀请链接</strong>
                      <small>{{ formatDate(invite.created_at) }} 创建 · 有效期至 {{ formatDate(invite.expires_at) }}</small>
                    </div>
                    <span>{{ isShared ? '共享使用' : formatShareBasisPoints(invite.share_basis_points) }}</span>
                    <StatusBadge :value="invite.status" />
                    <NPopconfirm
                      v-if="invite.status === 'pending'"
                      positive-text="撤销邀请"
                      negative-text="取消"
                      @positive-click="revokeInvite(invite.id)"
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
          </NTabPane>

          <NTabPane name="activity">
            <template #tab><span class="tab-label"><History :size="16" />活动</span></template>
            <div class="tab-panel activity-tab">
              <div class="section-heading">
                <div><h3>Plan 活动</h3><p>所有成员都可以查看最近 100 条变更记录</p></div>
                <NButton secondary class="icon-button" title="刷新活动" aria-label="刷新活动" :loading="auditLoading" @click="loadAudit(detail.plan.id)">
                  <template #icon><RefreshCw :size="17" /></template>
                </NButton>
              </div>
              <div v-if="auditLoading && auditEvents.length === 0" class="activity-loading"><NSpin size="small" /></div>
              <NEmpty v-else-if="auditEvents.length === 0" description="还没有活动记录" />
              <NTimeline v-else class="activity-timeline">
                <NTimelineItem v-for="event in auditEvents" :key="event.id" :time="formatDate(event.created_at)">
                  <template #header>
                    <div class="activity-title"><strong>{{ actionLabels[event.action] }}</strong><NTag size="tiny" :bordered="false">{{ event.action }}</NTag></div>
                  </template>
                  <div class="activity-body">
                    <span>{{ event.actor_username }}</span>
                    <dl v-if="Object.keys(event.metadata).length" class="activity-metadata">
                      <div v-for="(value, key) in event.metadata" :key="key"><dt>{{ metadataLabels[key] }}</dt><dd>{{ formatMetadata(key, value) }}</dd></div>
                    </dl>
                  </div>
                </NTimelineItem>
              </NTimeline>
            </div>
          </NTabPane>

          <NTabPane v-if="isOwner" name="settings">
            <template #tab><span class="tab-label"><SlidersHorizontal :size="16" />设置</span></template>
            <div class="tab-panel owner-controls">
              <div class="plan-settings">
                <section class="settings-group">
                  <header class="settings-group-heading">
                    <div><span>常规</span><h3>基本信息</h3></div>
                    <p>这些信息会展示给 Plan 内的所有成员。</p>
                  </header>

                  <form class="setting-row" @submit.prevent="renamePlan">
                    <div class="setting-identity">
                      <span class="setting-icon"><PencilLine :size="18" /></span>
                      <div><h4>Plan 名称</h4><p>修改后会立即同步给所有成员</p></div>
                    </div>
                    <div class="setting-action setting-action-inline">
                      <AppInput :value="renameDraft" clearable :maxlength="100" placeholder="Plan 名称" aria-label="Plan 名称" @update:value="updateRenameDraft" />
                      <NButton attr-type="submit" secondary :disabled="!canRename" :loading="actionLoading === 'rename'">
                        <template #icon><Save :size="16" /></template>
                        保存
                      </NButton>
                    </div>
                  </form>
                </section>

                <section v-if="!isArchived" class="settings-group">
                  <header class="settings-group-heading">
                    <div><span>发现</span><h3>公开加入</h3></div>
                    <p>控制 Plan 是否展示在探索大厅，以及公开席位数量。</p>
                  </header>

                  <form class="setting-row publication-control" @submit.prevent="savePublication">
                    <div class="setting-identity">
                      <span class="setting-icon setting-icon-teal"><Store :size="18" /></span>
                      <div><h4>探索大厅</h4><p>{{ isShared ? '允许其他用户申请共享席位' : '允许其他用户申请固定席位' }}</p></div>
                    </div>
                    <div class="setting-action">
                      <div class="publication-toggle">
                        <div><strong>公开展示</strong><small>{{ publication.visibility === 'public' ? '当前已在大厅展示' : '当前仅 Plan 成员可见' }}</small></div>
                        <NSwitch :value="publication.visibility === 'public'" @update:value="setPublicationVisibility" />
                      </div>
                      <div v-if="publication.visibility === 'public'" class="setting-fields" :class="{ single: isShared }">
                        <label>公开席位<NInputNumber :value="publication.slots" :min="1" :max="100" :precision="0" @update:value="updatePublicationSlots" /></label>
                        <label v-if="!isShared">每席份额<SharePicker :model-value="publication.share" aria-label="大厅每席份额" @update:model-value="updatePublicationShare" /></label>
                      </div>
                      <NButton attr-type="submit" secondary class="setting-submit" :disabled="!canSavePublication" :loading="actionLoading === 'publication'">
                        <template #icon><Save :size="16" /></template>
                        保存设置
                      </NButton>
                    </div>
                  </form>
                </section>

                <section class="settings-group">
                  <header class="settings-group-heading">
                    <div><span>管理</span><h3>Plan 管理</h3></div>
                    <p>以下操作会影响 Plan 的归属或请求路由。</p>
                  </header>

                  <div class="setting-row">
                    <div class="setting-identity">
                      <span class="setting-icon"><Replace :size="18" /></span>
                      <div><h4>更换 OpenAI 账号</h4><p>成员和 API Key 保持不变，请求会切换到新账号</p></div>
                    </div>
                    <div class="setting-action setting-action-inline">
                      <NSelect :value="rebindAccountID" :options="rebindAccountOptions" to="body" placeholder="选择尚未绑定的账号" aria-label="新 OpenAI 账号" @update:value="updateRebindAccount" />
                      <NPopconfirm positive-text="确认更换" negative-text="取消" @positive-click="rebindAccount">
                        <template #trigger>
                          <NButton secondary :disabled="!rebindAccountID" :loading="actionLoading === 'rebind'">
                            <template #icon><Replace :size="16" /></template>
                            更换
                          </NButton>
                        </template>
                        Plan 的请求将立即切换到新账号。
                      </NPopconfirm>
                    </div>
                  </div>

                  <div class="setting-row">
                    <div class="setting-identity">
                      <span class="setting-icon setting-icon-amber"><Crown :size="18" /></span>
                      <div><h4>转让所有权</h4><p>新房主将获得管理权限，OpenAI 账号所有权不会转移</p></div>
                    </div>
                    <div class="setting-action setting-action-inline">
                      <NSelect :value="transferMemberID" :options="transferMemberOptions" to="body" placeholder="选择一位成员" aria-label="新房主" @update:value="updateTransferMember" />
                      <NPopconfirm positive-text="确认转让" negative-text="取消" @positive-click="transferOwnership">
                        <template #trigger>
                          <NButton secondary :disabled="!transferMemberID" :loading="actionLoading === 'transfer'">
                            <template #icon><Crown :size="16" /></template>
                            转让
                          </NButton>
                        </template>
                        转让后你将成为普通成员，且无法自行撤销；OpenAI 账号所有权不会转移。
                      </NPopconfirm>
                    </div>
                  </div>
                </section>

                <section class="settings-group settings-group-danger">
                  <header class="settings-group-heading">
                    <div><span>生命周期</span><h3>归档与删除</h3></div>
                    <p>归档可恢复；永久删除仅在归档后提供。</p>
                  </header>

                  <div class="setting-row lifecycle-row" :class="{ archived: isArchived }">
                    <div class="setting-identity">
                      <span class="setting-icon setting-icon-danger"><Archive :size="18" /></span>
                      <div><h4>{{ isArchived ? 'Plan 已归档' : '归档 Plan' }}</h4><p>{{ isArchived ? '请求路由已暂停，可以恢复或永久删除' : '暂停所有请求路由，并关闭待处理邀请与申请' }}</p></div>
                    </div>
                    <div class="lifecycle-actions">
                      <NPopconfirm v-if="!isArchived" positive-text="归档 Plan" negative-text="取消" @positive-click="updatePlanStatus('archived')">
                        <template #trigger>
                          <NButton secondary type="warning" :loading="actionLoading === 'status-archived'">
                            <template #icon><Archive :size="16" /></template>
                            归档 Plan
                          </NButton>
                        </template>
                        归档后 API Key 会暂停使用此 Plan，待处理邀请与申请会关闭。
                      </NPopconfirm>
                      <NButton v-else secondary type="success" :loading="actionLoading === 'status-active'" @click="updatePlanStatus('active')">
                        <template #icon><ArchiveRestore :size="16" /></template>
                        恢复 Plan
                      </NButton>
                      <NButton v-if="isArchived" secondary type="error" @click="showDeleteConfirmOne = true">
                        <template #icon><Trash2 :size="16" /></template>
                        永久删除
                      </NButton>
                    </div>
                  </div>
                </section>
              </div>
            </div>
          </NTabPane>
        </NTabs>
      </template>
    </main>
  </section>

  <ModalShell v-if="showCreate" title="创建共享 Plan" subtitle="额度方式创建后不可更改" @close="showCreate = false">
    <label>Plan 名称<AppInput :value="createForm.name" clearable :maxlength="100" placeholder="给这个共享空间起个名字" @update:value="updateCreateName" /></label>
    <label>OpenAI 账号<NSelect :value="createForm.accountID" :options="accountOptions" to="body" @update:value="updateCreateAccount" /></label>
    <label>额度方式
      <NRadioGroup :value="createForm.allocationMode" class="allocation-picker" @update:value="updateCreateAllocationMode">
        <NRadioButton value="fixed">固定分配</NRadioButton>
        <NRadioButton value="shared">共享使用</NRadioButton>
      </NRadioGroup>
    </label>
    <div class="allocation-note">
      <strong>{{ createForm.allocationMode === 'shared' ? '成员共享账号总额度' : '为每位成员分配固定份额' }}</strong>
      <span>{{ createForm.allocationMode === 'shared' ? '不设置个人额度上限，账号总额度耗尽后停止使用' : '每位成员用完自己的份额后停止使用' }}</span>
    </div>
    <label v-if="createForm.allocationMode === 'fixed'">房主份额<SharePicker :model-value="createForm.share" aria-label="房主份额" @update:model-value="updateCreateShare" /></label>
    <template #footer>
      <NButton @click="showCreate = false">取消</NButton>
      <NButton type="primary" :disabled="!createForm.name.trim() || !createForm.accountID" :loading="actionLoading === 'create-plan'" @click="createPlan">
        <template #icon><Check :size="17" /></template>
        创建
      </NButton>
    </template>
  </ModalShell>

  <ModalShell v-if="showInviteComposer && detail" title="邀请成员" subtitle="创建一条 7 天内有效、仅可领取一次的链接" @close="showInviteComposer = false">
    <div class="invite-composer">
      <div class="invite-mode-summary">
        <span><Link2 :size="19" /></span>
        <div>
          <strong>{{ isShared ? '共享使用' : '固定分配' }}</strong>
          <small>{{ isShared ? '新成员与其他成员共同使用账号总额度' : '为通过该链接加入的成员预留固定份额' }}</small>
        </div>
      </div>
      <label v-if="!isShared">新成员份额<SharePicker :model-value="inviteForm.share" aria-label="受邀成员份额" @update:model-value="updateInviteShare" /></label>
      <NAlert type="warning" :show-icon="true">任何拿到链接并完成登录或注册的用户都可以领取，请只通过可信渠道发送。</NAlert>
    </div>
    <template #footer>
      <NButton @click="showInviteComposer = false">取消</NButton>
      <NButton type="primary" :loading="actionLoading === 'create-invite'" @click="sendInvite">
        <template #icon><UserPlus :size="17" /></template>
        生成链接
      </NButton>
    </template>
  </ModalShell>

  <ModalShell v-if="inviteSecret" title="邀请链接已创建" subtitle="链接只能使用一次，请通过可信渠道发送" @close="inviteSecret = ''">
    <div class="invite-link-preview"><Link2 :size="18" /><code>{{ inviteSecret }}</code></div>
    <template #footer>
      <NButton @click="inviteSecret = ''">完成</NButton>
      <NButton type="primary" @click="copyInvite"><template #icon><Copy :size="17" /></template>复制链接</NButton>
    </template>
  </ModalShell>

  <ModalShell v-if="showDeleteConfirmOne" title="删除归档 Plan" subtitle="这会永久删除成员关系、路由和 Plan 数据" @close="showDeleteConfirmOne = false">
    <NAlert type="error" :show-icon="true">删除无法撤销。所有成员将立即失去访问权限。</NAlert>
    <template #footer>
      <NButton @click="showDeleteConfirmOne = false">取消</NButton>
      <NButton type="error" @click="continueDelete">继续</NButton>
    </template>
  </ModalShell>

  <ModalShell v-if="showDeleteConfirmTwo && detail" title="最后确认" :subtitle="`输入“${detail.plan.name}”确认永久删除`" @close="closeDeleteDialogs">
    <label>Plan 名称<AppInput :value="deleteNameDraft" clearable placeholder="手动输入上方显示的 Plan 名称" @update:value="updateDeleteNameDraft" /></label>
    <template #footer>
      <NButton @click="closeDeleteDialogs">取消</NButton>
      <NButton type="error" :disabled="!canConfirmDelete" :loading="actionLoading === 'delete'" @click="deletePlan">
        <template #icon><Trash2 :size="17" /></template>
        永久删除
      </NButton>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import {
  NAlert,
  NButton,
  NEmpty,
  NInputNumber,
  NPopconfirm,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSpin,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NTimeline,
  NTimelineItem,
} from 'naive-ui'
import { computed, reactive, ref, watch } from 'vue'
import AppInput from '../components/AppInput.vue'
import {
  Archive,
  ArchiveRestore,
  Bot,
  ChartNoAxesCombined,
  Check,
  Copy,
  Crown,
  History,
  Layers3,
  Link2,
  Link2Off,
  LogOut,
  PencilLine,
  Plus,
  RefreshCw,
  Replace,
  Save,
  SlidersHorizontal,
  Store,
  Trash2,
  UserMinus,
  UserPlus,
  UsersRound,
  X,
} from 'lucide-vue-next'
import { APIRequestError, api } from '../api'
import { allocationModeLabel, allocationShareBasisPoints, formatShareBasisPoints } from '../planAllocation'
import type { Account, AuditEvent, Member, Plan, PlanAllocationMode, PlanDetail, User } from '../types'
import AccountConfigSummary from '../components/AccountConfigSummary.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalShell from '../components/ModalShell.vue'
import PlanInsights from '../components/PlanInsights.vue'
import SharePicker from '../components/SharePicker.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UserAvatar from '../components/UserAvatar.vue'

const props = withDefaults(defineProps<{
  accounts: Account[]
  plans: Plan[]
  user: User
  initialPlanId?: string
  invitePlanId?: string
}>(), {
  initialPlanId: '',
  invitePlanId: '',
})

const emit = defineEmits<{
  changed: []
  inviteOpened: []
  message: [type: 'success' | 'error', text: string]
}>()

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
</script>

<style scoped>
.archived-alert {
  margin-top: 18px;
}

.members-tab,
.activity-tab,
.owner-controls {
  min-width: 0;
}

.member-actions,
.lifecycle-actions {
  display: flex;
  align-items: center;
  gap: 7px;
}

.invite-list {
  margin-top: 12px;
  border-top: 1px solid var(--line);
}

.invite-row {
  min-height: 62px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 105px 84px 38px;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--line-muted);
  color: var(--ink);
  font-size: 11px;
}

.invite-recipient {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.invite-recipient strong,
.invite-recipient small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.invite-recipient strong {
  color: var(--ink-strong);
}

.invite-recipient small {
  color: var(--muted);
  font-size: 9px;
}

.plan-settings {
  width: min(1080px, 100%);
  display: grid;
  gap: 34px;
}

.settings-group {
  min-width: 0;
}

.settings-group-heading {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 460px);
  align-items: end;
  gap: 28px;
  padding: 0 2px 13px;
  border-bottom: 1px solid var(--line-strong);
}

.settings-group-heading > div {
  display: grid;
  gap: 5px;
}

.settings-group-heading span {
  color: var(--primary);
  font-size: 9px;
  font-weight: 800;
}

.settings-group-heading h3 {
  margin: 0;
  color: var(--ink-strong);
  font-size: 15px;
}

.settings-group-heading p {
  margin: 0;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.55;
}

.setting-row {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(260px, 1fr) minmax(380px, 460px);
  align-items: start;
  gap: 28px;
  padding: 22px 2px;
  border-bottom: 1px solid var(--line-soft);
}

.setting-identity {
  min-width: 0;
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr);
  align-items: start;
  gap: 12px;
}

.setting-icon {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  background: var(--blue-soft);
  color: var(--blue);
}

.setting-icon-teal {
  background: var(--teal-soft);
  color: var(--teal);
}

.setting-icon-amber {
  background: var(--amber-soft);
  color: var(--amber);
}

.setting-icon-danger {
  background: var(--red-soft);
  color: var(--red);
}

.setting-identity h4 {
  margin: 1px 0 0;
  color: var(--ink-strong);
  font-size: 12px;
}

.setting-identity p {
  max-width: 460px;
  margin: 5px 0 0;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.55;
}

.setting-action {
  min-width: 0;
  display: grid;
  gap: 12px;
}

.setting-action-inline {
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
}

.setting-action-inline > .n-button {
  min-width: 88px;
}

.setting-fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 128px;
  gap: 10px;
}

.setting-fields.single {
  grid-template-columns: 1fr;
}

.setting-submit {
  justify-self: end;
}

.publication-toggle {
  min-height: 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.publication-toggle > div {
  display: grid;
  gap: 3px;
}

.publication-toggle strong {
  color: var(--ink);
  font-size: 11px;
}

.publication-toggle small {
  color: var(--muted);
  font-size: 9px;
}

.settings-group-danger .settings-group-heading span {
  color: var(--red);
}

.lifecycle-row.archived {
  margin-top: 14px;
  padding: 18px 16px;
  border: 1px solid var(--danger-border);
  border-radius: 7px;
  background: var(--red-soft);
}

.lifecycle-actions {
  justify-content: flex-end;
  align-self: center;
}

.invite-composer {
  display: grid;
  gap: 18px;
}

.invite-composer > label {
  display: grid;
  gap: 7px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 700;
}

.invite-mode-summary {
  min-width: 0;
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr);
  align-items: center;
  gap: 11px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--line-soft);
}

.invite-mode-summary > span {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  background: var(--primary-soft);
  color: var(--primary);
}

.invite-mode-summary > div {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.invite-mode-summary strong {
  color: var(--ink-strong);
  font-size: 12px;
}

.invite-mode-summary small {
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}

.invite-link-preview {
  min-width: 0;
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  align-items: start;
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-soft);
  color: var(--primary);
}

.invite-link-preview code {
  color: var(--ink);
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.activity-loading {
  min-height: 220px;
  display: grid;
  place-items: center;
}

.activity-tab > .n-empty {
  margin-top: 80px;
}

.activity-timeline {
  margin-top: 22px;
  padding: 0 4px;
}

.activity-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.activity-title strong {
  color: var(--ink-strong);
  font-size: 12px;
}

.activity-body {
  display: grid;
  gap: 9px;
  padding: 5px 0 8px;
  color: var(--muted);
  font-size: 10px;
}

.activity-metadata {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  margin: 0;
}

.activity-metadata > div {
  display: flex;
  align-items: center;
  gap: 5px;
}

.activity-metadata dt {
  color: var(--muted-light);
}

.activity-metadata dd {
  margin: 0;
  color: var(--ink);
  overflow-wrap: anywhere;
}

@media (max-width: 960px) {
  .settings-group-heading,
  .setting-row {
    grid-template-columns: minmax(220px, 1fr) minmax(340px, 420px);
    gap: 22px;
  }
}

@media (max-width: 640px) {
  .members-section .section-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .members-section .section-heading > .n-button,
  .members-section .section-heading > .n-popconfirm {
    align-self: flex-end;
  }

  .invite-row {
    grid-template-columns: minmax(0, 1fr) auto 38px;
    gap: 8px;
  }

  .invite-row > span:nth-child(2) {
    display: none;
  }

  .lifecycle-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .lifecycle-actions .n-button {
    width: 100%;
  }

  .settings-group-heading,
  .setting-row {
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .settings-group-heading {
    align-items: start;
  }

  .setting-row {
    padding: 19px 2px;
  }

  .lifecycle-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 440px) {
  .setting-action-inline,
  .setting-fields {
    grid-template-columns: 1fr;
  }

  .setting-action-inline > .n-button,
  .setting-submit {
    width: 100%;
  }
}
</style>
