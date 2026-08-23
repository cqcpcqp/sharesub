<template>
  <section v-if="vote" class="vote-panel" :class="`vote-${vote.status}`" aria-live="polite">
    <div class="vote-heading">
      <div>
        <strong>{{ statusTitle }}</strong>
        <span>{{ vote.initiator_username }} 发起</span>
      </div>
      <span v-if="vote.status === 'active'" class="vote-time"><Clock3 :size="12" />{{ remainingLabel }}</span>
    </div>

    <template v-if="vote.status === 'active' || vote.status === 'executing'">
      <div class="vote-progress-copy">
        <span>{{ progressLabel }}</span>
        <strong>{{ supportLabel }}</strong>
      </div>
      <div
        class="vote-progress"
        role="progressbar"
        :aria-label="progressLabel"
        :aria-valuenow="progressPercent"
        aria-valuemin="0"
        aria-valuemax="100"
      >
        <i :style="{ width: `${progressPercent}%` }" />
        <b title="必须严格超过 50%" />
      </div>

      <div class="vote-members">
        <span v-for="member in vote.members" :key="member.member_id" :class="`choice-${member.choice || 'pending'}`" :title="memberLabel(member)">
          <UserAvatar :size="22" :username="member.username" :src="member.avatar_url" />
          <small>{{ member.username }}</small>
          <Check v-if="member.choice === 'support'" :size="12" />
          <X v-else-if="member.choice === 'oppose'" :size="12" />
        </span>
      </div>

      <div v-if="vote.can_vote && vote.status === 'active'" class="vote-actions">
        <NButton size="small" type="primary" :secondary="vote.current_user_choice === 'support'" :loading="action === 'support'" :disabled="busy" @click="emit('cast', 'support')">
          <template #icon><ThumbsUp :size="14" /></template>
          {{ vote.current_user_choice === 'support' ? '已支持' : '支持重置' }}
        </NButton>
        <NButton size="small" secondary :loading="action === 'oppose'" :disabled="busy" @click="emit('cast', 'oppose')">
          <template #icon><ThumbsDown :size="14" /></template>
          {{ vote.current_user_choice === 'oppose' ? '已反对' : '反对' }}
        </NButton>
      </div>
      <p v-else-if="vote.status === 'executing'" class="vote-executing"><NSpin :size="14" />投票已经通过，正在自动重置额度，请勿重复操作。</p>
    </template>

    <p v-else class="vote-result">{{ resultDescription }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, NSpin } from 'naive-ui'
import { Check, Clock3, ThumbsDown, ThumbsUp, X } from 'lucide-vue-next'
import type { QuotaResetVote, QuotaResetVoteChoice, QuotaResetVoteMember } from '../types'
import UserAvatar from './UserAvatar.vue'

const props = defineProps<{ vote: QuotaResetVote | null; loading: boolean; action: string }>()
const emit = defineEmits<{ cast: [choice: Exclude<QuotaResetVoteChoice, ''>]; refresh: [] }>()
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
let refreshTicks = 0

const busy = computed(() => props.loading || props.action !== '')
const progressPercent = computed(() => props.vote?.allocation_mode === 'fixed'
  ? Math.min(100, props.vote.support_weight_basis_points / 100)
  : Math.min(100, props.vote ? props.vote.support_count / props.vote.eligible_count * 100 : 0))
const supportLabel = computed(() => props.vote?.allocation_mode === 'fixed'
  ? `${(props.vote.support_weight_basis_points / 100).toFixed(2).replace(/\.00$/, '')}%`
  : `${props.vote?.support_count ?? 0} / ${props.vote?.eligible_count ?? 0} 票`)
const progressLabel = computed(() => props.vote?.allocation_mode === 'fixed'
  ? '赞成份额必须严格超过整个 Plan 的 50%'
  : `需要至少 ${Math.floor((props.vote?.eligible_count ?? 0) / 2) + 1} 票赞成`)
