<template>
  <NAvatar v-if="src" :size="size" :src="src" :img-props="{ alt: `${username}的头像` }">
    <template #fallback>{{ initial }}</template>
  </NAvatar>
  <NAvatar v-else :size="size">{{ initial }}</NAvatar>
</template>

<script setup lang="ts">
import { NAvatar } from 'naive-ui'
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  username: string
  src?: string
  size?: number
}>(), {
  src: '',
  size: 36,
})

const initial = computed(() => props.username.slice(0, 1).toUpperCase())
</script>
