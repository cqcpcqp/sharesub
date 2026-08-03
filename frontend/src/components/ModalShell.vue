<template>
  <NModal :show="true" :mask-closable="true" :close-on-esc="true" :trap-focus="false" @update:show="handleShowChange">
    <section class="modal" :class="{ wide }" role="dialog" aria-modal="true" :aria-labelledby="titleID">
      <header><div><h2 :id="titleID">{{ title }}</h2><p v-if="subtitle">{{ subtitle }}</p></div><NButton quaternary class="icon-button" title="关闭" aria-label="关闭" @click="$emit('close')"><template #icon><X :size="19" /></template></NButton></header>
      <slot />
      <footer v-if="$slots.footer"><slot name="footer" /></footer>
    </section>
  </NModal>
</template>

<script setup lang="ts">
import { NButton, NModal } from 'naive-ui'
import { X } from 'lucide-vue-next'
import { useId } from 'vue'
withDefaults(defineProps<{ title: string; subtitle?: string; wide?: boolean }>(), { subtitle: '', wide: false })
const emit = defineEmits<{ close: [] }>()
const titleID = `modal-${useId()}`
function handleShowChange(show: boolean) { if (!show) emit('close') }
</script>
