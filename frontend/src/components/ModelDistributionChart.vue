<template>
  <Doughnut :data="chartData" :options="chartOptions" />
</template>

<script setup lang="ts">
import { ArcElement, Chart as ChartJS, DoughnutController, Legend, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import type { ModelUsage } from '../types'
import type { ResolvedTheme } from '../themePreference'
import { formatTokens } from '../dashboardFormat'

ChartJS.register(DoughnutController, ArcElement, Tooltip, Legend)

const props = defineProps<{ usage: ModelUsage[]; theme: ResolvedTheme }>()
const colors = ['#4b7bec', '#18a27f', '#d59020', '#8b5cf6', '#e45b78', '#18a6b8', '#eb7f43', '#6f7a8a', '#57b86d', '#a66dd4', '#e3b341', '#3d93d8']
const textColor = computed(() => props.theme === 'dark' ? '#a5a19b' : '#6d7078')
const tooltipColors = computed(() => props.theme === 'dark'
  ? { background: '#292a2d', title: '#f7f5f2' }
  : { background: '#ffffff', title: '#222327' })

const chartData = computed<ChartData<'doughnut'>>(() => ({
  labels: props.usage.map(item => item.model),
  datasets: [{
    data: props.usage.map(item => item.token_usage.total_tokens),
    backgroundColor: props.usage.map((_, index) => colors[index % colors.length]),
    borderWidth: 0,
    hoverOffset: 4,
  }],
}))

const chartOptions = computed<ChartOptions<'doughnut'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  cutout: '64%',
  animation: { duration: 350 },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: tooltipColors.value.background,
      titleColor: tooltipColors.value.title,
      bodyColor: textColor.value,
      borderColor: 'rgba(109,112,120,.2)',
      borderWidth: 1,
      padding: 11,
      callbacks: { label: context => `Token: ${formatTokens(Number(context.parsed))}` },
    },
  },
}))
</script>