const remainingLabel = computed(() => {
  if (!props.vote) return ''
  const seconds = Math.max(0, Math.ceil((new Date(props.vote.expires_at).getTime() - now.value) / 1000))
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor(seconds % 3600 / 60)
  if (hours > 0) return `剩余 ${hours} 小时 ${minutes} 分`
  if (minutes > 0) return `剩余 ${minutes} 分钟`
  return `剩余 ${seconds} 秒`
})
const statusTitle = computed(() => ({
  active: '额度重置投票进行中', executing: '投票已通过', succeeded: '额度重置已完成',
  succeeded_unsynced: '重置完成，额度待同步', expired: '投票已过期', cancelled: '投票已取消', outcome_unknown: '重置结果待确认',
}[props.vote?.status ?? 'active']))
const resultDescription = computed(() => {
  if (!props.vote) return ''
  if (props.vote.status === 'succeeded') return `系统已自动使用 1 次重置机会，重置了 ${props.vote.windows_reset} 个额度窗口。`
  if (props.vote.status === 'succeeded_unsynced') return 'OpenAI 已完成重置，但最新额度暂未同步；请稍后查询额度，不要重复重置。'
  if (props.vote.status === 'outcome_unknown') return '无法确认 OpenAI 是否已经消费重置机会；请先查询剩余次数，不要重复操作。'
  if (props.vote.status === 'expired') return '两小时内未达到严格多数，本次没有消耗重置机会。'
	if (props.vote.result_code === 'quota_reset_preflight_failed') return '系统未开始消费重置机会，请刷新 Plan 后重新发起投票。'
  return 'Plan 状态或成员配置发生变化，本次投票不再有效。'
})

function memberLabel(member: QuotaResetVoteMember) {
  const choice = member.choice === 'support' ? '支持' : member.choice === 'oppose' ? '反对' : '尚未投票'
  return props.vote?.allocation_mode === 'fixed' ? `${member.username} · ${member.weight_basis_points / 100}% · ${choice}` : `${member.username} · ${choice}`
}
function startTimer() {
  if (timer) clearInterval(timer)
  timer = setInterval(() => {
    now.value = Date.now()
    if (props.vote?.status !== 'active') return
    refreshTicks++
    if (refreshTicks % 15 === 0 || new Date(props.vote.expires_at).getTime() <= now.value) emit('refresh')
  }, 1000)
}
watch(() => props.vote?.id, () => { refreshTicks = 0; now.value = Date.now() })
onMounted(startTimer)
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.vote-panel { margin-top: 10px; padding: 11px; border: 1px solid var(--line-soft); border-radius: 8px; background: var(--surface-soft); }
.vote-heading, .vote-heading > div, .vote-time, .vote-progress-copy, .vote-actions, .vote-executing { display: flex; align-items: center; }
.vote-heading { justify-content: space-between; gap: 10px; }
.vote-heading > div { min-width: 0; gap: 6px; }
.vote-heading strong { color: var(--ink); font-size: 11px; }
.vote-heading span { color: var(--muted); font-size: 10px; }
.vote-time { flex: 0 0 auto; gap: 4px; color: var(--amber) !important; font-variant-numeric: tabular-nums; }
.vote-progress-copy { justify-content: space-between; gap: 10px; margin-top: 10px; color: var(--muted); font-size: 10px; }
.vote-progress-copy strong { flex: 0 0 auto; color: var(--ink); font-variant-numeric: tabular-nums; }
.vote-progress { position: relative; height: 7px; margin-top: 5px; overflow: hidden; border-radius: 999px; background: var(--control-rail); }
.vote-progress i { position: absolute; inset: 0 auto 0 0; border-radius: inherit; background: var(--primary); transition: width 260ms ease-out; }
.vote-progress b { position: absolute; z-index: 1; top: -2px; bottom: -2px; left: 50%; width: 1px; background: var(--ink); opacity: .62; }
.vote-members { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 10px; }
.vote-members > span { min-width: 0; display: inline-flex; align-items: center; gap: 4px; padding: 3px 6px 3px 3px; border-radius: 999px; background: var(--surface); color: var(--muted); }
.vote-members small { max-width: 82px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.vote-members .choice-support { color: var(--teal); background: var(--teal-soft); }
.vote-members .choice-oppose { color: var(--red); background: var(--red-soft); }
.vote-actions { gap: 7px; margin-top: 11px; }
.vote-actions :deep(.n-button) { flex: 1; }
.vote-executing, .vote-result { gap: 6px; margin: 10px 0 0; color: var(--muted); font-size: 10px; line-height: 1.55; }
.vote-succeeded, .vote-succeeded_unsynced { border-color: var(--teal-soft); }
.vote-outcome_unknown { border-color: var(--amber); background: var(--amber-soft); }
@media (max-width: 560px) { .vote-heading { align-items: flex-start; } .vote-heading > div { align-items: flex-start; flex-direction: column; gap: 1px; } }
</style>
