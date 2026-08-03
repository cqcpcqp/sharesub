<template>
  <NPopover
    v-model:show="show"
    trigger="click"
    placement="bottom-start"
    to="body"
    :show-arrow="false"
    :style="{ width: '304px', maxWidth: 'calc(100vw - 32px)' }"
  >
    <template #trigger>
      <NButton
        secondary
        icon-placement="right"
        class="share-picker-trigger"
        :class="{ compact }"
        :size="compact ? 'small' : 'medium'"
        :disabled="disabled"
        :aria-label="ariaLabel"
      >
        <span class="share-trigger-copy">
          <strong>{{ currentValue }}%</strong>
          <small v-if="!compact && currentFraction">{{ currentFraction }}</small>
        </span>
        <template #icon><ChevronDown :size="15" /></template>
      </NButton>
    </template>

    <div class="share-picker-panel">
      <header>
        <div><strong>选择份额</strong><small>整数百分比</small></div>
        <output>{{ currentValue }}%</output>
      </header>
      <NSlider
        :value="currentValue"
        :min="1"
        :max="100"
        :step="1"
        :format-tooltip="formatTooltip"
        aria-label="份额百分比"
        @update:value="updateFromSlider"
      />
      <div class="share-presets" aria-label="常用份额">
        <NButton
          v-for="preset in presets"
          :key="preset.value"
          size="small"
          :secondary="currentValue === preset.value"
          :quaternary="currentValue !== preset.value"
          :type="currentValue === preset.value ? 'primary' : 'default'"
          @click="choosePreset(preset.value)"
        >
          <span><strong>{{ preset.value }}%</strong><small>{{ preset.label }}</small></span>
        </NButton>
      </div>
    </div>
  </NPopover>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NPopover, NSlider } from 'naive-ui'
import { ChevronDown } from 'lucide-vue-next'

const props = withDefaults(defineProps<{
  modelValue: number
  compact?: boolean
  disabled?: boolean
  ariaLabel?: string
}>(), {
  compact: false,
  disabled: false,
  ariaLabel: '选择份额',
})

const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const show = ref(false)
const currentValue = computed(() => Math.round(props.modelValue))
const presets = [
  { value: 10, label: '1/10' },
  { value: 20, label: '1/5' },
  { value: 25, label: '1/4' },
  { value: 33, label: '≈ 1/3' },
  { value: 50, label: '1/2' },
  { value: 67, label: '≈ 2/3' },
  { value: 75, label: '3/4' },
  { value: 100, label: '全部' },
]
const currentFraction = computed(() => presets.find(preset => preset.value === currentValue.value)?.label ?? '')

function updateFromSlider(value: number | number[]) {
  if (typeof value === 'number') emit('update:modelValue', value)
}

function choosePreset(value: number) {
  emit('update:modelValue', value)
  show.value = false
}

function formatTooltip(value: number) {
  return `${value}%`
}
</script>

<style scoped>
.share-picker-trigger {
  width: 100%;
}

.share-picker-trigger.compact {
  width: 104px;
}

.share-picker-trigger :deep(.n-button__content) {
  width: 100%;
  justify-content: space-between;
}

.share-trigger-copy {
  min-width: 0;
  display: inline-flex;
  align-items: baseline;
  gap: 7px;
}

.share-trigger-copy strong {
  color: var(--ink-strong);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.share-trigger-copy small {
  color: var(--muted);
  font-size: 9px;
  font-weight: 600;
}

.share-picker-panel {
  display: grid;
  gap: 18px;
  padding: 4px 2px 2px;
}

.share-picker-panel > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.share-picker-panel > header > div {
  display: grid;
  gap: 3px;
}

.share-picker-panel header strong {
  color: var(--ink-strong);
  font-size: 12px;
}

.share-picker-panel header small {
  color: var(--muted);
  font-size: 9px;
}

.share-picker-panel output {
  color: var(--primary);
  font-size: 20px;
  font-weight: 760;
  font-variant-numeric: tabular-nums;
}

.share-presets {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 5px;
}

.share-presets .n-button {
  width: 100%;
  height: 48px;
  padding: 0 4px;
}

.share-presets .n-button span {
  display: grid;
  gap: 2px;
  text-align: center;
}

.share-presets strong {
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}

.share-presets small {
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}
</style>
