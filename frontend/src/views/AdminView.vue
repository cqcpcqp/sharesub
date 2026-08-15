<template>
  <section class="view-content admin-view" aria-label="后台管理">
    <header class="admin-header">
      <div><h1>后台管理</h1><p>平台资源、运行状况与访问控制</p></div>
      <NButton secondary size="small" :loading="loading" @click="loadAll"><template #icon><RefreshCw :size="15" /></template>刷新</NButton>
    </header>

    <div v-if="overview" class="admin-overview-grid">
      <article><UsersRound :size="19" /><div><small>用户</small><strong>{{ overview.user_count }}</strong><span>{{ overview.active_user_count }} 正常</span></div></article>
      <article><Bot :size="19" /><div><small>OpenAI 账号</small><strong>{{ overview.account_count }}</strong><span>{{ overview.active_accounts }} 可调度</span></div></article>
      <article><Layers3 :size="19" /><div><small>Plan</small><strong>{{ overview.plan_count }}</strong><span>{{ overview.active_plans }} 正常</span></div></article>
      <article><KeyRound :size="19" /><div><small>API Key</small><strong>{{ overview.api_key_count }}</strong><span>{{ overview.active_api_keys }} 有效</span></div></article>
      <article><Activity :size="19" /><div><small>24h 请求</small><strong>{{ formatNumber(overview.requests_24h) }}</strong><span>{{ formatPercent(overview.success_rate_24h) }} 成功</span></div></article>
      <article><Coins :size="19" /><div><small>24h Token / 费用</small><strong>{{ formatTokens(overview.tokens_24h) }}</strong><span>{{ formatUSD(overview.cost_micros_24h) }}</span></div></article>
    </div>

    <section v-if="overview" class="runtime-panel" aria-labelledby="runtime-heading">
      <header class="runtime-heading">
        <div><h2 id="runtime-heading">运行状态</h2><p><span class="runtime-live-dot" aria-hidden="true" />采集于 {{ formatRuntimeTime(overview.runtime.collected_at) }}</p></div>
        <small>容器与服务实时快照</small>
      </header>
      <div class="runtime-grid">
        <article title="API 容器相对于 CPU 限额的使用率">
          <span>CPU</span>
          <strong :class="runtimeStatusClass(overview.runtime.cpu.status)">{{ formatRuntimePercent(overview.runtime.cpu.status, overview.runtime.cpu.usage_percent) }}</strong>
          <small>警告 80% · 严重 95%</small>
        </article>
        <article title="API 容器当前内存用量与容器限制">
          <span>内存</span>
          <strong :class="runtimeStatusClass(overview.runtime.memory.status)">{{ formatRuntimePercent(overview.runtime.memory.status, overview.runtime.memory.usage_percent) }}</strong>
          <small>{{ formatBytes(overview.runtime.memory.used_bytes) }} / {{ formatBytes(overview.runtime.memory.total_bytes) }}</small>
        </article>
        <article title="PostgreSQL 连通性与连接池状态">
          <span>数据库</span>
          <strong :class="runtimeStatusClass(overview.runtime.database.status)">{{ runtimeStatusLabel(overview.runtime.database.status) }}</strong>
          <small>连接 {{ overview.runtime.database.open_connections }} / {{ overview.runtime.database.max_connections }} · 活跃 {{ overview.runtime.database.active_connections }} · 空闲 {{ overview.runtime.database.idle_connections }} · 等待累计 {{ overview.runtime.database.waiting_requests }}</small>
        </article>
        <article title="当前 Go 进程的 goroutine 数量">
          <span>协程</span>
          <strong :class="runtimeStatusClass(overview.runtime.goroutines.status)">{{ runtimeStatusLabel(overview.runtime.goroutines.status) }}</strong>
          <small>当前 {{ formatNumber(overview.runtime.goroutines.count) }} · 警告 8,000 · 严重 15,000</small>
        </article>
        <article title="服务内定时任务的最近执行状态">
          <div class="runtime-card-heading"><span>后台任务</span><NButton text size="tiny" @click="jobsVisible = true">详情</NButton></div>
          <strong :class="runtimeStatusClass(overview.runtime.jobs_status)">{{ runtimeStatusLabel(overview.runtime.jobs_status) }}</strong>
          <small>总计 {{ overview.runtime.jobs.length }} · 警告 {{ warningJobCount }}</small>
        </article>
      </div>
    </section>

    <div class="admin-resource-panel">
      <div class="admin-resource-toolbar">
        <NTabs v-model:value="activeTab" type="segment" size="small">
          <NTab name="users">用户 {{ users.length }}</NTab>
          <NTab name="accounts">账号 {{ accounts.length }}</NTab>
          <NTab name="plans">Plans {{ plans.length }}</NTab>
          <NTab name="keys">API Keys {{ keys.length }}</NTab>
        </NTabs>
        <NInput v-model:value="query" clearable size="small" placeholder="搜索当前列表" aria-label="搜索后台资源"><template #prefix><Search :size="14" /></template></NInput>
      </div>

      <div v-if="loading && !overview" class="admin-loading"><NSpin size="small" /></div>
      <div v-else ref="tableScroll" class="admin-table-scroll" @scroll="rememberScroll">
        <table v-if="activeTab === 'users'" class="admin-table">
          <thead><tr><th>用户</th><th>状态</th><th>账号</th><th>Plan</th><th>Key</th><th>注册时间</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in filteredUsers" :key="item.id">
            <td><div class="admin-primary"><strong>{{ item.username }}<em v-if="item.is_admin">管理员</em></strong><small>{{ item.email }}</small></div></td>
            <td><StatusBadge :value="item.status" /></td><td>{{ item.account_count }}</td><td>{{ item.plan_count }}</td><td>{{ item.api_key_count }}</td><td>{{ formatDate(item.created_at) }}</td>
            <td><NPopconfirm :disabled="item.id === currentUser.id" :positive-text="item.status === 'active' ? '禁用用户' : '恢复用户'" negative-text="取消" @positive-click="toggleUser(item)"><template #trigger><NButton size="tiny" secondary :type="item.status === 'active' ? 'error' : 'primary'" :disabled="item.id === currentUser.id" :loading="actionID === item.id">{{ item.id === currentUser.id ? '当前账号' : item.status === 'active' ? '禁用' : '恢复' }}</NButton></template>{{ item.status === 'active' ? '禁用后该用户的所有登录会话将立即失效。' : '恢复后该用户可以重新登录。' }}</NPopconfirm></td>
          </tr></tbody>
        </table>

        <table v-else-if="activeTab === 'accounts'" class="admin-table">
          <thead><tr><th>OpenAI 账号</th><th>所有者</th><th>套餐</th><th>状态</th><th>绑定 Plan</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in filteredAccounts" :key="item.id">
            <td><div class="admin-primary"><strong>{{ item.name }}</strong><small>{{ item.email }}</small></div></td><td><div class="admin-primary"><strong>{{ item.owner_username }}</strong><small>{{ item.owner_email }}</small></div></td><td>{{ item.plan_type }}</td><td><StatusBadge :value="item.status" /></td><td>{{ item.plan_name || '未绑定' }}</td>
            <td><div class="admin-row-actions"><NButton size="tiny" secondary :disabled="refreshingAccountID === item.id || reauthorizeStartingID === item.id" @click="openAccountEditor(item)"><template #icon><Pencil :size="13" /></template>编辑</NButton><NButton size="tiny" quaternary :disabled="item.status !== 'active' || actionID === item.id || reauthorizeStartingID === item.id" :loading="refreshingAccountID === item.id" @click="refreshAccountToken(item)"><template #icon><RefreshCw :size="13" /></template>刷新令牌</NButton><NButton size="tiny" quaternary :disabled="refreshingAccountID === item.id || actionID === item.id" :loading="reauthorizeStartingID === item.id" @click="startReauthorize(item)"><template #icon><RotateCw :size="13" /></template>重新授权</NButton><NPopconfirm :positive-text="item.status === 'active' ? '禁用账号' : '启用账号'" negative-text="取消" @positive-click="toggleAccount(item)"><template #trigger><NButton size="tiny" secondary :type="item.status === 'active' ? 'error' : 'primary'" :disabled="refreshingAccountID === item.id || reauthorizeStartingID === item.id" :loading="actionID === item.id">{{ item.status === 'active' ? '禁用' : '启用' }}</NButton></template>{{ item.status === 'active' ? '禁用后网关将停止向该账号调度请求。' : '启用后账号将重新参与网关调度。' }}</NPopconfirm></div></td>
          </tr></tbody>
        </table>

        <table v-else-if="activeTab === 'plans'" class="admin-table">
          <thead><tr><th>Plan</th><th>所有者</th><th>账号</th><th>状态</th><th>成员</th><th>24h 请求</th><th>24h Token</th><th>创建时间</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in filteredPlans" :key="item.id"><td><div class="admin-primary"><strong>{{ item.name }}</strong><small>{{ item.allocation_mode === 'shared' ? '共享额度' : '固定份额' }} · {{ item.visibility === 'public' ? '公开' : '私密' }}</small></div></td><td>{{ item.owner_username }}</td><td>{{ item.account_email || '未绑定' }}</td><td><StatusBadge :value="item.status" /></td><td>{{ item.member_count }}</td><td>{{ formatNumber(item.requests_24h) }}</td><td>{{ formatTokens(item.total_tokens_24h) }}</td><td>{{ formatDate(item.created_at) }}</td><td><NButton size="tiny" secondary @click="emit('openPlan', item.id)"><template #icon><ArrowRight :size="13" /></template>查看详情</NButton></td></tr></tbody>
        </table>

        <table v-else class="admin-table">
          <thead><tr><th>API Key</th><th>用户</th><th>状态</th><th>策略</th><th>路由</th><th>最近使用</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in filteredKeys" :key="item.id"><td><div class="admin-primary"><strong>{{ item.name }}</strong><small><code>{{ item.key_prefix }}...</code></small></div></td><td><div class="admin-primary"><strong>{{ item.username }}</strong><small>{{ item.email }}</small></div></td><td><StatusBadge :value="item.status" /></td><td>{{ item.strategy === 'balanced' ? '额度均衡' : '优先级' }}</td><td>{{ item.route_count }}</td><td>{{ item.last_used_at ? formatDate(item.last_used_at) : '尚未使用' }}</td><td><NPopconfirm positive-text="吊销 Key" negative-text="取消" @positive-click="revokeKey(item)"><template #trigger><NButton size="tiny" secondary type="error" :disabled="item.status !== 'active'" :loading="actionID === item.id">吊销</NButton></template>吊销后无法恢复，使用该 Key 的客户端会立即停止访问。</NPopconfirm></td></tr></tbody>
        </table>
      </div>
      <NEmpty v-if="!loading && currentCount === 0" description="没有匹配的记录" />
    </div>
  </section>

  <AccountConfigDialog
    :account="editingAccount"
    :subtitle="accountDialogSubtitle"
    :saving="savingAccount"
    :policy-user-options="policyUserOptions"
    @close="closeAccountEditor"
    @save="saveAccount"
    @reauthorize="startReauthorizeFromEditor"
  />

  <ModalShell
    v-if="reauthorizing && reauthorizeAccount"
    title="重新授权 OpenAI 账号"
    :subtitle="`${reauthorizeAccount.email} · 所有者 ${reauthorizeAccount.owner_username}`"
    @close="returnToAccountEditor"
  >
    <NAlert type="info" :show-icon="true">请登录同一个 OpenAI 账号。授权到其他账号时，系统会拒绝替换现有凭据。</NAlert>
    <div class="oauth-step">
      <span>1</span>
      <div><strong>刷新 OpenAI 授权</strong><small>完成授权后，将地址栏中的完整回调地址粘贴到下方。</small></div>
      <NButton tag="a" type="primary" :href="reauthorizing.authorization_url" target="_blank" rel="noreferrer">
        <template #icon><ExternalLink :size="16" /></template>
        打开授权
      </NButton>
    </div>
    <label>回调地址<AppInput :value="callbackURL" clearable placeholder="http://localhost:1455/auth/callback?..." @update:value="callbackURL = $event" /></label>
    <template #footer>
      <NButton @click="returnToAccountEditor">返回编辑</NButton>
      <NButton type="primary" :loading="reauthorizeCompleting" :disabled="!callbackURL.trim()" @click="completeReauthorize">
        <template #icon><RotateCw :size="16" /></template>
        完成重新授权
      </NButton>
    </template>
  </ModalShell>

  <ModalShell v-if="jobsVisible && overview" title="后台任务" subtitle="最近一次执行结果与耗时" wide @close="jobsVisible = false">
    <div class="runtime-job-list">
      <article v-for="job in overview.runtime.jobs" :key="job.id">
        <div class="runtime-job-heading"><div><strong>{{ job.name }}</strong><small>{{ job.id }}</small></div><span class="runtime-job-status" :class="runtimeStatusClass(job.status)">{{ runtimeStatusLabel(job.status) }}</span></div>
        <dl>
          <div><dt>最近运行</dt><dd>{{ formatOptionalRuntimeTime(job.last_run_at) }}</dd></div>
          <div><dt>最近成功</dt><dd>{{ formatOptionalRuntimeTime(job.last_success_at) }}</dd></div>
          <div><dt>耗时</dt><dd>{{ job.last_run_at ? `${job.last_duration_ms} ms` : '尚未运行' }}</dd></div>
          <div><dt>结果</dt><dd>{{ job.last_result || '—' }}</dd></div>
        </dl>
        <p v-if="job.last_error" class="runtime-job-error">{{ job.last_error }}</p>
      </article>
    </div>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NEmpty, NInput, NPopconfirm, NSpin, NTab, NTabs } from 'naive-ui'
