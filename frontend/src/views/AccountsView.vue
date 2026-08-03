<template>
  <section class="view-content">
    <div class="collection-toolbar">
      <p><strong>{{ accounts.length }}</strong><span>个已接入账号</span></p>
      <NButton type="primary" :loading="oauthStarting" @click="startOAuth">
        <template #icon><Plus :size="17" /></template>
        接入账号
      </NButton>
    </div>

    <div v-if="accounts.length" class="account-grid">
      <article v-for="account in accounts" :key="account.id" class="account-card">
        <header>
          <span class="account-logo"><Bot :size="22" /></span>
          <div class="row-actions">
            <StatusBadge :value="account.status" />
            <NButton
              quaternary
              class="icon-button"
              title="重新授权"
              aria-label="重新授权"
              :loading="reauthorizeStartingID === account.id"
              @click="startReauthorize(account)"
            >
              <template #icon><RotateCw :size="17" /></template>
            </NButton>
            <NButton
              quaternary
              class="icon-button"
              title="编辑账号"
              aria-label="编辑账号"
              @click="openEdit(account)"
            >
              <template #icon><Pencil :size="17" /></template>
            </NButton>
          </div>
        </header>
        <div class="account-identity">
          <strong>{{ account.name }}</strong>
          <span>{{ account.email }} · {{ account.plan_type }}</span>
          <p v-if="account.notes">{{ account.notes }}</p>
        </div>
        <dl>
          <div>
            <dt><Gauge :size="14" />最大并发</dt>
            <dd>{{ limitLabel(account.max_concurrency, '请求') }}</dd>
          </div>
          <div>
            <dt><TimerReset :size="14" />RPM 上限</dt>
            <dd>{{ limitLabel(account.rpm_limit, '次/分钟') }}</dd>
          </div>
          <div>
            <dt><Network :size="14" />账号代理</dt>
            <dd>{{ account.proxy_url || '继承系统代理' }}</dd>
          </div>
          <div>
            <dt><CalendarClock :size="14" />Token 到期</dt>
            <dd>{{ formatDate(account.token_expires_at) }}</dd>
          </div>
        </dl>
        <NAlert v-if="account.last_error" type="warning" :show-icon="true" class="account-error">
          {{ account.last_error }}
        </NAlert>
        <footer>
          <span>ShareSub ID</span>
          <code>{{ shortID(account.id) }}</code>
        </footer>
      </article>
    </div>
    <div v-else class="data-surface">
      <EmptyState title="还没有 OpenAI 账号" description="接入账号后才能创建共享 Plan。" :icon="Bot" />
    </div>
  </section>

  <ModalShell
    v-if="oauth"
    title="接入 OpenAI"
    subtitle="授权并配置账号的网关策略"
    :wide="true"
    @close="closeOAuth"
  >
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
    <AccountConfigFields v-model="createConfig" />
    <template #footer>
      <NButton @click="closeOAuth">取消</NButton>
      <NButton type="primary" :loading="completing" :disabled="!callback.trim() || !createConfig.name.trim()" @click="completeOAuth">
        <template #icon><Check :size="17" /></template>
        完成接入
      </NButton>
    </template>
  </ModalShell>

  <ModalShell
    v-if="reauthorizing && reauthorizeAccount"
    title="重新授权 OpenAI 账号"
    :subtitle="reauthorizeAccount.email"
    @close="closeReauthorize"
  >
    <NAlert type="info" :show-icon="true">
      请登录同一个 OpenAI 账号完成授权。若授权到其他账号，系统会拒绝替换现有凭据。
    </NAlert>
    <div class="oauth-step">
      <span>1</span>
      <div>
        <strong>刷新 OpenAI 授权</strong>
        <small>完成授权后，将地址栏中的完整回调地址粘贴到下方。</small>
      </div>
      <NButton tag="a" type="primary" :href="reauthorizing.authorization_url" target="_blank" rel="noreferrer">
        <template #icon><ExternalLink :size="17" /></template>
        打开授权
      </NButton>
    </div>
    <label>
      回调地址
      <AppInput :value="reauthorizeCallback" clearable placeholder="http://localhost:1455/auth/callback?..." @update:value="updateReauthorizeCallback" />
    </label>
    <template #footer>
      <NButton @click="closeReauthorize">取消</NButton>
      <NButton type="primary" :loading="reauthorizeCompleting" :disabled="!reauthorizeCallback.trim()" @click="completeReauthorize">
        <template #icon><RotateCw :size="17" /></template>
        完成重新授权
      </NButton>
    </template>
  </ModalShell>

  <ModalShell
    v-if="editing"
    title="编辑 OpenAI 账号"
    :subtitle="editing.email"
    :wide="true"
    @close="editing = null"
  >
    <AccountConfigFields v-model="editConfig" :show-status="true" :policy-user-options="policyUserOptions" />
    <template #footer>
      <NButton @click="editing = null">取消</NButton>
      <NButton type="primary" :loading="saving" :disabled="!editConfig.name.trim()" @click="saveEdit">
        <template #icon><Save :size="17" /></template>
        保存配置
      </NButton>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import { NAlert, NButton } from 'naive-ui'
