<template>
  <section class="insights">
    <header class="insights-heading">
      <div>
        <h3>额度与性能</h3>
        <p>账号窗口、成员用量与最近 24 小时网关表现</p>
      </div>
    </header>

    <div class="performance-grid">
      <article class="performance-card performance-coral">
        <span class="performance-icon"><Activity :size="17" /></span>
        <div><small>请求数</small><strong>{{ formatNumber(insights.performance.request_count) }}</strong><span>最近 24 小时</span></div>
      </article>
      <article class="performance-card performance-green">
        <span class="performance-icon"><Gauge :size="17" /></span>
        <div><small>成功率</small><strong>{{ successRate }}</strong><span>2xx 响应</span></div>
      </article>
      <article class="performance-card performance-blue">
        <span class="performance-icon"><Zap :size="17" /></span>
        <div><small>平均 TTFT</small><strong>{{ duration(insights.performance.average_ttft_ms) }}</strong><span>P95 {{ duration(insights.performance.p95_ttft_ms) }}</span></div>
      </article>
      <article class="performance-card performance-amber">
        <span class="performance-icon"><Timer :size="17" /></span>
        <div><small>平均总耗时</small><strong>{{ duration(insights.performance.average_duration_ms) }}</strong><span>P95 {{ duration(insights.performance.p95_duration_ms) }}</span></div>
      </article>
    </div>

    <section class="quota-section">
      <header class="panel-heading quota-heading">
        <div>
          <span class="section-label">ACCOUNT QUOTA</span>
          <h4>账号额度与窗口用量</h4>
        </div>
        <NButton
          v-if="canRefresh"
          secondary
          size="small"
          :loading="refreshing"
          :disabled="refreshing"
          aria-label="查询账号额度"
          @click="emit('refresh')"
        >
          <template #icon><RefreshCw :size="15" /></template>
          查询额度
        </NButton>
      </header>

      <div class="quota-grid">
        <article v-for="item in quotaCards" :key="item.type" class="quota-card" :class="`quota-${item.type}`">
          <header class="quota-card-heading">
            <div>
              <span class="window-badge">{{ item.type }}</span>
              <h5>{{ item.label }}窗口</h5>
            </div>
            <small>{{ usagePeriod(item.usage) }}</small>
          </header>

          <div class="quota-level">
            <strong>{{ item.quota ? percent(item.quota.account_used_micros) : '--' }}</strong>
            <span>已用</span>
          </div>
          <NProgress
            type="line"
            :percentage="item.quota ? clampPercent(item.quota.account_used_micros) : 0"
            :show-indicator="false"
            :height="7"
            :color="item.type === '5h' ? 'var(--teal)' : 'var(--blue)'"
            rail-color="var(--line-soft)"
          />
          <div class="reset-row">
            <Clock3 :size="14" />
            <span>下次重置</span>
            <strong>{{ item.quota ? formatDate(item.quota.reset_at) : '--' }}</strong>
          </div>

          <dl class="window-summary">
            <div>
              <dt>请求数</dt>
              <dd>{{ item.usage ? formatNumber(item.usage.request_count) : '--' }}</dd>
            </div>
            <div class="cost-summary">
              <dt>账号计费</dt>
              <dd>{{ item.usage ? formatUSD(item.usage.estimated_cost_micros) : '--' }}</dd>
            </div>
          </dl>
          <dl class="token-grid">
            <div><dt>Input</dt><dd>{{ item.usage ? formatNumber(item.usage.token_usage.input_tokens) : '--' }}</dd></div>
            <div><dt>Output</dt><dd>{{ item.usage ? formatNumber(item.usage.token_usage.output_tokens) : '--' }}</dd></div>
            <div><dt>Cached</dt><dd>{{ item.usage ? formatNumber(item.usage.token_usage.cached_tokens) : '--' }}</dd></div>
            <div class="token-total"><dt>Total Token</dt><dd>{{ item.usage ? formatNumber(item.usage.token_usage.total_tokens) : '--' }}</dd></div>
          </dl>
        </article>
      </div>
    </section>

    <div class="usage-columns">
      <section class="data-panel member-panel">
        <header class="panel-heading">
          <div>
            <span class="section-label">CURRENT WINDOWS</span>
            <h4>{{ allocationMode === 'shared' ? '成员当前用量' : '成员当前额度' }}</h4>
          </div>
        </header>
        <div class="member-list">
          <div v-for="member in members" :key="member.id" class="member-row">
            <div class="member-identity">
              <UserAvatar :size="32" :username="member.username" :src="member.avatar_url" />
              <div>
                <strong>{{ member.username }}</strong>
                <small>{{ allocationMode === 'shared' ? '共享额度' : `份额 ${formatShareBasisPoints(member.share_basis_points)}` }}</small>
              </div>
            </div>
            <div class="member-windows">
              <span><small>5h</small><strong>{{ memberUsed(member.id, '5h') }}</strong></span>
              <span><small>7d</small><strong>{{ memberUsed(member.id, '7d') }}</strong></span>
            </div>
          </div>
        </div>
      </section>

      <section class="data-panel ranking-panel">
        <header class="panel-heading">
          <div>
            <span class="section-label">LAST 7 DAYS</span>
            <h4>成员用量排行</h4>
          </div>
          <UsersRound :size="18" />
        </header>
        <div v-if="memberRanking.length" class="table-scroll">
          <table class="ranking-table">
            <thead><tr><th>排名</th><th>成员</th><th>请求</th><th>Token</th><th>账号费用</th></tr></thead>
            <tbody>
              <tr v-for="(member, index) in memberRanking" :key="member.member_id">
                <td><span class="rank-index">{{ index + 1 }}</span></td>
                <td><strong class="ranking-name">{{ member.username }}</strong></td>
                <td>{{ formatNumber(member.request_count) }}</td>
                <td>
                  <strong class="ranking-total">{{ formatNumber(member.token_usage.total_tokens) }}</strong>
                  <small class="ranking-breakdown">I {{ formatNumber(member.token_usage.input_tokens) }} · O {{ formatNumber(member.token_usage.output_tokens) }} · C {{ formatNumber(member.token_usage.cached_tokens) }}</small>
                </td>
                <td><strong class="ranking-cost">{{ formatUSD(member.estimated_cost_micros) }}</strong></td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="empty-copy">最近 7 天还没有成员请求。</p>
      </section>
    </div>

  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NProgress } from 'naive-ui'
