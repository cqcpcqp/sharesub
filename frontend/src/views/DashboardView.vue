<template>
  <section class="view-content dashboard-content" aria-label="个人使用仪表盘" :aria-busy="loading">
    <header v-if="dashboard" class="dashboard-actions">
      <div>
        <h1>仪表盘</h1>
        <p>从年度使用节奏到最近 24 小时波动</p>
      </div>
      <NButton secondary size="small" :loading="refreshing" :disabled="refreshing" aria-label="刷新仪表盘数据" @click="emit('refresh')">
        <template #icon><RefreshCw :size="15" /></template>
        刷新数据
      </NButton>
    </header>

    <section v-if="dashboard" class="dashboard-metric-band" aria-label="使用概览">
      <dl class="dashboard-overview-ledger">
        <div>
          <dt>总 Token</dt>
          <dd>{{ formatTokens(dashboard.total_tokens.total_tokens) }}</dd>
          <small>累计使用</small>
        </div>
        <div>
          <dt>今日 Token</dt>
          <dd>{{ formatTokens(dashboard.today_tokens.total_tokens) }}</dd>
          <small>{{ dashboard.performance.requests_today }} 次请求</small>
        </div>
        <div>
          <dt>实时性能</dt>
          <dd>{{ dashboard.performance.requests_per_minute }} <small>RPM</small></dd>
          <small>{{ formatTokens(dashboard.performance.tokens_per_minute) }} TPM · {{ formatPercent(dashboard.performance.success_rate) }} 成功率</small>
        </div>
        <div>
          <dt>平均响应</dt>
          <dd>{{ formatDuration(dashboard.performance.average_duration_ms) }}</dd>
          <small>{{ formatDuration(dashboard.performance.average_ttft_ms) }} 首字响应 · {{ dashboard.performance.active_plans }} 个 Plan</small>
        </div>
      </dl>
    </section>

    <section v-if="dashboard" class="dashboard-annual-section" aria-labelledby="dashboard-annual-title">
      <header class="dashboard-section-header">
        <div>
          <h2 id="dashboard-annual-title">年度活跃</h2>
          <p>最近 365 天 · 按本地日期统计 · 颜色表示相对用量</p>
        </div>
      </header>

      <div class="dashboard-heatmap-stage">
        <TokenActivityHeatmap :usage="dashboard.daily_usage" />
      </div>

      <dl class="dashboard-annual-facts" aria-label="年度使用节奏">
        <div>
          <dt>活跃日</dt>
          <dd>{{ activeDays }}</dd>
          <small>最近 365 天</small>
        </div>
        <div>
          <dt>年度覆盖</dt>
          <dd>{{ formatPercent(annualCoverage) }}</dd>
          <small>有使用记录的日期</small>
        </div>
        <div>
          <dt>年度请求</dt>
          <dd>{{ annualRequestCount.toLocaleString('zh-CN') }}</dd>
          <small>最近 365 天</small>
        </div>
        <div>
          <dt>活跃日均量</dt>
          <dd>{{ formatTokens(averageTokensPerActiveDay) }}</dd>
          <small>仅统计活跃日期</small>
        </div>
        <div class="dashboard-peak-fact">
          <dt>峰值日</dt>
          <dd><time :datetime="peakDay.usage_date">{{ formatUTCShortDate(peakDay.usage_date) }}</time></dd>
          <small>{{ formatTokens(peakDay.token_usage.total_tokens) }} Token</small>
        </div>
      </dl>
    </section>

    <section v-if="dashboard" class="dashboard-trend-section" aria-labelledby="dashboard-trend-title">
      <header class="dashboard-section-header dashboard-trend-header">
        <div>
          <h2 id="dashboard-trend-title">24 小时趋势</h2>
          <p>按本地时间展示，用量变化与工具调用同时对照</p>
        </div>
        <dl class="dashboard-trend-facts" aria-label="最近 24 小时摘要">
          <div>
            <dt>Token</dt>
            <dd>{{ formatTokens(trendTotal) }}</dd>
          </div>
          <div>
            <dt>Web Search</dt>
            <dd>{{ trendWebSearchCalls.toLocaleString('zh-CN') }}</dd>
          </div>
          <div>
            <dt>图片</dt>
            <dd>{{ trendImageCount.toLocaleString('zh-CN') }}</dd>
          </div>
        </dl>
      </header>
      <div v-if="trendTotal > 0" class="dashboard-chart-wrap">
        <TokenUsageChart :trend="dashboard.trend" :theme="theme" />
      </div>
      <NEmpty v-else class="dashboard-chart-empty" description="最近 24 小时还没有 Token 使用记录" />
    </section>

    <div v-else-if="loading" class="dashboard-loading" aria-live="polite">
      <NSkeleton height="190px" :sharp="true" />
      <NSkeleton height="330px" :sharp="true" />
      <NSkeleton height="390px" :sharp="true" />
    </div>
    <NEmpty v-else description="仪表盘暂时不可用" />
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NEmpty, NSkeleton } from 'naive-ui'
import { RefreshCw } from 'lucide-vue-next'
import TokenActivityHeatmap from '../components/TokenActivityHeatmap.vue'
import TokenUsageChart from '../components/TokenUsageChart.vue'
import { formatDuration, formatPercent, formatTokens } from '../dashboardFormat'
import type { Dashboard } from '../types'
import type { ResolvedTheme } from '../themePreference'

const props = defineProps<{
  dashboard: Dashboard | null
  loading: boolean
  refreshing: boolean
  theme: ResolvedTheme
}>()

const emit = defineEmits<{ refresh: [] }>()

const activeDays = computed(() => props.dashboard?.daily_usage.filter(day => day.token_usage.total_tokens > 0).length ?? 0)
const annualCoverage = computed(() => props.dashboard === null ? 0 : activeDays.value / props.dashboard.daily_usage.length * 100)
const annualRequestCount = computed(() => props.dashboard?.daily_usage.reduce((total, day) => total + day.request_count, 0) ?? 0)
const annualTokens = computed(() => props.dashboard?.daily_usage.reduce((total, day) => total + day.token_usage.total_tokens, 0) ?? 0)
const averageTokensPerActiveDay = computed(() => activeDays.value === 0 ? 0 : annualTokens.value / activeDays.value)
const peakDay = computed(() => props.dashboard!.daily_usage.reduce((peak, day) => day.token_usage.total_tokens > peak.token_usage.total_tokens ? day : peak))
const trendTotal = computed(() => props.dashboard?.trend.reduce((total, point) => total + point.input_tokens + point.output_tokens, 0) ?? 0)
const trendWebSearchCalls = computed(() => props.dashboard?.trend.reduce((total, point) => total + point.web_search_calls, 0) ?? 0)
const trendImageCount = computed(() => props.dashboard?.trend.reduce((total, point) => total + point.image_count, 0) ?? 0)

function formatUTCShortDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', month: 'short', day: 'numeric' }).format(new Date(`${value}T00:00:00Z`))
}
</script>
