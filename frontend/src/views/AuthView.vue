<template>
  <AuthScaffold>
      <div class="auth-brand"><BrandMark :size="42" /><div><strong>ShareSub</strong><span>Access together</span></div></div>
      <div class="auth-heading"><h1>{{ pendingEmail ? '检查你的邮箱' : invitePending ? '登录后确认邀请' : mode === 'login' ? '很高兴再见到你' : '创建你的账户' }}</h1><p>{{ pendingEmail ? '完成邮箱验证后，我们会安全地为你登录。' : invitePending ? '验证身份后，你可以确认加入条件和使用账号。' : mode === 'login' ? '登录后继续管理共享访问。' : '只需要一分钟，就可以开始。' }}</p></div>
      <div v-if="invitePending" class="auth-invite-context">
        <NSpin v-if="inviteLoading" size="small" />
        <template v-else-if="invitation"><InvitationSummary :preview="invitation" /><div class="auth-invite-actions"><NButton text @click="emit('discardInvite')">放弃邀请</NButton></div></template>
        <template v-else-if="inviteError"><NAlert type="error">{{ inviteError }}</NAlert><div class="auth-invite-actions"><NButton text @click="emit('discardInvite')">放弃</NButton><NButton text type="primary" @click="emit('retryInvite')">重试</NButton></div></template>
      </div>
      <div v-if="pendingEmail" class="verification-flow">
        <div class="verification-pending" aria-live="polite">
          <span class="verification-state-icon"><MailCheck :size="24" /></span>
          <div><h2>验证邮件已发送</h2><p>我们已向 <strong>{{ pendingEmail }}</strong> 发送验证链接。链接 1 小时内有效，只能使用一次。</p></div>
        </div>
        <NAlert v-if="error" class="verification-alert" type="error" :show-icon="true">{{ error }}</NAlert>
        <div class="verification-actions">
          <NButton type="primary" block :loading="busy" :disabled="resendSeconds > 0" @click="resendVerification">
            <template #icon><RefreshCw :size="17" /></template>
            {{ resendSeconds > 0 ? `${resendSeconds} 秒后可重新发送` : '重新发送验证邮件' }}
          </NButton>
          <NButton text @click="changeRegistrationEmail"><template #icon><ArrowLeft :size="16" /></template>换一个邮箱</NButton>
        </div>
      </div>
      <div v-else class="auth-credentials">
      <NButtonGroup class="segmented" aria-label="认证模式"><NButton :type="mode === 'login' ? 'primary' : 'default'" :secondary="mode === 'login'" :quaternary="mode !== 'login'" @click="switchMode('login')">登录</NButton><NButton :type="mode === 'register' ? 'primary' : 'default'" :secondary="mode === 'register'" :quaternary="mode !== 'register'" @click="switchMode('register')">注册</NButton></NButtonGroup>
      <form class="form-stack" @submit.prevent="submit">
        <label v-if="mode === 'register'">用户名<AppInput :value="form.username" clearable :minlength="2" :maxlength="32" placeholder="你的公开昵称" :input-props="{ autocomplete: 'username', required: true }" @update:value="updateFormField('username', $event)" /></label>
        <label>邮箱<AppInput :value="form.email" clearable placeholder="name@example.com" :input-props="{ type: 'email', autocomplete: 'email', required: true }" @update:value="updateFormField('email', $event)" /></label>
        <label>密码<AppInput :value="form.password" type="password" clearable show-password-on="click" :minlength="10" placeholder="至少 10 个字符" :input-props="{ autocomplete: mode === 'login' ? 'current-password' : 'new-password', required: true }" @update:value="updateFormField('password', $event)" /></label>
        <NCheckbox v-if="mode === 'register'" v-model:checked="agreementAccepted" class="agreement-check">
          我已阅读并同意
          <a href="/terms" target="_blank" rel="noopener" @click.stop>《用户协议》</a>、<a href="/privacy" target="_blank" rel="noopener" @click.stop>《隐私政策》</a>和<a href="/acceptable-use" target="_blank" rel="noopener" @click.stop>《可接受使用规范》</a>
        </NCheckbox>
        <NButton type="primary" attr-type="submit" block :loading="busy" :disabled="mode === 'register' && !agreementAccepted"><template #icon><LogIn :size="18" /></template>{{ mode === 'login' ? '登录' : '创建账号' }}</NButton>
      </form>
      <p v-if="error" class="form-error" role="alert" aria-live="polite">{{ error }}</p>
      </div>
      <footer class="auth-legal"><nav><a href="/terms">用户协议</a><a href="/privacy">隐私政策</a><a href="/acceptable-use">使用规范</a></nav><p>ShareSub 是独立产品，与 OpenAI 无隶属、授权或代理关系。</p></footer>
  </AuthScaffold>
