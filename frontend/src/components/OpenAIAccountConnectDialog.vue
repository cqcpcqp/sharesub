<template>
  <ModalShell
    v-if="show"
    title="接入 OpenAI"
    :subtitle="subtitle"
    :wide="true"
    @close="close"
  >
    <div v-if="starting" class="account-connect-loading" aria-label="正在准备 OpenAI 授权">
      <NSpin size="small" />
      <span>正在准备授权流程…</span>
    </div>

    <template v-else-if="oauth">
      <div class="oauth-step">
        <span>1</span>
        <div>
          <strong>完成 OpenAI 授权</strong>
          <small>授权完成后，将浏览器地址栏中的完整回调地址粘贴到下方。</small>
        </div>
        <NButton tag="a" type="primary" :href="oauth.authorization_url" target="_blank" rel="noreferrer">
          <template #icon><ExternalLink :size="17" /></template>
          打开授权
        </NButton>
      </div>
      <label>
        回调地址
        <AppInput :value="callback" clearable placeholder="http://localhost:1455/auth/callback?..." @update:value="updateCallback" />
      </label>
      <div class="oauth-step-heading">
        <span>2</span>
        <div><strong>设置账号配置</strong><small>之后可随时在账号页修改。</small></div>
      </div>
      <AccountConfigFields v-model="config" />
    </template>

    <template #footer>
      <NButton :disabled="completing" @click="close">取消</NButton>
      <NButton v-if="oauth" type="primary" :loading="completing" :disabled="!callback.trim() || !config.name.trim()" @click="complete">
        <template #icon><Check :size="17" /></template>
        完成接入
      </NButton>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import { NButton, NSpin } from 'naive-ui'
import { ref, watch } from 'vue'
import { Check, ExternalLink } from 'lucide-vue-next'
import { api, parseOAuthCallback } from '../api'
import type { Account, AccountConfigInput, OAuthStart } from '../types'
import AccountConfigFields from './AccountConfigFields.vue'
import AppInput from './AppInput.vue'
import ModalShell from './ModalShell.vue'

const props = withDefaults(defineProps<{ show: boolean; subtitle?: string }>(), {
  subtitle: '授权并配置账号的网关策略',
})
const emit = defineEmits<{
  'update:show': [show: boolean]
  connected: [account: Account]
  message: [type: 'error', text: string]
}>()

const oauth = ref<OAuthStart | null>(null)
const callback = ref('')
const config = ref<AccountConfigInput>(emptyConfig())
const starting = ref(false)
const completing = ref(false)
let startSequence = 0

watch(() => props.show, show => {
  if (show) void start()
  else reset()
}, { immediate: true })

async function start() {
  const sequence = ++startSequence
  resetForm()
  starting.value = true
  try {
    const value = await api.oauthStart()
    if (sequence === startSequence && props.show) oauth.value = value
  } catch (error) {
    if (sequence === startSequence) {
      notifyError(error)
      emit('update:show', false)
    }
  } finally {
    if (sequence === startSequence) starting.value = false
  }
}

async function complete() {
  if (!oauth.value) return
  completing.value = true
  try {
    const params = parseOAuthCallback(callback.value.trim())
    const account = await api.oauthComplete(params.state, params.code, { ...config.value })
    emit('connected', account)
    emit('update:show', false)
  } catch (error) {
    notifyError(error)
  } finally {
    completing.value = false
  }
}

function close() {
  if (!completing.value) emit('update:show', false)
}

function reset() {
  startSequence += 1
  starting.value = false
  completing.value = false
  resetForm()
}

function resetForm() {
  oauth.value = null
  callback.value = ''
  config.value = emptyConfig()
}

function updateCallback(value: string) { callback.value = value }
function emptyConfig(): AccountConfigInput {
  return { name: '', notes: '', proxy_url: '', max_concurrency: 0, rpm_limit: 0, fast_policy: [], status: 'active' }
}
function notifyError(value: unknown) {
  emit('message', 'error', value instanceof Error ? value.message : String(value))
}
</script>

<style scoped>
.account-connect-loading {
  min-height: 220px;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 12px;
  color: var(--muted);
  font-size: 12px;
}
</style>
