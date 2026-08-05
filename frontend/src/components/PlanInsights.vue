<template>
  <section class="insights">
    <header class="insights-heading">
      <div>
        <h3>额度与性能</h3>
        <p>账号窗口、成员用量与{{ performancePeriodLabel }}网关表现</p>
      </div>
      <NSelect
        class="performance-period-select"
        size="small"
        :value="performancePeriod"
        :options="performancePeriodOptions"
        :loading="performanceLoading"
        :consistent-menu-width="false"
        aria-label="性能统计时间段"
        @update:value="emit('update:performancePeriod', $event)"
      />
    </header>

    <div class="performance-grid">
      <article class="performance-card performance-coral">
        <span class="performance-icon"><Activity :size="17" /></span>
        <div><small>请求数</small><strong>{{ formatNumber(insights.performance.request_count) }}</strong><span>{{ performancePeriodLabel }}</span></div>
      </article>
      <article class="performance-card performance-green">
        <span class="performance-icon"><Gauge :size="17" /></span>
        <div><small>成功率</small><strong>{{ successRate }}</strong><span>2xx 响应</span></div>
      </article>
      <article class="performance-card performance-blue">
        <span class="performance-icon"><Zap :size="17" /></span>
        <div><small>平均 TTFT</small><strong>{{ formatMilliseconds(insights.performance.average_ttft_ms) }}</strong><span>P95 {{ formatMilliseconds(insights.performance.p95_ttft_ms) }}</span></div>
      </article>
      <article class="performance-card performance-amber">
        <span class="performance-icon"><Timer :size="17" /></span>
        <div><small>平均总耗时</small><strong>{{ formatMilliseconds(insights.performance.average_duration_ms) }}</strong><span>P95 {{ formatMilliseconds(insights.performance.p95_duration_ms) }}</span></div>
      </article>
    </div>

    <section class="analytics-grid" aria-label="最近 24 小时使用分析">
      <article class="analytics-panel model-distribution-panel">
        <header class="analytics-heading">
          <div><span class="section-label">{{ performancePeriodLabel }}</span><h4>模型分布</h4></div>
          <strong>{{ formatTokens(modelUsageTotal) }} Token</strong>
        </header>
        <div v-if="insights.model_usage.length" class="model-distribution-body">
          <div class="model-chart-wrap"><ModelDistributionChart :usage="insights.model_usage" :theme="theme" /></div>
          <div class="model-table-scroll">
            <table class="model-table">
              <thead><tr><th>模型</th><th>请求</th><th>Token</th><th>账号费用</th></tr></thead>
              <tbody>
                <tr v-for="(item, index) in insights.model_usage" :key="item.model">
                  <td><span class="model-color" :style="{ backgroundColor: modelColors[index % modelColors.length] }" />{{ item.model }}</td>
                  <td>{{ formatNumber(item.request_count) }}</td>
                  <td>{{ formatTokens(item.token_usage.total_tokens) }}</td>
                  <td class="model-cost">{{ formatUSD(item.estimated_cost_micros) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <p v-else class="analytics-empty">{{ performancePeriodLabel }}还没有模型使用记录。</p>
      </article>

      <article class="analytics-panel token-trend-panel">
        <header class="analytics-heading">
          <div><span class="section-label">{{ performancePeriodLabel }}</span><h4>Token 使用趋势</h4></div>
          <strong>{{ formatTokens(tokenTrendTotal) }} Token</strong>
        </header>
        <div v-if="tokenTrendTotal > 0" class="analytics-chart-wrap"><TokenUsageChart :trend="insights.token_trend" :theme="theme" /></div>
        <p v-else class="analytics-empty">{{ performancePeriodLabel }}还没有 Token 使用记录。</p>
      </article>
    </section>

    <section class="analytics-panel recent-usage-panel">
      <header class="analytics-heading">
        <div><span class="section-label">{{ performancePeriodLabel }}</span><h4>最近使用</h4></div>
        <strong>{{ insights.recent_usage.length }} 位成员</strong>
      </header>
      <div v-if="insights.recent_usage.length" class="recent-chart-wrap"><MemberUsageChart :usage="insights.recent_usage" :theme="theme" /></div>
      <p v-else class="analytics-empty">{{ performancePeriodLabel }}还没有成员使用记录。</p>
    </section>

    <section class="quota-section">
      <header class="panel-heading quota-heading">
        <div>
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
            <div class="token-total"><dt>Total Token</dt><dd>{{ item.usage ? formatTokens(item.usage.token_usage.total_tokens) : '--' }}</dd></div>
            <div><dt>Input</dt><dd>{{ item.usage ? formatTokens(item.usage.token_usage.input_tokens) : '--' }}</dd></div>
            <div><dt>Output</dt><dd>{{ item.usage ? formatTokens(item.usage.token_usage.output_tokens) : '--' }}</dd></div>
            <div><dt>Cached</dt><dd>{{ item.usage ? formatTokens(item.usage.token_usage.cached_tokens) : '--' }}</dd></div>
          </dl>
        </article>
      </div>
    </section>

    <div class="usage-columns">
      <section class="data-panel member-panel">
        <header class="panel-heading">
          <div>
            <div class="panel-title-row">
              <h4>{{ allocationMode === 'shared' ? '成员当前用量' : '成员当前额度' }}</h4>
              <NTooltip placement="top" trigger="hover">
                <template #trigger>
                  <button type="button" class="metric-help" aria-label="查看成员当前用量口径">
                    <CircleHelp :size="14" />
                  </button>
                </template>
                <span class="metric-help-copy">按当前 5h/7d 窗口内账号已用百分比相对上次观测的正向增量累计，并归因到触发该次响应的成员；不是按 Token 数计算。手动额度查询只更新账号快照，不计入成员用量。</span>
              </NTooltip>
            </div>
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
            <span class="section-label">{{ rankingPeriodRange }}</span>
            <h4>成员用量排行</h4>
          </div>
          <NSelect v-model:value="rankingPeriodID" class="ranking-period-select" size="small" :options="rankingPeriodOptions" :consistent-menu-width="false" />
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
                  <strong class="ranking-total">{{ formatTokens(member.token_usage.total_tokens) }}</strong>
                  <small class="ranking-breakdown">I {{ formatTokens(member.token_usage.input_tokens) }} · O {{ formatTokens(member.token_usage.output_tokens) }} · C {{ formatTokens(member.token_usage.cached_tokens) }}</small>
                </td>
                <td><strong class="ranking-cost">{{ formatUSD(member.estimated_cost_micros) }}</strong></td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="empty-copy">{{ rankingPeriodLabel }}还没有成员请求。</p>
      </section>
    </div>

  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NProgress, NSelect, NTooltip } from 'naive-ui'
import { Activity, CircleHelp, Clock3, Gauge, RefreshCw, Timer, Zap } from 'lucide-vue-next'
import type { Member, MemberRankingPeriodID, PerformancePeriod, PlanAllocationMode, PlanInsights, QuotaWindow, WindowUsage } from '../types'
import type { ResolvedTheme } from '../themePreference'
import { formatShareBasisPoints } from '../planAllocation'
import { formatMilliseconds, formatTokens } from '../dashboardFormat'
import UserAvatar from './UserAvatar.vue'
import MemberUsageChart from './MemberUsageChart.vue'
import ModelDistributionChart from './ModelDistributionChart.vue'
import TokenUsageChart from './TokenUsageChart.vue'

const props = withDefaults(defineProps<{
  insights: PlanInsights
  members: Member[]
  allocationMode: PlanAllocationMode
  canRefresh?: boolean
  refreshing?: boolean
  performancePeriod?: PerformancePeriod
  performanceLoading?: boolean
  theme: ResolvedTheme
}>(), {
  canRefresh: false,
  refreshing: false,
  performancePeriod: '24h',
  performanceLoading: false,
})

const emit = defineEmits<{ refresh: []; 'update:performancePeriod': [value: PerformancePeriod] }>()
const performancePeriodLabels: Record<PerformancePeriod, string> = {
  today: '本日',
  '30m': '最近 30 分钟',
  '6h': '最近 6 小时',
  '12h': '最近 12 小时',
  '24h': '最近 24 小时',
}
const performancePeriodOptions = Object.entries(performancePeriodLabels).map(([value, label]) => ({ value, label }))
const performancePeriodLabel = computed(() => performancePeriodLabels[props.performancePeriod])
const rankingPeriodID = ref<MemberRankingPeriodID>('today')
const rankingPeriodLabels: Record<MemberRankingPeriodID, string> = {
  today: '本日',
  last_7_days: '最近 7 天',
  account_7d: '当前账号 7d 周期',
  account_lifecycle: '账号生命周期（接入以来）',
}
const rankingPeriodOptions = computed(() => props.insights.member_rankings.map(period => ({ label: rankingPeriodLabels[period.period], value: period.period })))
const rankingPeriod = computed(() => props.insights.member_rankings.find(period => period.period === rankingPeriodID.value)!)
const rankingPeriodLabel = computed(() => rankingPeriodLabels[rankingPeriodID.value])
const memberRanking = computed(() => rankingPeriod.value.members)
const rankingPeriodRange = computed(() => `${formatDate(rankingPeriod.value.window_start)} – ${formatDate(rankingPeriod.value.window_end)}`)
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
const modelColors = ['#4b7bec', '#18a27f', '#d59020', '#8b5cf6', '#e45b78', '#18a6b8', '#eb7f43', '#6f7a8a', '#57b86d', '#a66dd4', '#e3b341', '#3d93d8']
const modelUsageTotal = computed(() => props.insights.model_usage.reduce((total, item) => total + item.token_usage.total_tokens, 0))
const tokenTrendTotal = computed(() => props.insights.token_trend.reduce((total, point) => total + point.input_tokens + point.output_tokens, 0))

const numberFormatter = new Intl.NumberFormat('zh-CN')
const percentFormatter = new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 1, maximumFractionDigits: 2 })
const memberPercentFormatter = new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
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
  return window ? `${memberPercentFormatter.format(window.used_micros / 1_000_000)}%` : '--'
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
.insights-heading { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
.insights-heading > div { min-width: 0; }
.performance-period-select { width: 132px; flex: 0 0 auto; }

.analytics-grid { min-width: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.analytics-panel { min-width: 0; overflow: hidden; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-xs); }
.analytics-heading { min-height: 64px; display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 13px 16px; border-bottom: 1px solid var(--line-soft); }
.analytics-heading h4 { margin: 3px 0 0; color: var(--ink-strong); font-size: 13px; }
.analytics-heading > strong { color: var(--ink); font-size: 11px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.model-distribution-body { min-width: 0; height: 258px; display: grid; grid-template-columns: 180px minmax(0, 1fr); align-items: center; }
.model-chart-wrap { height: 180px; padding: 8px; }
.model-table-scroll { min-width: 0; max-height: 238px; overflow: auto; padding-right: 8px; }
.model-table { min-width: 390px; }
.model-table th { height: 30px; padding: 0 8px; }
.model-table td { height: 36px; padding: 6px 8px; }
.model-table td:first-child { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--ink-strong); font-weight: 700; }
.model-color { width: 7px; height: 7px; display: inline-block; margin-right: 7px; border-radius: 50%; }
.model-cost { color: var(--teal); font-weight: 700; }
.analytics-chart-wrap { height: 258px; padding: 10px 14px 14px; }
.recent-chart-wrap { height: 300px; padding: 10px 14px 14px; }
.analytics-empty { height: 180px; display: grid; place-items: center; margin: 0; color: var(--muted); font-size: 11px; }

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
.performance-card small { color: var(--muted); font-size: 11px; font-weight: 760; }
.performance-card strong { overflow-wrap: anywhere; color: var(--ink-strong); font-size: 21px; font-variant-numeric: tabular-nums; line-height: 1.2; }
.performance-card span:last-child { color: var(--muted-light); font-size: 11px; }

.quota-section { min-width: 0; padding-top: 2px; }
.panel-heading { min-height: 38px; display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; }
.panel-heading > div { min-width: 0; }
.panel-heading h4 { margin-top: 4px; font-size: 13px; }
.panel-heading > svg { color: var(--muted-light); }
.panel-title-row { min-width: 0; display: flex; align-items: center; gap: 5px; }
.metric-help { width: 22px; height: 22px; display: inline-grid; place-items: center; flex: 0 0 auto; margin-top: 4px; padding: 0; border: 0; border-radius: 999px; background: transparent; color: var(--muted-light); cursor: help; }
.metric-help:hover, .metric-help:focus-visible { background: var(--surface-hover); color: var(--ink); outline: none; }
.metric-help-copy { display: block; max-width: 340px; line-height: 1.55; }
.ranking-period-select { width: 150px; }
.section-label { display: block; color: var(--muted-light); font-size: 11px; font-weight: 800; letter-spacing: 0; }
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
.quota-card-heading > small { overflow: hidden; color: var(--muted-light); font-size: 11px; text-align: right; text-overflow: ellipsis; white-space: nowrap; }
.window-badge { min-width: 29px; height: 23px; display: inline-grid; place-items: center; border-radius: 6px; background: var(--window-soft); color: var(--window-accent); font-size: 11px; font-weight: 800; }
.quota-level { display: flex; align-items: baseline; gap: 7px; margin: 15px 0 9px; }
.quota-level strong { color: var(--ink-strong); font-size: 29px; font-variant-numeric: tabular-nums; line-height: 1; }
.quota-level span { color: var(--muted); font-size: 11px; }
.reset-row { display: grid; grid-template-columns: 14px auto minmax(0, 1fr); align-items: center; gap: 6px; margin-top: 10px; color: var(--muted-light); font-size: 11px; }
.reset-row strong { overflow: hidden; color: var(--ink); font-size: 11px; font-variant-numeric: tabular-nums; text-align: right; text-overflow: ellipsis; white-space: nowrap; }

.window-summary,
.token-grid { margin: 14px 0 0; }
.window-summary { display: grid; grid-template-columns: .7fr 1.3fr; border-top: 1px solid var(--line-soft); border-bottom: 1px solid var(--line-soft); }
.window-summary > div { min-width: 0; padding: 12px 0; }
.window-summary > div + div { padding-left: 14px; border-left: 1px solid var(--line-soft); }
.window-summary dt,
.token-grid dt { color: var(--muted-light); font-size: 11px; font-weight: 760; }
.window-summary dd,
.token-grid dd { overflow-wrap: anywhere; margin: 4px 0 0; color: var(--ink-strong); font-size: 13px; font-weight: 720; font-variant-numeric: tabular-nums; }
.cost-summary dd { color: var(--window-accent); }
.token-grid { display: grid; grid-template-columns: minmax(110px, 1.35fr) repeat(3, minmax(0, .8fr)); align-items: stretch; gap: 8px; }
.token-grid > div { min-width: 0; padding: 8px 0; }
.token-grid dd { font-size: 11px; }
.token-grid .token-total {
  display: grid;
  align-content: center;
  padding: 9px 11px;
  border: 1px solid var(--window-accent);
  border-radius: 7px;
  background: var(--window-soft);
}
.token-grid .token-total dt { color: var(--window-accent); font-size: 11px; font-weight: 820; }
.token-grid .token-total dd { margin-top: 5px; color: var(--window-accent); font-size: 17px; font-weight: 820; line-height: 1.1; }

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
.member-identity small { margin-top: 3px; color: var(--muted-light); font-size: 11px; }
.member-windows { flex: 0 0 auto; display: grid; grid-template-columns: repeat(2, minmax(48px, 1fr)); gap: 8px; }
.member-windows > span { display: grid; gap: 2px; text-align: right; }
.member-windows small { color: var(--muted-light); font-size: 11px; font-weight: 760; }
.member-windows strong { color: var(--ink); font-size: 11px; font-variant-numeric: tabular-nums; }

.table-scroll { min-width: 0; overflow-x: auto; border-top: 1px solid var(--line-soft); }
table { width: 100%; border-collapse: collapse; }
th { height: 36px; padding: 0 12px; background: var(--surface-soft); color: var(--muted); font-size: 11px; font-weight: 800; text-align: left; white-space: nowrap; }
td { height: 54px; padding: 9px 12px; border-top: 1px solid var(--line-soft); color: var(--ink); font-size: 11px; font-variant-numeric: tabular-nums; }
tbody tr:hover { background: var(--surface-hover); }
.ranking-table { min-width: 600px; }
.ranking-table th:first-child,
.ranking-table td:first-child { width: 48px; text-align: center; }
.rank-index { width: 23px; height: 23px; display: inline-grid; place-items: center; border-radius: 6px; background: var(--surface-soft); color: var(--muted); font-size: 11px; font-weight: 800; }
.ranking-table tbody tr:first-child .rank-index { background: var(--amber-soft); color: var(--amber); }
.ranking-name,
.ranking-total,
.ranking-cost { color: var(--ink-strong); font-size: 11px; }
.ranking-breakdown { display: block; margin-top: 3px; color: var(--muted-light); font-size: 11px; white-space: nowrap; }
.ranking-cost { color: var(--teal); }
.empty-copy { margin: 0; padding: 22px 16px; border-top: 1px solid var(--line-soft); color: var(--muted); font-size: 11px; }

@media (max-width: 1120px) {
  .performance-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .analytics-grid { grid-template-columns: 1fr; }
  .usage-columns { grid-template-columns: 1fr; }
}

@media (max-width: 720px) {
  .quota-grid { grid-template-columns: 1fr; }
  .quota-card-heading > small { max-width: 48%; }
  .model-distribution-body { height: auto; grid-template-columns: 1fr; }
  .model-chart-wrap { height: 200px; }
  .model-table-scroll { max-height: 220px; padding: 0 10px 10px; }
}

@media (max-width: 520px) {
  .insights { gap: 15px; }
  .insights-heading { align-items: stretch; flex-direction: column; }
  .performance-period-select { width: 100%; }
  .performance-grid { gap: 8px; }
  .analytics-heading { align-items: flex-start; flex-direction: column; gap: 5px; }
  .analytics-chart-wrap { height: 235px; padding-inline: 7px; }
  .recent-chart-wrap { height: 270px; padding-inline: 7px; }
  .performance-card { min-height: 108px; grid-template-columns: 30px minmax(0, 1fr); gap: 9px; padding: 12px 10px; }
  .performance-icon { width: 30px; height: 30px; }
  .performance-card strong { font-size: 18px; }
  .quota-card { padding: 15px 13px; }
  .quota-card-heading { align-items: flex-start; flex-direction: column; gap: 7px; }
  .quota-card-heading > small { max-width: 100%; text-align: left; }
  .quota-level strong { font-size: 25px; }
  .window-summary { grid-template-columns: .65fr 1.35fr; }
  .window-summary > div + div { padding-left: 10px; }
  .token-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .token-grid .token-total { grid-column: 1 / -1; grid-template-columns: 1fr auto; align-items: end; }
  .token-grid .token-total dd { margin-top: 0; text-align: right; }
  .member-row { align-items: flex-start; flex-direction: column; padding: 11px 0; }
  .member-windows { width: 100%; }
  .member-windows > span { text-align: left; }
  .ranking-panel > .panel-heading { align-items: stretch; flex-direction: column; }
  .ranking-period-select { width: 100%; }
}

@media (max-width: 380px) {
  .performance-grid { grid-template-columns: 1fr; }
  .quota-heading { align-items: flex-start; }
  .quota-heading :deep(.n-button) { width: 34px; min-width: 34px; padding: 0; }
  .quota-heading :deep(.n-button__content) { font-size: 0; }
}
</style>