import { Activity, Clock3, Gauge, RefreshCw, Timer, UsersRound, Zap } from 'lucide-vue-next'
import type { Member, PlanAllocationMode, PlanInsights, QuotaWindow, WindowUsage } from '../types'
import { formatShareBasisPoints } from '../planAllocation'
import UserAvatar from './UserAvatar.vue'

const props = withDefaults(defineProps<{
  insights: PlanInsights
  members: Member[]
  allocationMode: PlanAllocationMode
  canRefresh?: boolean
  refreshing?: boolean
}>(), {
  canRefresh: false,
  refreshing: false,
})

const emit = defineEmits<{ refresh: [] }>()
const memberRanking = computed(() => props.insights.member_ranking)
const quotaKinds = [
  { type: '5h' as const, label: '5 小时' },
  { type: '7d' as const, label: '7 天' },
]
const quotaCards = computed(() => quotaKinds.map(item => ({
  ...item,
  quota: props.insights.account_windows.find(window => window.window_type === item.type),
  usage: props.insights.window_usage.find(window => window.window_type === item.type),
})))
const successRate = computed(() => props.insights.performance.request_count === 0
  ? '--'
  : `${((props.insights.performance.success_count / props.insights.performance.request_count) * 100).toFixed(1)}%`)

const numberFormatter = new Intl.NumberFormat('zh-CN')
const percentFormatter = new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 1, maximumFractionDigits: 2 })
const usdFormatter = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 4,
})
const dateFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: 'numeric',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

function duration(value: number) {
  return value >= 1000 ? `${(value / 1000).toFixed(2)} s` : `${Math.round(value)} ms`
}

function formatNumber(value: number) {
  return numberFormatter.format(value)
}

function percent(value: number) {
  return `${percentFormatter.format(value / 1_000_000)}%`
}

function clampPercent(value: number) {
  return Math.min(100, Math.max(0, value / 1_000_000))
}

function formatUSD(value: number) {
  return usdFormatter.format(value / 1_000_000)
}

function memberUsed(memberID: string, kind: QuotaWindow['window_type']) {
  const quota = props.insights.member_quotas.find(item => item.member_id === memberID)
  const window = quota?.windows.find(item => item.window_type === kind)
  return window ? percent(window.used_micros) : '--'
}

function formatDate(value: string) {
  return dateFormatter.format(new Date(value))
}

