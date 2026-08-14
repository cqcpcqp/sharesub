<template>
  <section class="view-content">
    <div class="collection-toolbar">
      <p><strong>{{ accounts.length }}</strong><span>个已接入账号</span></p>
      <NButton type="primary" @click="showConnect = true">
        <template #icon><Plus :size="17" /></template>
        接入账号
      </NButton>
    </div>

    <div v-if="accounts.length" class="account-grid">
      <article v-for="account in accounts" :key="account.id" class="account-card">
        <header class="account-card-header">
          <div class="account-identity">
            <h3>{{ account.name }}</h3>
            <div class="account-meta">
              <span class="account-email">{{ account.email }}</span>
              <span class="account-plan-type">{{ account.plan_type }}</span>
            </div>
          </div>
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
        <p v-if="account.notes" class="account-notes">{{ account.notes }}</p>
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
            <dt><CalendarRange :size="14" />订阅有效期至</dt>
            <dd>{{ account.subscription_expires_at ? formatDate(account.subscription_expires_at) : '暂无订阅有效期' }}</dd>
          </div>
        </dl>
        <NAlert v-if="account.last_error" type="warning" :show-icon="true" class="account-error">
          {{ account.last_error }}
        </NAlert>
      </article>
    </div>
    <div v-else class="data-surface">
      <EmptyState title="还没有 OpenAI 账号" description="你可以先创建 Plan 探索协作设置，需要开始使用时再接入账号。" :icon="Bot" />
    </div>
  </section>

  <OpenAIAccountConnectDialog v-model:show="showConnect" @connected="handleConnected" @message="forwardError" />

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

  <AccountConfigDialog
    :account="editing"
    :saving="saving"
    :policy-user-options="policyUserOptions"
    @close="editing = null"
    @save="saveEdit"
    @reauthorize="startReauthorizeFromEditor"
  />
</template>

<script setup lang="ts">
import { NAlert, NButton } from 'naive-ui'
import { computed, ref } from 'vue'
import { Bot, CalendarRange, ExternalLink, Gauge, Network, Pencil, Plus, RotateCw, TimerReset } from 'lucide-vue-next'
import { api, parseOAuthCallback } from '../api'
import type { Account, AccountConfigInput, Member, OAuthStart, Plan } from '../types'
import AccountConfigDialog from '../components/AccountConfigDialog.vue'
import AppInput from '../components/AppInput.vue'
import EmptyState from '../components/EmptyState.vue'
import ModalShell from '../components/ModalShell.vue'
import OpenAIAccountConnectDialog from '../components/OpenAIAccountConnectDialog.vue'
import StatusBadge from '../components/StatusBadge.vue'

const props = withDefaults(defineProps<{ accounts: Account[]; plans?: Plan[] }>(), { plans: () => [] })
const accounts = computed(() => props.accounts)
const emit = defineEmits<{ changed: []; message: [type: 'success' | 'error', text: string] }>()
const showConnect = ref(false)
const editing = ref<Account | null>(null)
const reauthorizing = ref<OAuthStart | null>(null)
const reauthorizeAccount = ref<Account | null>(null)
const reauthorizeCallback = ref('')
const reauthorizeStartingID = ref('')
const reauthorizeCompleting = ref(false)
const saving = ref(false)
const policyMembers = ref<Member[]>([])
const policyUserOptions = computed(() => policyMembers.value.map(member => ({ label: member.email ? `${member.username} · ${member.email}` : member.username, value: member.user_id })))

function updateReauthorizeCallback(value: string) { reauthorizeCallback.value = value }

function startReauthorizeFromEditor(account: Account) {
  editing.value = null
  void startReauthorize(account)
}

function handleConnected() {
  emit('message', 'success', 'OpenAI 账号已接入')
  emit('changed')
}

function forwardError(_: 'error', text: string) { emit('message', 'error', text) }

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

async function saveEdit(config: AccountConfigInput) {
  if (!editing.value) return
  saving.value = true
  try {
    await api.updateAccount(editing.value.id, config)
    editing.value = null
    emit('message', 'success', '账号配置已更新')
    emit('changed')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

function notifyError(value: unknown) {
  emit('message', 'error', value instanceof Error ? value.message : String(value))
}

function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function limitLabel(value: number, suffix: string) { return value === 0 ? '不限制' : `${value} ${suffix}` }
</script>
