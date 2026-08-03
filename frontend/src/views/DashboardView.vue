<template>
  <section class="view-content dashboard-content" aria-label="个人使用仪表盘" :aria-busy="loading">
    <div v-if="dashboard" class="dashboard-kpi-grid">
      <article class="dashboard-kpi dashboard-kpi-today">
        <div class="dashboard-kpi-icon"><Zap :size="21" /></div>
        <div class="dashboard-kpi-body">
          <NStatistic label="今日 Token" :value="formatTokens(dashboard.today_tokens.total_tokens)" />
          <div class="token-breakdown">
            <span class="token-input">Input {{ formatTokens(dashboard.today_tokens.input_tokens) }}</span>
            <span class="token-output">Output {{ formatTokens(dashboard.today_tokens.output_tokens) }}</span>
            <span>Cached {{ formatTokens(dashboard.today_tokens.cached_tokens) }}</span>
          </div>
        </div>
      </article>

      <article class="dashboard-kpi dashboard-kpi-total">
        <div class="dashboard-kpi-icon"><Database :size="21" /></div>
        <div class="dashboard-kpi-body">
          <NStatistic label="总 Token" :value="formatTokens(dashboard.total_tokens.total_tokens)" />
          <div class="token-breakdown">
            <span class="token-input">Input {{ formatTokens(dashboard.total_tokens.input_tokens) }}</span>
            <span class="token-output">Output {{ formatTokens(dashboard.total_tokens.output_tokens) }}</span>
            <span>Cached {{ formatTokens(dashboard.total_tokens.cached_tokens) }}</span>
          </div>
        </div>
      </article>

      <article class="dashboard-kpi dashboard-kpi-performance">
        <div class="dashboard-kpi-icon"><Gauge :size="21" /></div>
        <div class="dashboard-kpi-body">
          <NStatistic label="性能指标" :value="dashboard.performance.requests_per_minute">
            <template #suffix><small>RPM</small></template>
          </NStatistic>
          <div class="dashboard-kpi-details">
            <span><strong>{{ formatTokens(dashboard.performance.tokens_per_minute) }}</strong> TPM</span>
            <span><strong>{{ formatPercent(dashboard.performance.success_rate) }}</strong> 成功率</span>
          </div>
        </div>
      </article>

      <article class="dashboard-kpi dashboard-kpi-response">
        <div class="dashboard-kpi-icon"><Clock3 :size="21" /></div>
        <div class="dashboard-kpi-body">
          <NStatistic label="平均响应" :value="formatDuration(dashboard.performance.average_duration_ms)" />
          <div class="dashboard-kpi-details">
            <span><strong>{{ formatDuration(dashboard.performance.average_ttft_ms) }}</strong> 首字响应</span>
            <span><strong>{{ dashboard.performance.requests_today }}</strong> 次请求 · <strong>{{ dashboard.performance.active_plans }}</strong> 个 Plan</span>
          </div>
        </div>
      </article>
    </div>

    <section v-if="dashboard" class="dashboard-trend-panel">
      <header class="dashboard-trend-header">
        <div>
          <span class="section-kicker">USAGE TREND</span>
          <h2>Token 使用趋势</h2>
          <p>最近 24 小时 · 按本地时间展示</p>
        </div>
        <div class="trend-summary">
          <small>24 小时合计</small>
          <strong>{{ formatTokens(trendTotal) }}</strong>
        </div>
      </header>
      <div class="dashboard-chart-wrap">
        <TokenUsageChart :trend="dashboard.trend" :theme="theme" />
      </div>
    </section>

    <div v-else-if="loading" class="dashboard-loading" aria-live="polite">
      <NSkeleton v-for="index in 4" :key="index" height="154px" :sharp="false" />
    </div>
    <NEmpty v-else description="仪表盘暂时不可用" />
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NEmpty, NStatistic, NSkeleton } from 'naive-ui'
import { Clock3, Database, Gauge, Zap } from 'lucide-vue-next'
import TokenUsageChart from '../components/TokenUsageChart.vue'
import { formatDuration, formatPercent, formatTokens } from '../dashboardFormat'
import type { Dashboard } from '../types'
import type { ResolvedTheme } from '../themePreference'

const props = defineProps<{
  dashboard: Dashboard | null
  loading: boolean
  theme: ResolvedTheme
}>()

const trendTotal = computed(() => props.dashboard?.trend.reduce((total, point) => total + point.input_tokens + point.output_tokens, 0) ?? 0)
</script>
