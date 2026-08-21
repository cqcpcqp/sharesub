<template>
  <Line :data="chartData" :options="chartOptions" />
</template>

<script setup lang="ts">
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartData,
  type ChartOptions,
} from 'chart.js'
import { computed } from 'vue'
import { Line } from 'vue-chartjs'
import type { DashboardTrendPoint } from '../types'
import type { ResolvedTheme } from '../themePreference'
import { formatTokens } from '../dashboardFormat'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const props = defineProps<{
  trend: DashboardTrendPoint[]
  theme: ResolvedTheme
}>()

const palette = computed(() => props.theme === 'dark' ? {
  text: '#a5a19b',
  grid: 'rgba(165, 161, 155, .15)',
  tooltip: '#f7f5f2',
  tooltipBackground: '#292a2d',
  input: '#ed715c',
  inputFill: 'rgba(237, 113, 92, .12)',
  output: '#dca64a',
  outputFill: 'rgba(220, 166, 74, .08)',
  cached: '#a5a19b',
} : {
  text: '#6d7078',
  grid: 'rgba(109, 112, 120, .14)',
  tooltip: '#222327',
  tooltipBackground: '#ffffff',
  input: '#df5943',
  inputFill: 'rgba(223, 89, 67, .11)',
  output: '#b47814',
  outputFill: 'rgba(180, 120, 20, .07)',
  cached: '#7b7d84',
})

const chartData = computed<ChartData<'line'>>(() => ({
  labels: props.trend.map(point => new Date(point.bucket_start).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })),
  datasets: [
    {
      label: 'Input',
      data: props.trend.map(point => point.input_tokens),
      borderColor: palette.value.input,
      backgroundColor: palette.value.inputFill,
      fill: true,
      tension: 0.34,
      borderWidth: 2,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
    {
      label: 'Output',
      data: props.trend.map(point => point.output_tokens),
      borderColor: palette.value.output,
      backgroundColor: palette.value.outputFill,
      fill: false,
      tension: 0.34,
      borderWidth: 2,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
    {
      label: 'Cached',
      data: props.trend.map(point => point.cached_tokens),
      borderColor: palette.value.cached,
      backgroundColor: 'transparent',
      borderDash: [5, 5],
      fill: false,
      tension: 0.34,
      borderWidth: 2,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
  ],
}))

const chartOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 350 },
  interaction: { intersect: false, mode: 'index' },
  plugins: {
    legend: {
      align: 'end',
      labels: {
        color: palette.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        boxWidth: 7,
        boxHeight: 7,
        padding: 18,
        font: { size: 11, weight: 600 },
      },
    },
    tooltip: {
      backgroundColor: palette.value.tooltipBackground,
      titleColor: palette.value.tooltip,
      bodyColor: palette.value.text,
      borderColor: palette.value.grid,
      borderWidth: 1,
      padding: 12,
      displayColors: true,
      callbacks: {
        label: context => `${context.dataset.label}: ${formatTokens(Number(context.parsed.y))}`,
      },
    },
  },
  scales: {
    x: {
      border: { display: false },
      grid: { display: false },
      ticks: { color: palette.value.text, maxTicksLimit: 8, maxRotation: 0, font: { size: 10 } },
    },
    y: {
      beginAtZero: true,
      border: { display: false },
      grid: { color: palette.value.grid },
      ticks: { color: palette.value.text, maxTicksLimit: 6, padding: 8, callback: value => formatTokens(Number(value)), font: { size: 10 } },
    },
  },
}))
</script>