import { computed, ref } from 'vue'
import { Bot, CalendarClock, Check, ExternalLink, Gauge, Network, Pencil, Plus, RotateCw, Save, TimerReset } from 'lucide-vue-next'
import { api, parseOAuthCallback } from '../api'
import type { Account, AccountConfigInput, Member, OAuthStart, Plan } from '../types'
import AccountConfigFields from '../components/AccountConfigFields.vue'
import AppInput from '../components/AppInput.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalShell from '../components/ModalShell.vue'
import StatusBadge from '../components/StatusBadge.vue'

const props = withDefaults(defineProps<{ accounts: Account[]; plans?: Plan[] }>(), { plans: () => [] })
const accounts = computed(() => props.accounts)
const emit = defineEmits<{ changed: []; message: [type: 'success' | 'error', text: string] }>()
const oauth = ref<OAuthStart | null>(null)
const callback = ref('')
const editing = ref<Account | null>(null)
const reauthorizing = ref<OAuthStart | null>(null)
const reauthorizeAccount = ref<Account | null>(null)
const reauthorizeCallback = ref('')
const reauthorizeStartingID = ref('')
const reauthorizeCompleting = ref(false)
const oauthStarting = ref(false)
const completing = ref(false)
const saving = ref(false)
const createConfig = ref<AccountConfigInput>(emptyConfig())
const editConfig = ref<AccountConfigInput>(emptyConfig())
const policyMembers = ref<Member[]>([])
const policyUserOptions = computed(() => policyMembers.value.map(member => ({ label: member.email ? `${member.username} · ${member.email}` : member.username, value: member.user_id })))

function updateCallback(value: string) { callback.value = value }
function updateReauthorizeCallback(value: string) { reauthorizeCallback.value = value }

async function startOAuth() {
  oauthStarting.value = true
  try {
    createConfig.value = emptyConfig()
    oauth.value = await api.oauthStart()
  } catch (error) {
    notifyError(error)
  } finally {
    oauthStarting.value = false
  }
}

function closeOAuth() {
  oauth.value = null
  callback.value = ''
}

async function completeOAuth() {
  completing.value = true
  try {
    const params = parseOAuthCallback(callback.value.trim())
    await api.oauthComplete(params.state, params.code, { ...createConfig.value })
    closeOAuth()
    emit('message', 'success', 'OpenAI 账号已接入')
    emit('changed')
  } catch (error) {
    notifyError(error)
  } finally {
    completing.value = false
  }
}

async function startReauthorize(account: Account) {
  reauthorizeStartingID.value = account.id
  try {
    reauthorizeAccount.value = account
    reauthorizeCallback.value = ''
    reauthorizing.value = await api.oauthReauthorizeStart(account.id)
  } catch (error) {
    reauthorizeAccount.value = null
    notifyError(error)
  } finally {
    reauthorizeStartingID.value = ''
  }
}

function closeReauthorize() {
  reauthorizing.value = null
  reauthorizeAccount.value = null
  reauthorizeCallback.value = ''
}

async function completeReauthorize() {
  if (!reauthorizeAccount.value) return
  reauthorizeCompleting.value = true
  try {
    const params = parseOAuthCallback(reauthorizeCallback.value.trim())
    await api.oauthReauthorizeComplete(reauthorizeAccount.value.id, params.state, params.code)
    closeReauthorize()
    emit('message', 'success', 'OpenAI 账号已重新授权')
    emit('changed')
  } catch (error) {
    notifyError(error)
  } finally {
    reauthorizeCompleting.value = false
  }
}

async function openEdit(account: Account) {
  editing.value = account
  policyMembers.value = []
  editConfig.value = {
    name: account.name,
    notes: account.notes,
    proxy_url: account.proxy_url,
    max_concurrency: account.max_concurrency,
    rpm_limit: account.rpm_limit,
    fast_policy: account.fast_policy.map(rule => ({ ...rule, user_ids: [...rule.user_ids], model_whitelist: [...rule.model_whitelist] })),
    status: account.status,
  }
  const plan = props.plans.find(candidate => candidate.account_id === account.id)
  if (plan) {
    try {
      const members = (await api.plan(plan.id)).members
      if (editing.value?.id === account.id) policyMembers.value = members
    } catch (error) {
      notifyError(error)
    }
  }
}

async function saveEdit() {
  if (!editing.value) return
  saving.value = true
  try {
    await api.updateAccount(editing.value.id, { ...editConfig.value })
    editing.value = null
    emit('message', 'success', '账号配置已更新')
    emit('changed')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

function emptyConfig(): AccountConfigInput {
  return { name: '', notes: '', proxy_url: '', max_concurrency: 0, rpm_limit: 0, fast_policy: [], status: 'active' }
}

function notifyError(value: unknown) {
  emit('message', 'error', value instanceof Error ? value.message : String(value))
}

function shortID(value: string) { return value.slice(0, 10) }
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function limitLabel(value: number, suffix: string) { return value === 0 ? '不限制' : `${value} ${suffix}` }
</script>
