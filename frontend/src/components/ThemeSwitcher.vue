<template>
  <div class="theme-switcher">
    <NDropdown trigger="click" placement="bottom-end" :options="options" @select="selectTheme">
      <NButton secondary class="icon-button" :title="`主题：${themeLabels[modelValue]}`" :aria-label="`主题：${themeLabels[modelValue]}`">
        <template #icon><component :is="currentIcon" :size="18" /></template>
      </NButton>
    </NDropdown>
  </div>
</template>

<script setup lang="ts">
import { computed, h } from 'vue'
import { NButton, NDropdown, type DropdownOption } from 'naive-ui'
import { Check, Monitor, Moon, Sun } from 'lucide-vue-next'
import type { ThemeMode } from '../themePreference'

const props = defineProps<{ modelValue: ThemeMode }>()
const emit = defineEmits<{ 'update:modelValue': [mode: ThemeMode] }>()

const themeLabels: Record<ThemeMode, string> = {
  system: '跟随系统',
  light: '浅色',
  dark: '深色',
}
const themeIcons = { system: Monitor, light: Sun, dark: Moon }
const currentIcon = computed(() => themeIcons[props.modelValue])
const options = computed<DropdownOption[]>(() => (Object.keys(themeLabels) as ThemeMode[]).map(key => ({
  key,
  icon: () => h(themeIcons[key], { size: 16 }),
  label: () => h('span', { class: 'theme-option-label' }, [
    h('span', themeLabels[key]),
    props.modelValue === key ? h(Check, { size: 14 }) : null,
  ]),
})))

function selectTheme(key: string | number) {
  emit('update:modelValue', key as ThemeMode)
}
</script>
