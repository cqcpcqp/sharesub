<template>
  <Line :data="chartData" :options="chartOptions" />
</template>

<script setup lang="ts">
import { CategoryScale, Chart as ChartJS, Legend, LinearScale, LineElement, PointElement, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { computed } from 'vue'
import { Line } from 'vue-chartjs'
import type { MemberUsageTrend } from '../types'
import type { ResolvedTheme } from '../themePreference'
import { formatTokens } from '../dashboardFormat'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const props = defineProps<{ usage: MemberUsageTrend[]; theme: ResolvedTheme }>()
const colors = ['#4b7bec', '#18a27f', '#d59020', '#e05260', '#8b5cf6', '#d64b8c', '#18a6b8', '#eb7f43', '#57b86d', '#a66dd4', '#e3b341', '#3d93d8']
const palette = computed(() => props.theme === 'dark' ? {
  text: '#a5a19b', grid: 'rgba(165,161,155,.15)', tooltip: '#f7f5f2', tooltipBackground: '#292a2d',
} : {
  text: '#6d7078', grid: 'rgba(109,112,120,.14)', tooltip: '#222327', tooltipBackground: '#ffffff',
})

const chartData = computed<ChartData<'line'>>(() => ({
  labels: props.usage[0].trend.map(point => new Date(point.bucket_start).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })),
  datasets: props.usage.map((member, index) => ({
    label: member.username,
    data: member.trend.map(point => point.input_tokens + point.output_tokens),
    borderColor: colors[index % colors.length],
    backgroundColor: colors[index % colors.length],
    tension: .34,
    borderWidth: 2,
    pointRadius: 0,
    pointHoverRadius: 4,
  })),
}))

const chartOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 350 },
  interaction: { intersect: false, mode: 'index' },
  plugins: {
    legend: { labels: { color: palette.value.text, usePointStyle: true, pointStyle: 'circle', boxWidth: 7, boxHeight: 7, padding: 16, font: { size: 10, weight: 600 } } },
    tooltip: {
      backgroundColor: palette.value.tooltipBackground,
      titleColor: palette.value.tooltip,
      bodyColor: palette.value.text,
      borderColor: palette.value.grid,
      borderWidth: 1,
      padding: 12,
      callbacks: { label: context => `${context.dataset.label}: ${formatTokens(Number(context.parsed.y))}` },
    },
  },
  scales: {
    x: { border: { display: false }, grid: { display: false }, ticks: { color: palette.value.text, maxTicksLimit: 8, maxRotation: 0, font: { size: 10 } } },
    y: { beginAtZero: true, border: { display: false }, grid: { color: palette.value.grid }, ticks: { color: palette.value.text, maxTicksLimit: 6, padding: 8, callback: value => formatTokens(Number(value)), font: { size: 10 } } },
  },
}))
</script>