import { Activity, ArrowRight, Bot, Coins, ExternalLink, KeyRound, Layers3, Pencil, RefreshCw, RotateCw, Search, UsersRound } from 'lucide-vue-next'
import { api, parseOAuthCallback } from '../api'
import { adminAPI } from '../api/admin'
import type { Account, AccountConfigInput, AdminAPIKey, AdminAccount, AdminOverview, AdminPlan, AdminUser, Member, OAuthStart, RuntimeStatus, User } from '../types'
import AccountConfigDialog from '../components/AccountConfigDialog.vue'
import AppInput from '../components/AppInput.vue'
import ModalShell from '../components/ModalShell.vue'
import { formatPercent, formatTokens } from '../dashboardFormat'
import StatusBadge from '../components/StatusBadge.vue'

const props = withDefaults(defineProps<{ currentUser: User; initialAccountId?: string }>(), { initialAccountId: '' })
const emit = defineEmits<{ message: [type: 'success' | 'error', text: string]; openPlan: [id: string]; openAccount: [id: string]; closeAccount: [] }>()
const activeTab = defineModel<'users' | 'accounts' | 'plans' | 'keys'>('activeTab', { default: 'users' })
const query = defineModel<string>('query', { default: '' })
const scrollTop = defineModel<number>('scrollTop', { default: 0 })
const overview = ref<AdminOverview | null>(null)
const users = ref<AdminUser[]>([])
const accounts = ref<AdminAccount[]>([])
const plans = ref<AdminPlan[]>([])
const keys = ref<AdminAPIKey[]>([])
const tableScroll = ref<HTMLElement | null>(null)
const loading = ref(false)
const actionID = ref('')
const editingAccount = ref<AdminAccount | null>(null)
const policyMembers = ref<Member[]>([])
const savingAccount = ref(false)
const reauthorizing = ref<OAuthStart | null>(null)
const reauthorizeAccount = ref<AdminAccount | null>(null)
const callbackURL = ref('')
const reauthorizeStartingID = ref('')
const refreshingAccountID = ref('')
const reauthorizeCompleting = ref(false)
const jobsVisible = ref(false)
const normalizedQuery = computed(() => query.value.trim().toLowerCase())
const matches = (...values: string[]) => !normalizedQuery.value || values.some(value => value.toLowerCase().includes(normalizedQuery.value))
const filteredUsers = computed(() => users.value.filter(item => matches(item.username, item.email, item.id)))
const filteredAccounts = computed(() => accounts.value.filter(item => matches(item.name, item.email, item.owner_username, item.owner_email, item.plan_name)))
const filteredPlans = computed(() => plans.value.filter(item => matches(item.name, item.owner_username, item.account_email, item.id)))
const filteredKeys = computed(() => keys.value.filter(item => matches(item.name, item.key_prefix, item.username, item.email)))
const currentCount = computed(() => ({ users: filteredUsers.value.length, accounts: filteredAccounts.value.length, plans: filteredPlans.value.length, keys: filteredKeys.value.length })[activeTab.value ?? 'users'])
const warningJobCount = computed(() => overview.value?.runtime.jobs.filter(job => job.status === 'warning' || job.status === 'critical').length ?? 0)
const policyUserOptions = computed(() => policyMembers.value.map(member => ({ label: member.email ? `${member.username} · ${member.email}` : member.username, value: member.user_id })))
const accountDialogSubtitle = computed(() => editingAccount.value ? `${editingAccount.value.email} · 所有者 ${editingAccount.value.owner_username}` : '')
const numberFormatter = new Intl.NumberFormat('zh-CN')
const usdFormatter = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 4 })
const dateFormatter = new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
const runtimeTimeFormatter = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
const runtimeStatusLabels: Record<RuntimeStatus, string> = { healthy: '正常', warning: '警告', critical: '严重', pending: '待运行', disabled: '已停用', unavailable: '不可用' }

