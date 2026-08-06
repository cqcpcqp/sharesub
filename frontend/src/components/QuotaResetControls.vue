<template>
  <section class="quota-reset-controls" aria-label="7d 额度重置">
    <div class="quota-reset-action-row">
      <span class="quota-reset-label"><RotateCcw :size="13" />重置机会</span>
      <div class="quota-reset-actions">
        <NButton
          size="tiny"
          quaternary
          type="primary"
          :loading="querying"
          :disabled="busy"
          aria-label="查询重置次数"
          @click="emit('query')"
        >
          {{ credits ? `次数 ${credits.available_count}` : '查询重置次数' }}
        </NButton>
        <NPopconfirm
          :disabled="!canReset"
          positive-text="确认重置"
          negative-text="取消"
          @positive-click="emit('reset')"
        >
          <template #trigger>
            <NButton
              size="tiny"
              quaternary
              type="warning"
              :loading="resetting"
              :disabled="!canReset"
              :title="allowReset ? undefined : '只有房主可以执行额度重置'"
              aria-label="重置 OpenAI 额度"
            >
              重置
            </NButton>
          </template>
          将消耗 1 次重置机会。OpenAI 可能同时重置当前 5h 与 7d 额度窗口，此操作不可撤销。
        </NPopconfirm>
      </div>
    </div>

    <div v-if="credits && expirationTimes.length" class="quota-reset-expirations">
      <span class="quota-reset-expiry" :title="formatFullDate(expirationTimes[0])">
        到期 {{ formatShortDate(expirationTimes[0]) }}
      </span>
      <button
        v-if="hiddenExpirationCount"
        type="button"
        class="quota-reset-more"
        :aria-expanded="showAllExpirations"
        :aria-label="showAllExpirations ? '收起重置机会到期时间' : `查看其余 ${hiddenExpirationCount} 个到期时间`"
        @click="showAllExpirations = !showAllExpirations"
      >
        {{ showAllExpirations ? '收起' : `+${hiddenExpirationCount}` }}
      </button>
    </div>
    <div v-if="credits && showAllExpirations" class="quota-reset-expiration-list">
      <span v-for="(expiresAt, index) in expirationTimes" :key="`${expiresAt}-${index}`" :title="formatFullDate(expiresAt)">
        <i aria-hidden="true" />{{ formatShortDate(expiresAt) }}
      </span>
    </div>
    <p v-else-if="credits && credits.available_count === 0" class="quota-reset-empty">当前没有可用的重置机会</p>
    <p v-else-if="!credits" class="quota-reset-hint">
      查询 OpenAI 当前可用次数及到期时间<span v-if="!allowReset">；仅房主可执行重置</span>
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NButton, NPopconfirm } from 'naive-ui'
import { RotateCcw } from 'lucide-vue-next'
import type { QuotaResetCredits } from '../types'

const props = defineProps<{
  credits: QuotaResetCredits | null
  querying: boolean
  resetting: boolean
  disabled: boolean
  allowReset: boolean
}>()

const emit = defineEmits<{ query: []; reset: [] }>()
const showAllExpirations = ref(false)
const busy = computed(() => props.disabled || props.querying || props.resetting)
const canReset = computed(() => props.allowReset && props.credits !== null && props.credits.available_count > 0 && !busy.value)
const expirationTimes = computed(() => (props.credits === null ? [] : [...props.credits.credits])
  .sort((left, right) => new Date(left.expires_at).getTime() - new Date(right.expires_at).getTime())
  .map(credit => credit.expires_at))
const hiddenExpirationCount = computed(() => Math.max(0, expirationTimes.value.length - 1))

const shortDateFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})
const fullDateFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

function formatShortDate(value: string) { return shortDateFormatter.format(new Date(value)) }
function formatFullDate(value: string) { return fullDateFormatter.format(new Date(value)) }

watch(() => props.credits, () => { showAllExpirations.value = false })
</script>

<style scoped>
.quota-reset-controls { margin-top: 13px; padding-top: 11px; border-top: 1px solid var(--line-soft); }
.quota-reset-action-row { min-width: 0; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.quota-reset-label { display: inline-flex; align-items: center; gap: 5px; color: var(--muted); font-size: 11px; font-weight: 760; }
.quota-reset-label svg { color: var(--blue); }
.quota-reset-actions { display: flex; align-items: center; gap: 2px; }
.quota-reset-actions :deep(.n-button) { padding: 0 6px; font-size: 11px; }
.quota-reset-expirations { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; margin-top: 7px; }
.quota-reset-expiry,
.quota-reset-more { min-height: 22px; display: inline-flex; align-items: center; border-radius: 6px; background: var(--surface-soft); color: var(--muted); font-size: 10px; font-variant-numeric: tabular-nums; line-height: 1.4; }
.quota-reset-expiry { padding: 3px 7px; }
.quota-reset-more { padding: 3px 7px; border: 0; cursor: pointer; font-weight: 760; }
.quota-reset-more:hover,
.quota-reset-more:focus-visible { background: var(--surface-hover); color: var(--ink); outline: 2px solid var(--primary-soft); outline-offset: 1px; }
.quota-reset-expiration-list { width: fit-content; max-width: 100%; display: grid; gap: 3px; margin-top: 5px; padding: 6px 8px; border: 1px solid var(--line-soft); border-radius: 7px; background: var(--surface-soft); }
.quota-reset-expiration-list span { min-width: 0; display: flex; align-items: center; gap: 6px; color: var(--muted); font-size: 10px; font-variant-numeric: tabular-nums; }
.quota-reset-expiration-list i { width: 4px; height: 4px; flex: 0 0 auto; border-radius: 50%; background: var(--muted-light); }
.quota-reset-hint,
.quota-reset-empty { margin: 6px 0 0; color: var(--muted-light); font-size: 10px; line-height: 1.45; }
.quota-reset-empty { color: var(--amber); }
</style>
