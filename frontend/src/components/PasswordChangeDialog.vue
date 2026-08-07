<template>
  <ModalShell
    :title="forced ? '设置新密码' : '修改密码'"
    :subtitle="forced ? '首次登录必须更换临时管理员密码' : '更新你的 ShareSub 登录密码'"
    :closable="!forced"
    @close="emit('close')"
  >
    <NAlert v-if="forced" type="warning" :show-icon="true">临时密码只能用于首次登录。新密码设置成功后，后台管理功能才会开放。</NAlert>
    <form class="password-change-form" @submit.prevent="submit">
      <label>{{ forced ? '当前临时密码' : '当前密码' }}<AppInput :value="currentPassword" type="password" show-password-on="mousedown" :input-props="{ autocomplete: 'current-password', required: true }" @update:value="currentPassword = $event" /></label>
      <label>新密码<AppInput :value="newPassword" type="password" show-password-on="mousedown" :minlength="10" :maxlength="128" :input-props="{ autocomplete: 'new-password', required: true }" @update:value="newPassword = $event" /><small>至少 10 个字符，且不能与当前密码相同。</small></label>
      <label>确认新密码<AppInput :value="confirmation" type="password" show-password-on="mousedown" :minlength="10" :maxlength="128" :input-props="{ autocomplete: 'new-password', required: true }" @update:value="confirmation = $event" /></label>
      <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
      <div class="password-change-actions">
        <NButton v-if="!forced" secondary :disabled="busy" @click="emit('close')">取消</NButton>
        <NButton type="primary" attr-type="submit" :loading="busy" :disabled="!canSubmit">保存新密码</NButton>
      </div>
    </form>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NAlert, NButton } from 'naive-ui'
import { api } from '../api'
import type { User } from '../types'
import AppInput from './AppInput.vue'
import ModalShell from './ModalShell.vue'

withDefaults(defineProps<{ forced?: boolean }>(), { forced: true })
const emit = defineEmits<{ changed: [user: User]; close: [] }>()
const currentPassword = ref('')
const newPassword = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref('')
const canSubmit = computed(() => currentPassword.value.length >= 10 && newPassword.value.length >= 10 && newPassword.value === confirmation.value && newPassword.value !== currentPassword.value)
async function submit() {
  if (!canSubmit.value || busy.value) return
  busy.value = true
  error.value = ''
  try { emit('changed', await api.changePassword(currentPassword.value, newPassword.value)) }
  catch (value) { error.value = value instanceof Error ? value.message : String(value) }
  finally { busy.value = false }
}
</script>

<style scoped>
.password-change-form { display: grid; gap: 14px; margin-top: 16px; }
.password-change-form label { display: grid; gap: 7px; color: var(--ink); font-size: 11px; font-weight: 700; }
.password-change-form label small { color: var(--muted); font-size: 11px; font-weight: 500; }
.password-change-actions { display: flex; justify-content: flex-end; gap: 10px; }
</style>
