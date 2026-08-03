<template>
  <NAvatar :size="size" :src="src || undefined" :img-props="{ alt: `${username}的头像` }">
    {{ username.slice(0, 1).toUpperCase() }}
  </NAvatar>
</template>

<script setup lang="ts">
import { NAvatar } from 'naive-ui'

withDefaults(defineProps<{
  username: string
  src?: string
  size?: number
}>(), {
  src: '',
  size: 36,
})
</script>
