<template>
  <section class="view-content lobby-view">
    <div class="filter-row"><div class="search-box"><AppInput :value="query" size="small" clearable :input-props="{ 'aria-label': '搜索 Plan 或房主' }" placeholder="搜索 Plan 或房主" @update:value="updateQuery"><template #prefix><Search :size="16" /></template></AppInput></div><div class="lobby-summary"><NTag round size="small"><strong>{{ plans.length }}</strong> 个公开 Plan</NTag><NTag round size="small" type="success"><strong>{{ availableCount }}</strong> 个有空位</NTag></div></div>
    <div v-if="filteredPlans.length" class="lobby-grid">
      <article v-for="item in filteredPlans" :key="item.plan.id" class="plan-card">
        <div class="plan-card-main"><h3>{{ item.plan.name }}</h3><p v-if="item.plan.description" class="plan-card-description">{{ item.plan.description }}</p><div class="plan-owner"><UserAvatar :size="34" :username="item.owner_username" :src="item.owner_avatar_url" /><p><strong>{{ item.owner_username }}</strong><small>{{ item.plan.account_id ? `${item.plan_type} 账号` : '筹备中 · 尚未绑定账号' }}</small></p></div><div class="plan-subscription"><CalendarRange :size="14" /><span>{{ item.plan.account_id ? '订阅有效期至' : '服务状态' }}</span><strong>{{ item.plan.account_id ? (item.subscription_expires_at ? formatSubscriptionDate(item.subscription_expires_at) : '暂无订阅有效期') : '等待房主接入账号' }}</strong></div></div>
        <div class="seat-meter"><div><span>席位使用情况</span><strong>{{ item.available_slots }} 个空位</strong></div><NProgress type="line" :percentage="seatUsage(item)" :show-indicator="false" :height="7" color="var(--card-accent)" rail-color="#e8eae6" /></div>
        <div class="plan-stats"><span><strong>{{ item.plan.allocation_mode === 'shared' ? '共享' : formatShareBasisPoints(item.plan.public_share_basis_points) }}</strong><small>{{ item.plan.allocation_mode === 'shared' ? '额度方式' : '每席份额' }}</small></span><span><strong>{{ item.plan.public_slots }}</strong><small>公开席位</small></span><span><strong>{{ item.member_count }}</strong><small>当前成员</small></span></div>
        <footer>
          <span v-if="item.plan.owner_user_id === user.id" class="owner-label"><Crown :size="15" />我的 Plan</span>
          <NButton
            v-else-if="canApplyToPublicPlan(item.application_status)"
            type="primary"
            :secondary="item.application_status === 'rejected'"
            block
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
  </section>

  <ModalShell v-if="selected" title="申请加入 Plan" :subtitle="selected.plan.allocation_mode === 'shared' ? `${selected.plan.name} · 共享额度` : `${selected.plan.name} · 每席 ${formatShareBasisPoints(selected.plan.public_share_basis_points)}`" @close="closeApply">
    <label>申请留言<AppInput :value="message" type="textarea" clearable :maxlength="500" show-count :autosize="{ minRows: 4, maxRows: 8 }" placeholder="向房主简单介绍你的使用需求（可选）" @update:value="updateMessage" /></label>
    <div class="info-note"><ShieldCheck :size="17" /><span>{{ selected.plan.account_id ? (selected.plan.allocation_mode === 'shared' ? '批准后与其他成员共同使用账号额度，不设置个人上限。' : '批准后会立即获得房主预设的固定份额，你无法自行修改。') : '这个 Plan 尚未绑定 OpenAI 账号。批准加入后需等待房主接入账号，才能开始使用。' }}</span></div>
    <template #footer><NButton :disabled="applying" @click="closeApply">取消</NButton><NButton type="primary" :loading="applying" @click="apply"><template #icon><Send :size="16" /></template>提交申请</NButton></template>
  </ModalShell>
</template>

<script setup lang="ts">
import { NButton, NProgress, NTag } from 'naive-ui'
import { computed, ref } from 'vue'
import { CalendarRange, Compass, Crown, RotateCcw, Search, Send, ShieldCheck } from 'lucide-vue-next'
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
const message = ref('')
const applying = ref(false)
const availableCount = computed(() => props.plans.filter(item => item.available_slots > 0).length)
const filteredPlans = computed(() => { const needle = query.value.trim().toLowerCase(); return props.plans.filter(item => !needle || item.plan.name.toLowerCase().includes(needle) || item.plan.description.toLowerCase().includes(needle) || item.owner_username.toLowerCase().includes(needle)) })
function updateQuery(value: string) { query.value = value }
function updateMessage(value: string) { message.value = value }
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
function formatSubscriptionDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(new Date(value)) }
</script>
