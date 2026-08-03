<template>
  <img class="brand-logo" :src="source" :width="size" :height="size" alt="" />
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{ size?: number; inverse?: boolean }>(), {
  size: 42,
  inverse: false,
})

const source = computed(() => props.inverse
  ? '/brand/sharesub-mark-inverse.svg'
  : '/brand/sharesub-mark.svg')
</script>
