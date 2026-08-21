<template>
  <section ref="panelRoot" class="dashboard-activity-panel" aria-label="最近 365 天每日 Token 使用情况">
    <div ref="heatmapRoot" class="activity-heatmap-scroll" @scroll="hideTooltip(tooltipIndex)">
      <div class="activity-heatmap" :style="{ '--activity-week-count': weeks.length }">
        <div class="activity-month-row" aria-hidden="true">
          <span
            v-for="label in monthLabels"
            :key="`${label.weekIndex}-${label.text}`"
            :style="{ gridColumn: label.weekIndex + 1 }"
          >{{ label.text }}</span>
        </div>
        <div class="activity-chart-row">
          <div class="activity-weekday-labels" aria-hidden="true">
            <span></span><span>一</span><span></span><span>三</span><span></span><span>五</span><span></span>
          </div>
          <div class="activity-weeks" role="group" aria-label="最近 365 天每日 Token 使用情况">
            <div v-for="(week, weekIndex) in weeks" :key="weekIndex" class="activity-week">
              <template v-for="(day, weekdayIndex) in week" :key="weekdayIndex">
                <span v-if="day === null" class="activity-cell-spacer" aria-hidden="true"></span>
                <button
                  v-else
                  type="button"
                  class="activity-cell"
                  :class="`activity-level-${activityLevels[day.index]}`"
                  :data-usage-index="day.index"
                  :aria-label="dayLabel(day.usage)"
                  :aria-describedby="tooltipIndex === day.index ? 'token-activity-tooltip' : undefined"
                  :aria-current="day.index === usage.length - 1 ? 'date' : undefined"
                  :tabindex="day.index === selectedIndex ? 0 : -1"
                  @mouseenter="showTooltip($event, day.usage, day.index)"
                  @mouseleave="hideTooltip(day.index)"
                  @focus="showTooltip($event, day.usage, day.index)"
                  @blur="hideTooltip(day.index)"
                  @click="showTooltip($event, day.usage, day.index)"
                  @keydown="moveFocus($event, day.index)"
                ></button>
              </template>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div
      v-if="tooltipUsage !== null"
      id="token-activity-tooltip"
      class="activity-tooltip"
      role="tooltip"
      :style="{ left: `${tooltipLeft}px`, top: `${tooltipTop}px` }"
    >
      <strong>{{ formatUTCDate(tooltipUsage.usage_date) }}</strong>
      <span>{{ formatTokens(tooltipUsage.token_usage.total_tokens) }} Token</span>
    </div>

    <footer class="activity-legend-row">
      <div class="activity-legend" aria-label="颜色深浅图例">
        <span>少</span>
        <i v-for="level in 5" :key="level" :class="`activity-level-${level - 1}`"></i>
        <span>多</span>
      </div>
    </footer>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { formatTokens } from '../dashboardFormat'
import type { DashboardDailyUsage } from '../types'

const props = defineProps<{
  usage: DashboardDailyUsage[]
}>()

interface IndexedUsage {
  index: number
  usage: DashboardDailyUsage
}

interface MonthLabel {
  weekIndex: number
  text: string
}

const panelRoot = ref<HTMLElement | null>(null)
const heatmapRoot = ref<HTMLElement | null>(null)
const selectedIndex = ref(props.usage.length - 1)
const tooltipIndex = ref<number | null>(null)
const tooltipUsage = ref<DashboardDailyUsage | null>(null)
const tooltipLeft = ref(0)
const tooltipTop = ref(0)

watch(() => props.usage, usage => {
  selectedIndex.value = usage.length - 1
  tooltipIndex.value = null
  tooltipUsage.value = null
  void scrollToLatest()
})

onMounted(() => {
  void scrollToLatest()
})

const rankedNonZeroUsage = computed(() => props.usage
  .map(day => day.token_usage.total_tokens)
  .filter(value => value > 0)
  .sort((left, right) => left - right))

