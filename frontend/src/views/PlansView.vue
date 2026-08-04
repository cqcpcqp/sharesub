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
                :key="detail.plan.id"
                :insights="detail.insights"
                :members="detail.members"
                :allocation-mode="detail.plan.allocation_mode"
                :can-refresh="isOwner && !isArchived"
                :refreshing="quotaRefreshing"
                :performance-period="performancePeriod"
                :performance-loading="performanceLoading"
                :theme="theme"
                @refresh="refreshQuota"
                @update:performance-period="loadPerformance"
              />
            </div>
          </NTabPane>

          <NTabPane name="account">
            <template #tab><span class="tab-label"><Bot :size="16" />账号配置</span></template>
            <div class="tab-panel"><AccountConfigSummary :account="detail.account" :members="detail.members" /></div>
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
import { allocationModeLabel, formatShareBasisPoints } from '../planAllocation'
import type { Account, Plan, User } from '../types'
import type { ResolvedTheme } from '../themePreference'
import AccountConfigSummary from '../components/AccountConfigSummary.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalShell from '../components/ModalShell.vue'
import PlanInsights from '../components/PlanInsights.vue'
import SharePicker from '../components/SharePicker.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UserAvatar from '../components/UserAvatar.vue'
import { usePlansView } from './usePlansView'

const props = withDefaults(defineProps<{
  accounts: Account[]
  plans: Plan[]
  user: User
  theme: ResolvedTheme
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

const {
  detail, planLoading, quotaRefreshing, performanceLoading, performancePeriod, actionLoading, activeTab, auditEvents, auditLoading,
  availableAccounts, loadPlan, loadAudit, loadPerformance,
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
} = usePlansView(props, emit)
</script>

<style scoped src="./PlansView.css"></style>