function usagePeriod(usage: WindowUsage | undefined) {
  if (!usage) return '等待窗口数据'
  return `${formatDate(usage.window_start)} - ${formatDate(usage.window_end)}`
}
</script>

<style scoped>
.insights {
  min-width: 0;
  display: grid;
  gap: 18px;
}

.insights-heading h3,
.panel-heading h4,
.quota-card h5 {
  margin: 0;
  color: var(--ink-strong);
  letter-spacing: 0;
}

.insights-heading h3 { font-size: 15px; }
.insights-heading p { margin: 6px 0 0; color: var(--muted); font-size: 11px; line-height: 1.5; }

.performance-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.performance-card {
  --performance-accent: var(--primary);
  --performance-soft: var(--primary-soft);
  min-width: 0;
  min-height: 116px;
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr);
  align-content: start;
  gap: 11px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: var(--shadow-xs);
}

.performance-green { --performance-accent: var(--teal); --performance-soft: var(--teal-soft); }
.performance-blue { --performance-accent: var(--blue); --performance-soft: var(--blue-soft); }
.performance-amber { --performance-accent: var(--amber); --performance-soft: var(--amber-soft); }
.performance-icon { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 7px; background: var(--performance-soft); color: var(--performance-accent); }
.performance-card > div { min-width: 0; display: grid; align-content: start; gap: 4px; }
.performance-card small { color: var(--muted); font-size: 9px; font-weight: 760; }
.performance-card strong { overflow-wrap: anywhere; color: var(--ink-strong); font-size: 21px; font-variant-numeric: tabular-nums; line-height: 1.2; }
.performance-card span:last-child { color: var(--muted-light); font-size: 9px; }

.quota-section { min-width: 0; padding-top: 2px; }
.panel-heading { min-height: 38px; display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; }
.panel-heading > div { min-width: 0; }
.panel-heading h4 { margin-top: 4px; font-size: 13px; }
.panel-heading > svg { color: var(--muted-light); }
.section-label { display: block; color: var(--muted-light); font-size: 8px; font-weight: 800; letter-spacing: 0; }
.quota-heading { align-items: center; margin-bottom: 11px; }
.quota-heading :deep(.n-button) { flex: 0 0 auto; }

.quota-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.quota-card {
  --window-accent: var(--teal);
  --window-soft: var(--teal-soft);
  min-width: 0;
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: var(--shadow-xs);
}
.quota-7d { --window-accent: var(--blue); --window-soft: var(--blue-soft); }
.quota-card-heading { min-width: 0; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.quota-card-heading > div { min-width: 0; display: flex; align-items: center; gap: 8px; }
.quota-card-heading h5 { font-size: 12px; }
.quota-card-heading > small { overflow: hidden; color: var(--muted-light); font-size: 8px; text-align: right; text-overflow: ellipsis; white-space: nowrap; }
.window-badge { min-width: 29px; height: 23px; display: inline-grid; place-items: center; border-radius: 6px; background: var(--window-soft); color: var(--window-accent); font-size: 9px; font-weight: 800; }
.quota-level { display: flex; align-items: baseline; gap: 7px; margin: 15px 0 9px; }
.quota-level strong { color: var(--ink-strong); font-size: 29px; font-variant-numeric: tabular-nums; line-height: 1; }
.quota-level span { color: var(--muted); font-size: 9px; }
.reset-row { display: grid; grid-template-columns: 14px auto minmax(0, 1fr); align-items: center; gap: 6px; margin-top: 10px; color: var(--muted-light); font-size: 9px; }
.reset-row strong { overflow: hidden; color: var(--ink); font-size: 10px; font-variant-numeric: tabular-nums; text-align: right; text-overflow: ellipsis; white-space: nowrap; }

.window-summary,
.token-grid { margin: 14px 0 0; }
.window-summary { display: grid; grid-template-columns: .7fr 1.3fr; border-top: 1px solid var(--line-soft); border-bottom: 1px solid var(--line-soft); }
.window-summary > div { min-width: 0; padding: 12px 0; }
.window-summary > div + div { padding-left: 14px; border-left: 1px solid var(--line-soft); }
.window-summary dt,
.token-grid dt { color: var(--muted-light); font-size: 8px; font-weight: 760; }
.window-summary dd,
.token-grid dd { overflow-wrap: anywhere; margin: 4px 0 0; color: var(--ink-strong); font-size: 13px; font-weight: 720; font-variant-numeric: tabular-nums; }
.cost-summary dd { color: var(--window-accent); }
.token-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
.token-grid > div { min-width: 0; }
.token-grid dd { font-size: 11px; }
.token-total { padding-left: 8px; border-left: 1px solid var(--line-soft); }

.usage-columns { min-width: 0; display: grid; grid-template-columns: minmax(260px, .7fr) minmax(0, 1.3fr); gap: 12px; }
.data-panel { min-width: 0; overflow: hidden; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-xs); }
.data-panel > .panel-heading { padding: 15px 16px 12px; }
.member-list { padding: 0 16px 5px; }
.member-row { min-width: 0; min-height: 58px; display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--line-soft); }
.member-identity { min-width: 0; display: flex; align-items: center; gap: 9px; }
.member-identity > div { min-width: 0; }
.member-identity strong,
.member-identity small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.member-identity strong { color: var(--ink-strong); font-size: 11px; }
.member-identity small { margin-top: 3px; color: var(--muted-light); font-size: 8px; }
.member-windows { flex: 0 0 auto; display: grid; grid-template-columns: repeat(2, minmax(48px, 1fr)); gap: 8px; }
.member-windows > span { display: grid; gap: 2px; text-align: right; }
.member-windows small { color: var(--muted-light); font-size: 8px; font-weight: 760; }
.member-windows strong { color: var(--ink); font-size: 10px; font-variant-numeric: tabular-nums; }

