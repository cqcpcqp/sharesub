<template>
  <section class="view-content lobby-view">
    <template v-if="detailTarget">
      <div class="public-plan-detail-nav">
        <NButton quaternary @click="closeDetail"><template #icon><ArrowLeft :size="17" /></template>返回探索大厅</NButton>
      </div>
      <div class="public-plan-detail-layout">
        <article class="public-plan-detail-main">
          <header>
            <div class="public-plan-detail-title">
              <div>
                <h2>{{ detailTarget.plan.name }}</h2>
                <div class="public-plan-detail-tags">
                  <NTag size="small" :bordered="false" type="success">公开 Plan</NTag>
                  <NTag size="small" :bordered="false">{{ detailTarget.plan.allocation_mode === 'shared' ? '共享额度' : '固定份额' }}</NTag>
                  <NTag v-if="!detailTarget.plan.account_id" size="small" :bordered="false" type="warning">筹备中</NTag>
                </div>
              </div>
            </div>
            <p class="public-plan-description">{{ detailTarget.plan.description || '房主暂未填写 Plan 介绍。' }}</p>
          </header>

          <section class="public-plan-owner-section">
            <h3>房主与账号</h3>
            <div class="public-plan-owner-row">
              <UserAvatar :size="46" :username="detailTarget.owner_username" :src="detailTarget.owner_avatar_url" />
              <div><strong>{{ detailTarget.owner_username }}</strong><span>Plan 房主</span></div>
              <div class="public-plan-account-state">
                <span>{{ detailTarget.plan.account_id ? `${detailTarget.plan_type} 账号` : '尚未绑定账号' }}</span>
                <strong>{{ detailTarget.plan.account_id ? (detailTarget.subscription_expires_at ? `订阅有效期至 ${formatSubscriptionDate(detailTarget.subscription_expires_at)}` : '暂无订阅有效期') : '加入后需等待房主接入账号' }}</strong>
              </div>
            </div>
          </section>

          <section class="public-plan-allocation-section">
            <h3>共享方式</h3>
            <p>{{ detailTarget.plan.allocation_mode === 'shared' ? '成员共同使用账号额度，不设置个人份额上限。' : detailTarget.plan.public_share_basis_points === 0 ? '公开加入后的份额为 0%：可以查看 Plan，但不能通过该 Plan 发起请求。' : `每位通过大厅加入的成员获得 ${formatShareBasisPoints(detailTarget.plan.public_share_basis_points)} 的固定份额。` }}</p>
            <div class="public-plan-detail-stats">
              <div><span>分配方式</span><strong>{{ detailTarget.plan.allocation_mode === 'shared' ? '共享额度' : '固定份额' }}</strong></div>
              <div><span>每人份额</span><strong>{{ detailTarget.plan.allocation_mode === 'shared' ? '不单独限制' : formatPublicShare(detailTarget.plan.public_share_basis_points) }}</strong></div>
              <div><span>当前成员</span><strong>{{ detailTarget.member_count }} 人</strong></div>
            </div>
          </section>
        </article>

        <aside class="public-plan-detail-side">
          <div class="public-plan-seat-heading"><div><span>公开招募名额</span><strong>{{ detailTarget.available_slots }} / {{ detailTarget.plan.public_slots }} 可申请</strong></div><UsersRound :size="21" /></div>
          <NProgress type="line" :percentage="seatUsage(detailTarget)" :show-indicator="false" :height="8" color="var(--teal)" rail-color="var(--line-soft)" />
          <p>{{ detailTarget.available_slots > 0 ? '提交申请后，需等待房主批准才能加入。' : '当前公开招募名额已满，暂时无法提交申请。' }}</p>
          <div class="public-plan-detail-action">
            <span v-if="detailTarget.plan.owner_user_id === user.id" class="owner-label"><Crown :size="15" />这是我的 Plan</span>
            <NButton
              v-else-if="canApplyToPublicPlan(detailTarget.application_status)"
              type="primary"
              :secondary="detailTarget.application_status === 'rejected'"
              block
              :disabled="detailTarget.available_slots === 0"
              @click="openApply(detailTarget)"
            >
              <template #icon><RotateCcw v-if="detailTarget.application_status === 'rejected'" :size="16" /><Send v-else :size="16" /></template>
              {{ detailTarget.application_status === 'rejected' ? '重新申请' : '申请加入' }}
            </NButton>
            <StatusBadge v-else :value="detailTarget.application_status" />
          </div>
        </aside>
      </div>
    </template>

    <template v-else>
      <div class="filter-row"><div class="search-box"><AppInput :value="query" size="small" clearable :input-props="{ 'aria-label': '搜索 Plan 或房主' }" placeholder="搜索 Plan 或房主" @update:value="updateQuery"><template #prefix><Search :size="16" /></template></AppInput></div><div class="lobby-summary"><NTag round size="small"><strong>{{ plans.length }}</strong> 个公开 Plan</NTag><NTag round size="small" type="success"><strong>{{ availableCount }}</strong> 个有空位</NTag></div></div>
      <div v-if="filteredPlans.length" class="lobby-grid">
        <article v-for="item in filteredPlans" :key="item.plan.id" class="plan-card">
          <div class="plan-card-main"><h3>{{ item.plan.name }}</h3><p v-if="item.plan.description" class="plan-card-description">{{ item.plan.description }}</p><div class="plan-owner"><UserAvatar :size="34" :username="item.owner_username" :src="item.owner_avatar_url" /><p><strong>{{ item.owner_username }}</strong><small>{{ item.plan.account_id ? `${item.plan_type} 账号` : '筹备中 · 尚未绑定账号' }}</small></p></div><div class="plan-subscription"><CalendarRange :size="14" /><span>{{ item.plan.account_id ? '订阅有效期至' : '服务状态' }}</span><strong>{{ item.plan.account_id ? (item.subscription_expires_at ? formatSubscriptionDate(item.subscription_expires_at) : '暂无订阅有效期') : '等待房主接入账号' }}</strong></div></div>
          <div class="seat-meter"><div><span>席位使用情况</span><strong>{{ item.available_slots }} 个空位</strong></div><NProgress type="line" :percentage="seatUsage(item)" :show-indicator="false" :height="7" color="var(--card-accent)" rail-color="var(--line-soft)" /></div>
          <div class="plan-stats"><span><strong>{{ item.plan.allocation_mode === 'shared' ? '共享' : formatPublicShare(item.plan.public_share_basis_points) }}</strong><small>{{ item.plan.allocation_mode === 'shared' ? '额度方式' : '每人份额' }}</small></span><span><strong>{{ item.plan.public_slots }}</strong><small>招募名额</small></span><span><strong>{{ item.member_count }}</strong><small>当前成员</small></span></div>
          <footer>
            <NButton secondary class="plan-detail-link" @click="openDetail(item)">查看详情<template #icon><ArrowRight :size="16" /></template></NButton>
            <span v-if="item.plan.owner_user_id === user.id" class="owner-label"><Crown :size="15" />我的 Plan</span>
            <NButton
              v-else-if="canApplyToPublicPlan(item.application_status)"
              type="primary"
              :secondary="item.application_status === 'rejected'"
              icon-placement="right"
              class="plan-apply"
              :disabled="item.available_slots === 0"
              @click="openApply(item)"
            >
              <template #icon><RotateCcw v-if="item.application_status === 'rejected'" :size="16" /><Send v-else :size="16" /></template>
              {{ item.application_status === 'rejected' ? '重新申请' : '申请加入' }}
            </NButton>
            <StatusBadge v-else :value="item.application_status" />
          </footer>
        </article>
      </div>
      <EmptyState v-else title="没有匹配的公开 Plan" description="公开 Plan 上架后会显示在这里。" :icon="Compass" />
    </template>
  </section>

  <ModalShell v-if="selected" title="申请加入 Plan" :subtitle="selected.plan.allocation_mode === 'shared' ? `${selected.plan.name} · 共享额度` : `${selected.plan.name} · 每席 ${formatPublicShare(selected.plan.public_share_basis_points)}`" @close="closeApply">
    <label>申请留言<AppInput :value="message" type="textarea" clearable :maxlength="500" show-count :autosize="{ minRows: 4, maxRows: 8 }" placeholder="向房主简单介绍你的使用需求（可选）" @update:value="updateMessage" /></label>
    <div class="info-note"><ShieldCheck :size="17" /><span>{{ selected.plan.account_id ? (selected.plan.allocation_mode === 'shared' ? '批准后与其他成员共同使用账号额度，不设置个人上限。' : '批准后会立即获得房主预设的固定份额，你无法自行修改。') : '这个 Plan 尚未绑定 OpenAI 账号。批准加入后需等待房主接入账号，才能开始使用。' }}</span></div>
    <template #footer><NButton :disabled="applying" @click="closeApply">取消</NButton><NButton type="primary" :loading="applying" @click="apply"><template #icon><Send :size="16" /></template>提交申请</NButton></template>
  </ModalShell>
