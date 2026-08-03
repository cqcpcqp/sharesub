<template>
  <main class="auth-shell">
    <section class="auth-art" aria-hidden="true"><div class="auth-art-brand"><BrandMark :size="42" inverse /><strong>ShareSub</strong></div><div class="auth-art-copy"><span>SHARE · ROUTE · CREATE</span><p>一起使用，<br />也各自清楚。</p></div><div class="auth-art-blocks"><i /><i /><i /><i /></div></section>
    <section class="auth-panel"><div class="auth-form-wrap">
      <div class="auth-brand"><BrandMark :size="42" /><div><strong>ShareSub</strong><span>Access together</span></div></div>
      <div class="auth-heading"><span>{{ invitePending ? 'PLAN INVITATION' : mode === 'login' ? 'WELCOME BACK' : 'GET STARTED' }}</span><h1>{{ invitePending ? '登录后加入共享' : mode === 'login' ? '很高兴再见到你' : '创建你的账户' }}</h1><p>{{ invitePending ? '验证身份后，系统会自动接受这份邀请。' : mode === 'login' ? '登录后继续管理共享访问。' : '只需要一分钟，就可以开始。' }}</p></div>
      <div v-if="invitePending" class="auth-invite-context">
        <NSpin v-if="inviteLoading" size="small" />
        <template v-else-if="invitation"><span><Layers3 :size="18" /></span><div><strong>{{ invitation.plan_name }}</strong><small>{{ invitation.owner_username }} 邀请你加入 · 链接仅可使用一次</small></div></template>
        <template v-else-if="inviteError"><NAlert type="error">{{ inviteError }}</NAlert><div class="auth-invite-actions"><NButton text @click="emit('discardInvite')">放弃</NButton><NButton text type="primary" @click="emit('retryInvite')">重试</NButton></div></template>
      </div>
      <NButtonGroup class="segmented" aria-label="认证模式"><NButton :type="mode === 'login' ? 'primary' : 'default'" :secondary="mode === 'login'" :quaternary="mode !== 'login'" @click="mode = 'login'">登录</NButton><NButton :type="mode === 'register' ? 'primary' : 'default'" :secondary="mode === 'register'" :quaternary="mode !== 'register'" @click="mode = 'register'">注册</NButton></NButtonGroup>
      <form class="form-stack" @submit.prevent="submit">
        <label v-if="mode === 'register'">用户名<AppInput :value="form.username" clearable :minlength="2" :maxlength="32" placeholder="你的公开昵称" :input-props="{ autocomplete: 'username', required: true }" @update:value="updateFormField('username', $event)" /></label>
        <label>邮箱<AppInput :value="form.email" clearable placeholder="name@example.com" :input-props="{ type: 'email', autocomplete: 'email', required: true }" @update:value="updateFormField('email', $event)" /></label>
        <label>密码<AppInput :value="form.password" type="password" clearable show-password-on="click" :minlength="10" placeholder="至少 10 个字符" :input-props="{ autocomplete: mode === 'login' ? 'current-password' : 'new-password', required: true }" @update:value="updateFormField('password', $event)" /></label>
        <NButton type="primary" attr-type="submit" block :loading="busy"><template #icon><LogIn :size="18" /></template>{{ mode === 'login' ? '登录' : '创建账号' }}</NButton>
      </form>
      <p v-if="error" class="form-error">{{ error }}</p>
    </div></section>
  </main>
</template>

<script setup lang="ts">
import { NAlert, NButton, NButtonGroup, NSpin } from 'naive-ui'
import { reactive, ref } from 'vue'
import { Layers3, LogIn } from 'lucide-vue-next'
import { APIRequestError, api, setSessionToken } from '../api'
import type { InvitePreview, User } from '../types'
import BrandMark from '../components/BrandMark.vue'
import AppInput from '../components/AppInput.vue'

defineProps<{ invitePending: boolean; invitation: InvitePreview | null; inviteLoading: boolean; inviteError: string }>()
const emit = defineEmits<{ authenticated: [user: User]; retryInvite: []; discardInvite: [] }>()
const mode = ref<'login' | 'register'>('login')
const busy = ref(false)
const error = ref('')
const form = reactive({ username: '', email: '', password: '' })

function updateFormField(field: 'username' | 'email' | 'password', value: string) { form[field] = value }

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const result = mode.value === 'login'
      ? await api.login(form.email.trim(), form.password)
      : await api.register(form.username.trim(), form.email.trim(), form.password)
    setSessionToken(result.token)
    emit('authenticated', result.user)
  } catch (reason) {
    error.value = reason instanceof APIRequestError && reason.code === 'unauthorized'
      ? '邮箱或密码错误'
      : reason instanceof Error ? reason.message : String(reason)
  } finally {
    busy.value = false
  }
}
</script>