function formatNumber(value: number) { return numberFormatter.format(value) }
function formatUSD(value: number) { return usdFormatter.format(value / 1_000_000) }
function formatDate(value: string) { return dateFormatter.format(new Date(value)) }
function formatRuntimeTime(value: string) { return runtimeTimeFormatter.format(new Date(value)) }
function formatOptionalRuntimeTime(value: string | null) { return value ? dateFormatter.format(new Date(value)) : '尚未运行' }
function runtimeStatusLabel(status: RuntimeStatus) { return runtimeStatusLabels[status] }
function runtimeStatusClass(status: RuntimeStatus) { return `runtime-status-${status}` }
function formatRuntimePercent(status: RuntimeStatus, value: number) { return status === 'unavailable' ? runtimeStatusLabel(status) : `${value.toFixed(1)}%` }
function formatBytes(value: number) {
  if (value === 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index >= 3 ? 1 : 0)} ${units[index]}`
}
function rememberScroll(event: Event) { scrollTop.value = (event.currentTarget as HTMLElement).scrollTop }
async function loadAll() {
  if (loading.value) return
  loading.value = true
  try { [overview.value, users.value, accounts.value, plans.value, keys.value] = await Promise.all([api.adminOverview(), api.adminUsers(), api.adminAccounts(), api.adminPlans(), api.adminKeys()]) }
  catch (error) { emit('message', 'error', error instanceof Error ? error.message : String(error)) }
  finally { loading.value = false }
}
async function toggleUser(item: AdminUser) {
  actionID.value = item.id
  try { const updated = await api.adminUpdateUserStatus(item.id, item.status === 'active' ? 'disabled' : 'active'); Object.assign(item, updated); overview.value = await api.adminOverview(); emit('message', 'success', '用户状态已更新') }
  catch (error) { emit('message', 'error', error instanceof Error ? error.message : String(error)) }
  finally { actionID.value = '' }
}
async function toggleAccount(item: AdminAccount) {
  actionID.value = item.id
  try { Object.assign(item, await api.adminUpdateAccountStatus(item.id, item.status === 'active' ? 'disabled' : 'active')); overview.value = await api.adminOverview(); emit('message', 'success', '账号状态已更新') }
  catch (error) { emit('message', 'error', error instanceof Error ? error.message : String(error)) }
  finally { actionID.value = '' }
}
async function refreshAccountToken(item: AdminAccount) {
  if (refreshingAccountID.value || reauthorizeStartingID.value === item.id || actionID.value === item.id || item.status !== 'active') return
  refreshingAccountID.value = item.id
  try {
    Object.assign(item, await adminAPI.adminRefreshAccountToken(item.id))
    emit('message', 'success', 'OpenAI 账号令牌已刷新')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    refreshingAccountID.value = ''
  }
}
async function openAccountEditor(account: AdminAccount) {
  emit('openAccount', account.id)
  editingAccount.value = account
  policyMembers.value = []
  if (!account.plan_id) return
  try {
    const members = (await adminAPI.adminPlan(account.plan_id)).members
    if (editingAccount.value?.id === account.id) policyMembers.value = members
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  }
}
function closeAccountEditor() {
  editingAccount.value = null
  policyMembers.value = []
  emit('closeAccount')
}
async function saveAccount(config: AccountConfigInput) {
  if (!editingAccount.value) return
  savingAccount.value = true
  try {
    const updated = await adminAPI.adminUpdateAccount(editingAccount.value.id, config)
    const index = accounts.value.findIndex(account => account.id === updated.id)
    if (index >= 0) accounts.value[index] = updated
    overview.value = await api.adminOverview()
    emit('message', 'success', 'OpenAI 账号配置已更新')
    closeAccountEditor()
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    savingAccount.value = false
  }
}
function startReauthorizeFromEditor(account: Account) {
  const adminAccount = accounts.value.find(candidate => candidate.id === account.id)
  if (adminAccount) void startReauthorize(adminAccount)
}
async function startReauthorize(account: AdminAccount) {
  if (refreshingAccountID.value === account.id || reauthorizeStartingID.value || actionID.value === account.id) return
  emit('openAccount', account.id)
  reauthorizeStartingID.value = account.id
  editingAccount.value = null
  reauthorizeAccount.value = account
  callbackURL.value = ''
  try {
    reauthorizing.value = await adminAPI.adminOAuthReauthorizeStart(account.id)
  } catch (error) {
    reauthorizeAccount.value = null
    editingAccount.value = account
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    reauthorizeStartingID.value = ''
  }
}
function returnToAccountEditor() {
  const account = reauthorizeAccount.value
  reauthorizing.value = null
  reauthorizeAccount.value = null
  callbackURL.value = ''
  if (account) void openAccountEditor(account)
}
async function completeReauthorize() {
  if (!reauthorizeAccount.value) return
  reauthorizeCompleting.value = true
  try {
    const callback = parseOAuthCallback(callbackURL.value.trim())
    const accountID = reauthorizeAccount.value.id
    await adminAPI.adminOAuthReauthorizeComplete(accountID, callback.state, callback.code)
    const updated = await adminAPI.adminAccount(accountID)
    const index = accounts.value.findIndex(account => account.id === accountID)
    if (index >= 0) accounts.value[index] = updated
    reauthorizing.value = null
    reauthorizeAccount.value = null
    callbackURL.value = ''
    emit('message', 'success', 'OpenAI 账号已重新授权')
    emit('closeAccount')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    reauthorizeCompleting.value = false
  }
}
async function revokeKey(item: AdminAPIKey) {
  actionID.value = item.id
  try { await api.adminRevokeKey(item.id); item.status = 'revoked'; overview.value = await api.adminOverview(); emit('message', 'success', 'API Key 已吊销') }
  catch (error) { emit('message', 'error', error instanceof Error ? error.message : String(error)) }
  finally { actionID.value = '' }
}
onMounted(async () => {
  await loadAll()
  await nextTick()
  if (tableScroll.value) tableScroll.value.scrollTop = scrollTop.value
})
watch([() => props.initialAccountId, () => accounts.value.map(account => account.id).join(',')], ([accountID]) => {
  if (!accountID) return
  activeTab.value = 'accounts'
  const account = accounts.value.find(candidate => candidate.id === accountID)
  if (account && editingAccount.value?.id !== accountID && reauthorizeAccount.value?.id !== accountID) void openAccountEditor(account)
}, { immediate: true })
</script>

<style scoped>
.admin-view { display: grid; gap: 16px; }
.admin-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.admin-header h1 { margin: 4px 0 0; color: var(--ink-strong); font-size: 20px; }
.admin-header p { margin: 5px 0 0; color: var(--muted); font-size: 11px; }
.admin-overview-grid { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; }
.admin-overview-grid article { min-width: 0; display: flex; gap: 10px; padding: 14px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); color: var(--primary); box-shadow: var(--shadow-xs); }
.admin-overview-grid article div { min-width: 0; display: grid; gap: 3px; }
.admin-overview-grid small,.admin-overview-grid span { color: var(--muted); font-size: 11px; }
.admin-overview-grid strong { overflow: hidden; color: var(--ink-strong); font-size: 18px; text-overflow: ellipsis; }
.runtime-panel { overflow: hidden; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-xs); }
.runtime-heading { min-height: 58px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 11px 14px; border-bottom: 1px solid var(--line-soft); }
.runtime-heading h2 { margin: 0; color: var(--ink-strong); font-size: 13px; }
.runtime-heading p { display: flex; align-items: center; gap: 6px; margin: 4px 0 0; color: var(--muted); font-size: 10px; font-variant-numeric: tabular-nums; }
.runtime-heading > small { color: var(--muted); font-size: 10px; }
.runtime-live-dot { width: 6px; height: 6px; flex: 0 0 auto; border-radius: 999px; background: var(--teal); }
.runtime-grid { display: grid; grid-template-columns: .8fr 1fr 1.55fr 1.3fr 1fr; }
.runtime-grid > article { min-width: 0; min-height: 102px; display: grid; align-content: center; gap: 7px; padding: 14px 15px; border-right: 1px solid var(--line-soft); background: var(--surface); }
.runtime-grid > article:last-child { border-right: 0; }
.runtime-grid span,.runtime-card-heading { color: var(--muted); font-size: 10px; font-weight: 760; letter-spacing: .06em; text-transform: uppercase; }
.runtime-grid strong { overflow: hidden; font-size: 19px; font-variant-numeric: tabular-nums; line-height: 1.1; text-overflow: ellipsis; }
.runtime-grid small { overflow: hidden; color: var(--muted); font-size: 10px; font-variant-numeric: tabular-nums; line-height: 1.5; text-overflow: ellipsis; }
.runtime-card-heading { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.runtime-card-heading .n-button { min-width: 36px; min-height: 36px; padding: 0 8px; color: var(--blue); font-size: 10px; font-weight: 750; letter-spacing: 0; text-transform: none; }
.runtime-status-healthy { color: var(--teal); }
.runtime-status-warning,.runtime-status-pending { color: var(--amber); }
.runtime-status-critical { color: var(--red); }
.runtime-status-disabled,.runtime-status-unavailable { color: var(--muted); }
.runtime-job-list { display: grid; gap: 10px; }
.runtime-job-list > article { padding: 14px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-soft); }
.runtime-job-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.runtime-job-heading > div { min-width: 0; display: grid; gap: 3px; }
.runtime-job-heading strong { color: var(--ink-strong); font-size: 12px; }
.runtime-job-heading small { color: var(--muted); font-family: "SFMono-Regular", Consolas, monospace; font-size: 10px; }
.runtime-job-status { padding: 3px 8px; border-radius: 999px; background: var(--surface); font-size: 10px; font-weight: 750; }
.runtime-job-status.runtime-status-warning,.runtime-job-status.runtime-status-pending { background: var(--amber-soft); color: var(--ink); }
.runtime-job-list dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 9px 18px; margin: 13px 0 0; }
.runtime-job-list dl div { min-width: 0; display: grid; gap: 3px; }
.runtime-job-list dt { color: var(--muted); font-size: 10px; }
.runtime-job-list dd { overflow-wrap: anywhere; margin: 0; color: var(--ink); font-size: 11px; font-variant-numeric: tabular-nums; }
.runtime-job-error { margin: 12px 0 0; padding: 9px 10px; border: 1px solid var(--danger-border); border-radius: 7px; background: var(--red-soft); color: var(--danger-ink); font-size: 11px; line-height: 1.55; }
.admin-resource-panel { min-width: 0; overflow: hidden; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); box-shadow: var(--shadow-xs); }
.admin-resource-toolbar { display: grid; grid-template-columns: minmax(420px, 1fr) 240px; gap: 14px; align-items: center; padding: 12px 14px; border-bottom: 1px solid var(--line-soft); }
.admin-table-scroll { min-width: 0; overflow: auto; }
.admin-table { width: 100%; min-width: 920px; border-collapse: collapse; }
.admin-table th { height: 38px; padding: 0 13px; background: var(--surface-soft); color: var(--muted); font-size: 11px; font-weight: 800; text-align: left; white-space: nowrap; }
.admin-table td { height: 58px; padding: 9px 13px; border-top: 1px solid var(--line-soft); color: var(--ink); font-size: 11px; white-space: nowrap; }
.admin-table tbody tr:hover { background: var(--surface-hover); }
.admin-primary { min-width: 140px; display: grid; gap: 4px; }
.admin-primary strong { color: var(--ink-strong); font-size: 11px; }
.admin-primary small { max-width: 240px; overflow: hidden; color: var(--muted); font-size: 11px; text-overflow: ellipsis; }
.admin-primary em { margin-left: 6px; padding: 2px 5px; border-radius: 4px; background: var(--primary-soft); color: var(--primary); font-size: 11px; font-style: normal; }
.admin-row-actions { display: flex; gap: 6px; }
.admin-loading { min-height: 240px; display: grid; place-items: center; }
:deep(.n-empty) { padding: 48px 20px; }
@media (max-width: 1200px) { .admin-overview-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 1200px) { .runtime-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } .runtime-grid > article { border-bottom: 1px solid var(--line-soft); } .runtime-grid > article:nth-child(3n) { border-right: 0; } .runtime-grid > article:nth-last-child(-n+2) { border-bottom: 0; } }
@media (max-width: 720px) { .admin-overview-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .admin-resource-toolbar { grid-template-columns: 1fr; } .runtime-heading > small { display: none; } .runtime-grid { grid-template-columns: 1fr; } .runtime-grid > article { min-height: 92px; border-right: 0; border-bottom: 1px solid var(--line-soft); } .runtime-grid > article:nth-last-child(-n+2) { border-bottom: 1px solid var(--line-soft); } .runtime-grid > article:last-child { border-bottom: 0; } .runtime-job-list dl { grid-template-columns: 1fr; } }
</style>
