<template><NEmpty class="empty-state" :description="title" size="medium"><template #icon><component :is="icon" /></template><template #extra><span>{{ description }}</span></template></NEmpty></template>

<script setup lang="ts">
import { NEmpty } from 'naive-ui'
import { Inbox } from 'lucide-vue-next'
withDefaults(defineProps<{ title: string; description: string; icon?: typeof Inbox }>(), { icon: () => Inbox })
</script>