</template>

<script setup lang="ts">
import { NAlert, NButton, NButtonGroup, NCheckbox, NSpin } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ArrowLeft, LogIn, MailCheck, RefreshCw } from 'lucide-vue-next'
import { APIRequestError, api, setSessionToken } from '../api'
import type { InvitePreview, User } from '../types'
import AuthScaffold from '../components/AuthScaffold.vue'
import AppInput from '../components/AppInput.vue'
import BrandMark from '../components/BrandMark.vue'
import InvitationSummary from '../components/InvitationSummary.vue'
import { agreementVersions } from '../agreements'

const props = withDefaults(defineProps<{ invitePending: boolean; invitation: InvitePreview | null; inviteLoading: boolean; inviteError: string; initialMode?: 'login' | 'register' }>(), { initialMode: 'login' })
const emit = defineEmits<{ authenticated: [user: User]; retryInvite: []; discardInvite: [] }>()
const mode = ref<'login' | 'register'>(props.initialMode)
const busy = ref(false)
const error = ref('')
const agreementAccepted = ref(false)
const form = reactive({ username: '', email: '', password: '' })
const pendingEmail = ref('')
const resendAvailableAt = ref(0)
const clock = ref(Date.now())
let clockTimer: ReturnType<typeof setInterval> | undefined
const resendSeconds = computed(() => Math.max(0, Math.ceil((resendAvailableAt.value - clock.value) / 1000)))

function updateFormField(field: 'username' | 'email' | 'password', value: string) { form[field] = value }
function switchMode(value: 'login' | 'register') { mode.value = value; error.value = '' }

function showPendingVerification(email: string, availableAt?: string) {
  pendingEmail.value = email
  resendAvailableAt.value = availableAt ? new Date(availableAt).getTime() : Date.now()
  clock.value = Date.now()
}

function changeRegistrationEmail() {
  pendingEmail.value = ''
  resendAvailableAt.value = 0
  error.value = ''
  mode.value = 'register'
}

async function resendVerification() {
  if (busy.value || resendSeconds.value > 0) return
  busy.value = true
  error.value = ''
  try {
    const result = await api.resendEmailVerification(pendingEmail.value)
    resendAvailableAt.value = new Date(result.resend_available_at).getTime()
    clock.value = Date.now()
  } catch (reason) {
    error.value = reason instanceof APIRequestError && reason.code === 'email_resend_too_soon'
      ? '发送得太频繁，请稍后再试。'
      : reason instanceof APIRequestError && reason.code === 'email_verification_limited'
        ? '请求次数过多，请一小时后再试。'
        : reason instanceof APIRequestError && reason.code === 'email_delivery_unavailable'
          ? '邮件服务暂时不可用，请稍后重新发送。'
          : reason instanceof Error ? reason.message : String(reason)
  } finally {
    busy.value = false
  }
}

async function submit() {
  if (busy.value) return
  if (mode.value === 'register' && !agreementAccepted.value) return
  busy.value = true
  error.value = ''
  try {
    if (mode.value === 'register') {
      const result = await api.register(form.username.trim(), form.email.trim(), form.password, {
        accepted: true,
        terms_version: agreementVersions.terms,
        privacy_policy_version: agreementVersions.privacy,
        acceptable_use_version: agreementVersions.acceptableUse,
      })
      form.password = ''
      showPendingVerification(result.email, result.resend_available_at)
      return
    }
    const result = await api.login(form.email.trim(), form.password)
    setSessionToken(result.token)
    emit('authenticated', result.user)
  } catch (reason) {
    if (reason instanceof APIRequestError && reason.code === 'email_verification_required') {
      showPendingVerification(form.email.trim())
      error.value = '这个邮箱还没有完成验证，请重新发送验证邮件。'
    } else if (reason instanceof APIRequestError && reason.code === 'email_delivery_unavailable' && mode.value === 'register') {
      showPendingVerification(form.email.trim(), new Date(Date.now() + 60_000).toISOString())
      form.password = ''
      error.value = '账号已创建，但邮件暂时未能送出。请稍后重新发送。'
    } else {
      error.value = reason instanceof APIRequestError && reason.code === 'unauthorized'
        ? '邮箱或密码错误'
        : reason instanceof Error ? reason.message : String(reason)
    }
  } finally {
    busy.value = false
  }
}

onMounted(() => { clockTimer = setInterval(() => { clock.value = Date.now() }, 1000) })
onBeforeUnmount(() => clearInterval(clockTimer))
</script>