const activityLevels = computed(() => props.usage.map(day => {
  const value = day.token_usage.total_tokens
  if (value === 0) return 0
  let upperRank = 0
  for (const rankedValue of rankedNonZeroUsage.value) {
    if (rankedValue <= value) upperRank++
  }
  return Math.max(1, Math.ceil(upperRank / rankedNonZeroUsage.value.length * 4))
}))

const weeks = computed<Array<Array<IndexedUsage | null>>>(() => {
  const firstDate = parseUTCDate(props.usage[0].usage_date)
  const cells: Array<IndexedUsage | null> = Array.from({ length: firstDate.getUTCDay() }, () => null)
  props.usage.forEach((usage, index) => cells.push({ index, usage }))
  while (cells.length % 7 !== 0) cells.push(null)

  const result: Array<Array<IndexedUsage | null>> = []
  for (let index = 0; index < cells.length; index += 7) result.push(cells.slice(index, index + 7))
  return result
})

const monthLabels = computed<MonthLabel[]>(() => weeks.value.flatMap((week, weekIndex) => {
  const firstOfMonth = week.find(day => day !== null && parseUTCDate(day.usage.usage_date).getUTCDate() === 1)
  const firstVisibleDay = weekIndex === 0 ? week.find(day => day !== null) : null
  const labelDay = firstOfMonth ?? firstVisibleDay
  return labelDay === null || labelDay === undefined
    ? []
    : [{ weekIndex, text: `${parseUTCDate(labelDay.usage.usage_date).getUTCMonth() + 1}月` }]
}))

function dayLabel(day: DashboardDailyUsage): string {
  return `${formatUTCDate(day.usage_date)}：${formatTokens(day.token_usage.total_tokens)} Token`
}

function formatUTCDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', year: 'numeric', month: 'long', day: 'numeric' }).format(parseUTCDate(value))
}

function parseUTCDate(value: string): Date {
  return new Date(`${value}T00:00:00Z`)
}

function showTooltip(event: MouseEvent | FocusEvent, usage: DashboardDailyUsage, index: number) {
  const panel = panelRoot.value
  const target = event.currentTarget as HTMLElement
  if (panel === null) return

  const panelRect = panel.getBoundingClientRect()
  const targetRect = target.getBoundingClientRect()
  const targetCenter = targetRect.left + targetRect.width / 2 - panelRect.left
  const horizontalInset = Math.min(170, panelRect.width / 2)
  tooltipLeft.value = Math.min(Math.max(targetCenter, horizontalInset), panelRect.width - horizontalInset)
  tooltipTop.value = targetRect.top - panelRect.top - 8
  selectedIndex.value = index
  tooltipIndex.value = index
  tooltipUsage.value = usage
}

function hideTooltip(index: number | null) {
  if (tooltipIndex.value !== index) return
  tooltipIndex.value = null
  tooltipUsage.value = null
}

async function scrollToLatest() {
  await nextTick()
  if (heatmapRoot.value !== null) heatmapRoot.value.scrollLeft = heatmapRoot.value.scrollWidth
}

function moveFocus(event: KeyboardEvent, index: number) {
  if (event.key === 'Escape') {
    event.preventDefault()
    hideTooltip(index)
    return
  }

  const movement: Record<string, number> = { ArrowUp: -1, ArrowDown: 1, ArrowLeft: -7, ArrowRight: 7 }
  let targetIndex: number
  if (event.key === 'Home') targetIndex = 0
  else if (event.key === 'End') targetIndex = props.usage.length - 1
  else if (event.key in movement) targetIndex = Math.min(props.usage.length - 1, Math.max(0, index + movement[event.key]))
  else return

  event.preventDefault()
  selectedIndex.value = targetIndex
  heatmapRoot.value?.querySelector<HTMLElement>(`[data-usage-index="${targetIndex}"]`)?.focus()
}
</script>
