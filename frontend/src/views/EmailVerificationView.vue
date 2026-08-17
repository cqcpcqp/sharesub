<template>
  <AuthScaffold>
    <div class="auth-brand"><BrandMark :size="42" /><div><strong>ShareSub</strong><span>Access together</span></div></div>
    <div class="auth-heading"><h1>完成邮箱验证</h1><p>确认后，这个邮箱将成为你的 ShareSub 登录凭据。</p></div>
    <div class="verification-confirm" aria-live="polite">
      <span class="verification-state-icon" :class="{ invalid: !token }"><ShieldCheck v-if="token" :size="25" /><MailWarning v-else :size="25" /></span>
      <div v-if="token">
        <h2>验证这次注册</h2>
        <p>此链接只能使用一次。点击后，我们会验证邮箱并安全地为你登录。</p>
      </div>
      <div v-else>
        <h2>验证链接不完整</h2>
        <p>邮件中的验证令牌缺失或格式不正确，请返回登录页重新发送验证邮件。</p>
      </div>
    </div>
    <NAlert v-if="error" class="verification-alert" type="error" :show-icon="true">{{ error }}</NAlert>
    <div class="verification-actions">
      <NButton v-if="token" type="primary" block :loading="busy" @click="verify">
        <template #icon><BadgeCheck :size="18" /></template>完成邮箱验证
      </NButton>
      <NButton :secondary="Boolean(token)" :block="!token" @click="emit('login')">
        <template #icon><ArrowLeft :size="16" /></template>返回登录
      </NButton>
    </div>
    <footer class="auth-legal"><p>验证链接将在邮件发送 1 小时后失效。ShareSub 不会通过邮件索要你的密码。</p></footer>
  </AuthScaffold>
</template>

<script setup lang="ts">
import { NAlert, NButton } from 'naive-ui'
import { ref } from 'vue'
import { ArrowLeft, BadgeCheck, MailWarning, ShieldCheck } from 'lucide-vue-next'
import { APIRequestError, api, setSessionToken } from '../api'
import type { User } from '../types'
import AuthScaffold from '../components/AuthScaffold.vue'
import BrandMark from '../components/BrandMark.vue'

const props = defineProps<{ token: string }>()
const emit = defineEmits<{ authenticated: [user: User]; login: [] }>()
const busy = ref(false)
const error = ref('')

async function verify() {
  if (!props.token || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const result = await api.verifyEmail(props.token)
    setSessionToken(result.token)
    window.history.replaceState(null, '', '/verify-email')
    emit('authenticated', result.user)
  } catch (reason) {
    error.value = reason instanceof APIRequestError && reason.code === 'email_verification_invalid'
      ? '验证链接已失效或已经使用过，请返回登录页重新发送。'
      : reason instanceof Error ? reason.message : String(reason)
  } finally {
    busy.value = false
  }
}
</script>
