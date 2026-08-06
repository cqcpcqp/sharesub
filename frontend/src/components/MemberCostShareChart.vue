<template>
  <div class="member-cost-share-chart" role="img" :aria-label="ariaLabel">
    <Doughnut :data="chartData" :options="chartOptions" />
    <div class="chart-center" aria-hidden="true">
      <strong>{{ windowType }}</strong>
      <small>成本占比</small>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ArcElement, Chart as ChartJS, DoughnutController, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import type { ResolvedTheme } from '../themePreference'

ChartJS.register(DoughnutController, ArcElement, Tooltip)

interface MemberCostShare {
  label: string
  value: number
  color: string
}

const props = defineProps<{
  shares: MemberCostShare[]
  theme: ResolvedTheme
  windowType: '5h' | '7d'
  windowLabel: string
}>()

const percentFormatter = new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const textColor = computed(() => props.theme === 'dark' ? '#a5a19b' : '#6d7078')
const tooltipColors = computed(() => props.theme === 'dark'
  ? { background: '#292a2d', title: '#f7f5f2', border: 'rgba(165,161,155,.2)' }
  : { background: '#ffffff', title: '#222327', border: 'rgba(109,112,120,.2)' })
const surfaceColor = computed(() => props.theme === 'dark' ? '#202123' : '#ffffff')

const ariaLabel = computed(() => `${props.windowLabel}成员账号费用占比：${props.shares
  .filter(item => item.value > 0)
  .map(item => `${item.label} ${percentFormatter.format(item.value / 1_000_000)}%`)
  .join('，')}`)

const chartData = computed<ChartData<'doughnut'>>(() => ({
  labels: props.shares.map(item => item.label),
  datasets: [{
    data: props.shares.map(item => item.value),
    backgroundColor: props.shares.map(item => item.color),
    borderColor: surfaceColor.value,
    borderWidth: 2,
    hoverOffset: 3,
  }],
}))

const chartOptions = computed<ChartOptions<'doughnut'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  cutout: '67%',
  animation: { duration: 300 },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: tooltipColors.value.background,
      titleColor: tooltipColors.value.title,
      bodyColor: textColor.value,
      borderColor: tooltipColors.value.border,
      borderWidth: 1,
      padding: 11,
      callbacks: {
        label: context => `${context.label}: ${percentFormatter.format(Number(context.parsed) / 1_000_000)}%`,
      },
    },
  },
}))
</script>

<style scoped>
.member-cost-share-chart { position: relative; width: min(132px, 100%); aspect-ratio: 1; }
.member-cost-share-chart :deep(canvas) { position: absolute; inset: 0; width: 100% !important; height: 100% !important; }
.chart-center { position: absolute; inset: 27%; display: grid; place-content: center; gap: 2px; text-align: center; pointer-events: none; }
.chart-center strong { color: var(--ink-strong); font-size: 15px; line-height: 1; }
.chart-center small { color: var(--muted-light); font-size: 9px; font-weight: 700; white-space: nowrap; }
</style>
