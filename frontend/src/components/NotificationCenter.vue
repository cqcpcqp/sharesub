<template>
  <NBadge :value="unreadCount" :max="99" :show="unreadCount > 0">
    <NButton secondary class="icon-button notification-trigger" title="通知中心" aria-label="通知中心" @click="openDrawer">
      <template #icon><Bell :size="18" /></template>
    </NButton>
  </NBadge>

  <NDrawer v-model:show="show" :width="410" placement="right" class="notification-drawer">
    <NDrawerContent closable>
      <template #header><div class="notification-drawer-title"><strong>通知中心</strong><NButton v-if="unreadCount" text type="primary" :loading="readingAll" @click="emit('readAll')"><template #icon><CheckCheck :size="16" /></template>全部已读</NButton></div></template>
      <NSpin v-if="loading && !items.length" class="notification-loading" size="small" />
      <div v-else-if="items.length" class="notification-list">
        <NButton v-for="item in items" :key="item.id" quaternary class="notification-item" :class="{ unread: !item.read_at }" @click="openNotification(item)">
          <span class="notification-icon"><component :is="iconFor(item.type)" :size="18" /></span>
          <span class="notification-copy"><span><strong>{{ item.title }}</strong><i v-if="!item.read_at" /></span><small>{{ item.body }}</small><NTime :time="new Date(item.created_at)" type="relative" /></span>
          <ChevronRight :size="16" />
        </NButton>
      </div>
      <NEmpty v-else description="暂时没有通知" class="notification-empty" />
    </NDrawerContent>
  </NDrawer>
</template>

<script setup lang="ts">
import { NBadge, NButton, NDrawer, NDrawerContent, NEmpty, NSpin, NTime } from 'naive-ui'
import { ref } from 'vue'
import { Archive, Bell, CheckCircle2, CheckCheck, ChevronRight, CircleX, Info, Layers3, LogOut, RefreshCw, UserMinus, UserPlus } from 'lucide-vue-next'
import type { Notification } from '../types'

defineProps<{ items: Notification[]; unreadCount: number; loading: boolean; readingAll: boolean }>()
const emit = defineEmits<{
  refresh: []
  readAll: []
  read: [notification: Notification]
  open: [notification: Notification]
}>()
const show = ref(false)

function openDrawer() { show.value = true; emit('refresh') }
function openNotification(notification: Notification) {
  if (!notification.read_at) emit('read', notification)
  emit('open', notification)
  show.value = false
}
function iconFor(type: string) {
  if (type === 'join_application') return UserPlus
  if (type === 'application_approved') return CheckCircle2
  if (type === 'application_rejected') return CircleX
  if (type === 'plan_invite' || type === 'invite_accepted' || type === 'plan_joined') return UserPlus
  if (type === 'member_removed') return UserMinus
  if (type === 'member_left') return LogOut
  if (type === 'plan_archived' || type === 'plan_deleted') return Archive
  if (type === 'plan_restored' || type === 'account_reauthorized') return RefreshCw
  if (type.startsWith('plan_')) return Layers3
  return Info
}
</script>
