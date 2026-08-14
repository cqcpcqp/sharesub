<template>
  <section class="view-content admin-detail-shell">
    <header class="admin-context-bar">
      <NButton quaternary size="small" @click="emit('back')">
        <template #icon><ArrowLeft :size="16" /></template>
        后台管理
      </NButton>
      <div class="admin-context-note">
        <ShieldCheck :size="16" />
        <span>管理员权限</span>
        <small>操作将记录为你的管理员身份，不会模拟 Plan 房主</small>
      </div>
    </header>
    <div v-if="loading" class="detail-loading"><NSpin size="small" /></div>
    <NAlert v-else-if="loadError" type="error" :show-icon="true">
      <template #header>无法加载管理员 Plan 数据</template>
      {{ loadError }}
      <NButton class="retry-button" size="small" secondary @click="loadResources">重试</NButton>
    </NAlert>
    <PlansView
      v-else
      :accounts="accounts"
      :plans="plans"
      :user="currentUser"
      :theme="theme"
      :initial-plan-id="planId"
      admin-mode
      @changed="refreshResources"
      @deleted="emit('back')"
      @message="(type, text) => emit('message', type, text)"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NAlert, NButton, NSpin } from 'naive-ui'
import { ArrowLeft, ShieldCheck } from 'lucide-vue-next'
import { adminAPI } from '../api/admin'
import type { AdminAccount, AdminPlan, User } from '../types'
import type { ResolvedTheme } from '../themePreference'
import PlansView from './PlansView.vue'

defineProps<{ planId: string; currentUser: User; theme: ResolvedTheme }>()
const emit = defineEmits<{
  back: []
  changed: []
  message: [type: 'success' | 'error', text: string]
}>()
const accounts = ref<AdminAccount[]>([])
const plans = ref<AdminPlan[]>([])
const loading = ref(true)
const loadError = ref('')

async function loadResources() {
  loading.value = true
  loadError.value = ''
  try {
    ;[accounts.value, plans.value] = await Promise.all([adminAPI.adminAccounts(), adminAPI.adminPlans()])
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

async function refreshResources() {
  try {
    ;[accounts.value, plans.value] = await Promise.all([adminAPI.adminAccounts(), adminAPI.adminPlans()])
    emit('changed')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  }
}

onMounted(loadResources)
</script>

<style scoped>
.admin-detail-shell { display: grid; gap: 18px; }
.admin-context-bar { min-height: 36px; display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 0 2px 12px; border-bottom: 1px solid var(--line); }
.admin-context-note { display: flex; align-items: center; gap: 7px; color: var(--info-ink); }
.admin-context-bar span { font-size: 11px; font-weight: 800; }
.admin-context-bar small { font-size: 11px; }
.detail-loading { min-height: 280px; display: grid; place-items: center; }
.retry-button { margin-left: 10px; }
@media (max-width: 560px) {
  .admin-context-bar { align-items: flex-start; flex-direction: column; }
  .admin-context-note small { display: none; }
}
</style>