.table-scroll { min-width: 0; overflow-x: auto; border-top: 1px solid var(--line-soft); }
table { width: 100%; border-collapse: collapse; }
th { height: 36px; padding: 0 12px; background: var(--surface-soft); color: var(--muted); font-size: 8px; font-weight: 800; text-align: left; white-space: nowrap; }
td { height: 54px; padding: 9px 12px; border-top: 1px solid var(--line-soft); color: var(--ink); font-size: 10px; font-variant-numeric: tabular-nums; }
tbody tr:hover { background: var(--surface-hover); }
.ranking-table { min-width: 600px; }
.ranking-table th:first-child,
.ranking-table td:first-child { width: 48px; text-align: center; }
.rank-index { width: 23px; height: 23px; display: inline-grid; place-items: center; border-radius: 6px; background: var(--surface-soft); color: var(--muted); font-size: 9px; font-weight: 800; }
.ranking-table tbody tr:first-child .rank-index { background: var(--amber-soft); color: var(--amber); }
.ranking-name,
.ranking-total,
.ranking-cost { color: var(--ink-strong); font-size: 10px; }
.ranking-breakdown { display: block; margin-top: 3px; color: var(--muted-light); font-size: 8px; white-space: nowrap; }
.ranking-cost { color: var(--teal); }
.empty-copy { margin: 0; padding: 22px 16px; border-top: 1px solid var(--line-soft); color: var(--muted); font-size: 10px; }

@media (max-width: 1120px) {
  .performance-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .usage-columns { grid-template-columns: 1fr; }
}

@media (max-width: 720px) {
  .quota-grid { grid-template-columns: 1fr; }
  .quota-card-heading > small { max-width: 48%; }
}

@media (max-width: 520px) {
  .insights { gap: 15px; }
  .performance-grid { gap: 8px; }
  .performance-card { min-height: 108px; grid-template-columns: 30px minmax(0, 1fr); gap: 9px; padding: 12px 10px; }
  .performance-icon { width: 30px; height: 30px; }
  .performance-card strong { font-size: 18px; }
  .quota-card { padding: 15px 13px; }
  .quota-card-heading { align-items: flex-start; flex-direction: column; gap: 7px; }
  .quota-card-heading > small { max-width: 100%; text-align: left; }
  .quota-level strong { font-size: 25px; }
  .window-summary { grid-template-columns: .65fr 1.35fr; }
  .window-summary > div + div { padding-left: 10px; }
  .token-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 11px 8px; }
  .token-total { padding-left: 0; border-left: 0; }
  .member-row { align-items: flex-start; flex-direction: column; padding: 11px 0; }
  .member-windows { width: 100%; }
  .member-windows > span { text-align: left; }
}

@media (max-width: 380px) {
  .performance-grid { grid-template-columns: 1fr; }
  .quota-heading { align-items: flex-start; }
  .quota-heading :deep(.n-button) { width: 34px; min-width: 34px; padding: 0; }
  .quota-heading :deep(.n-button__content) { font-size: 0; }
}
</style>