</template>

<script setup lang="ts">
import { NButton, NProgress, NTag } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { ArrowLeft, ArrowRight, CalendarRange, Compass, Crown, RotateCcw, Search, Send, ShieldCheck, UsersRound } from 'lucide-vue-next'
import { api } from '../api'
import { formatShareBasisPoints } from '../planAllocation'
import { canApplyToPublicPlan } from '../publicPlanApplication'
import type { PublicPlan, User } from '../types'
import EmptyState from '../components/EmptyState.vue'
import AppInput from '../components/AppInput.vue'
import ModalShell from '../components/ModalShell.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UserAvatar from '../components/UserAvatar.vue'

const props = defineProps<{ plans: PublicPlan[]; user: User }>()
const emit = defineEmits<{ changed: []; message: [type: 'success' | 'error', text: string] }>()
const query = ref('')
const selected = ref<PublicPlan | null>(null)
const detailTarget = ref<PublicPlan | null>(null)
const message = ref('')
const applying = ref(false)
const availableCount = computed(() => props.plans.filter(item => item.available_slots > 0).length)
const filteredPlans = computed(() => { const needle = query.value.trim().toLowerCase(); return props.plans.filter(item => !needle || item.plan.name.toLowerCase().includes(needle) || item.plan.description.toLowerCase().includes(needle) || item.owner_username.toLowerCase().includes(needle)) })
watch(() => props.plans, plans => {
  if (!detailTarget.value) return
  detailTarget.value = plans.find(item => item.plan.id === detailTarget.value!.plan.id) ?? null
})
function updateQuery(value: string) { query.value = value }
function updateMessage(value: string) { message.value = value }
function openDetail(item: PublicPlan) { detailTarget.value = item }
function closeDetail() { detailTarget.value = null }
function openApply(item: PublicPlan) { selected.value = item; message.value = '' }
function closeApply() { if (!applying.value) selected.value = null }
async function apply() {
  if (!selected.value || applying.value) return
  applying.value = true
  try {
    await api.applyToPlan(selected.value.plan.id, message.value.trim())
    selected.value = null
    emit('message', 'success', '申请已提交，等待房主处理')
    emit('changed')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    applying.value = false
  }
}
function seatUsage(item: PublicPlan) { return ((item.plan.public_slots - item.available_slots) / item.plan.public_slots) * 100 }
function formatPublicShare(value: number) { return value === 0 ? '0% · 仅查看' : formatShareBasisPoints(value) }
function formatSubscriptionDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(new Date(value)) }
</script>
