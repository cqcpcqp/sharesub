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
      <div v-else class="admin-table-scroll">
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
            <td><div class="admin-row-actions"><NButton size="tiny" secondary @click="openAccountEdit(item)"><template #icon><Pencil :size="13" /></template>编辑</NButton><NPopconfirm :positive-text="item.status === 'active' ? '禁用账号' : '启用账号'" negative-text="取消" @positive-click="toggleAccount(item)"><template #trigger><NButton size="tiny" secondary :type="item.status === 'active' ? 'error' : 'primary'" :loading="actionID === item.id">{{ item.status === 'active' ? '禁用' : '启用' }}</NButton></template>{{ item.status === 'active' ? '禁用后网关将停止向该账号调度请求。' : '启用后账号将重新参与网关调度。' }}</NPopconfirm></div></td>
          </tr></tbody>
        </table>

        <table v-else-if="activeTab === 'plans'" class="admin-table">
          <thead><tr><th>Plan</th><th>所有者</th><th>账号</th><th>状态</th><th>成员</th><th>24h 请求</th><th>24h Token</th><th>创建时间</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in filteredPlans" :key="item.id"><td><div class="admin-primary"><strong>{{ item.name }}</strong><small>{{ item.allocation_mode === 'shared' ? '共享额度' : '固定份额' }} · {{ item.visibility === 'public' ? '公开' : '私密' }}</small></div></td><td>{{ item.owner_username }}</td><td>{{ item.account_email || '未绑定' }}</td><td><StatusBadge :value="item.status" /></td><td>{{ item.member_count }}</td><td>{{ formatNumber(item.requests_24h) }}</td><td>{{ formatTokens(item.total_tokens_24h) }}</td><td>{{ formatDate(item.created_at) }}</td><td><NButton size="tiny" secondary @click="openPlanEdit(item)"><template #icon><Settings2 :size="13" /></template>管理</NButton></td></tr></tbody>
        </table>

        <table v-else class="admin-table">
          <thead><tr><th>API Key</th><th>用户</th><th>状态</th><th>策略</th><th>路由</th><th>最近使用</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in filteredKeys" :key="item.id"><td><div class="admin-primary"><strong>{{ item.name }}</strong><small><code>{{ item.key_prefix }}...</code></small></div></td><td><div class="admin-primary"><strong>{{ item.username }}</strong><small>{{ item.email }}</small></div></td><td><StatusBadge :value="item.status" /></td><td>{{ item.strategy === 'balanced' ? '额度均衡' : '优先级' }}</td><td>{{ item.route_count }}</td><td>{{ item.last_used_at ? formatDate(item.last_used_at) : '尚未使用' }}</td><td><NPopconfirm positive-text="吊销 Key" negative-text="取消" @positive-click="revokeKey(item)"><template #trigger><NButton size="tiny" secondary type="error" :disabled="item.status !== 'active'" :loading="actionID === item.id">吊销</NButton></template>吊销后无法恢复，使用该 Key 的客户端会立即停止访问。</NPopconfirm></td></tr></tbody>
        </table>
      </div>
      <NEmpty v-if="!loading && currentCount === 0" description="没有匹配的记录" />
    </div>
  </section>

  <ModalShell v-if="editingAccount" title="编辑 OpenAI 账号" :subtitle="`${editingAccount.owner_username} · ${editingAccount.email}`" :wide="true" @close="editingAccount = null">
    <AccountConfigFields v-model="accountConfig" :show-status="true" />
    <template #footer><NButton @click="editingAccount = null">取消</NButton><NButton type="primary" :loading="saving" :disabled="!accountConfig.name.trim()" @click="saveAccountEdit"><template #icon><Save :size="15" /></template>保存账号</NButton></template>
  </ModalShell>

  <ModalShell v-if="editingPlan" title="管理 Plan" :subtitle="`${editingPlan.owner_username} · ${editingPlan.id}`" :wide="true" @close="editingPlan = null">
    <div class="admin-plan-form">
      <section>
        <header><div><strong>基本信息</strong><small>管理员操作会记录在 Plan 活动日志中</small></div></header>
        <div class="admin-form-grid">
          <label>Plan 名称<AppInput :value="planForm.name" clearable :maxlength="100" @update:value="planForm.name = $event" /></label>
          <label>运行状态<NSelect v-model:value="planForm.status" :options="planStatusOptions" to="body" /></label>
          <label class="admin-form-full">描述<AppInput :value="planForm.description" type="textarea" clearable :autosize="{ minRows: 3, maxRows: 6 }" :maxlength="2000" show-count @update:value="planForm.description = $event" /></label>
        </div>
      </section>
      <section>
        <header><div><strong>绑定账号</strong><small>只显示房主拥有、可用且未被其他 Plan 占用的账号</small></div></header>
        <label>OpenAI 账号<NSelect v-model:value="planForm.account_id" filterable :options="planAccountOptions" placeholder="选择房主的 OpenAI 账号" to="body" /></label>
        <NAlert v-if="planAccountOptions.length === 0" type="warning" :show-icon="true">该房主当前没有可绑定的有效账号，请先到“账号”页检查账号状态和占用情况。</NAlert>
      </section>
      <section>
        <header><div><strong>公开招募</strong><small>归档状态下不能修改公开设置</small></div></header>
        <div class="admin-form-grid">
          <label>可见性<NSelect v-model:value="planForm.visibility" :options="visibilityOptions" :disabled="planForm.status !== 'active'" to="body" /></label>
          <label v-if="planForm.visibility === 'public'">公开席位<NInputNumber v-model:value="planForm.public_slots" :min="1" :max="100" :precision="0" :disabled="planForm.status !== 'active'" /></label>
          <label v-if="planForm.visibility === 'public' && editingPlan.allocation_mode === 'fixed'">每席份额<NInputNumber v-model:value="planForm.public_share_basis_points" :min="0" :max="10000" :precision="0" :disabled="planForm.status !== 'active'"><template #suffix>bps</template></NInputNumber><small>按万分比存储，10000 = 100%</small></label>
        </div>
      </section>
    </div>
    <template #footer><NButton @click="editingPlan = null">取消</NButton><NButton type="primary" :loading="saving" :disabled="!planForm.name.trim()" @click="savePlanEdit"><template #icon><Save :size="15" /></template>保存 Plan</NButton></template>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NEmpty, NInput, NInputNumber, NPopconfirm, NSelect, NSpin, NTab, NTabs } from 'naive-ui'
import { Activity, Bot, Coins, KeyRound, Layers3, Pencil, RefreshCw, Save, Search, Settings2, UsersRound } from 'lucide-vue-next'
import { api } from '../api'
import type { AccountConfigInput, AdminAPIKey, AdminAccount, AdminOverview, AdminPlan, AdminUser, User } from '../types'
import { formatPercent, formatTokens } from '../dashboardFormat'
import AccountConfigFields from '../components/AccountConfigFields.vue'
import AppInput from '../components/AppInput.vue'
import ModalShell from '../components/ModalShell.vue'
import StatusBadge from '../components/StatusBadge.vue'

const props = defineProps<{ currentUser: User }>()
const emit = defineEmits<{ message: [type: 'success' | 'error', text: string] }>()
const overview = ref<AdminOverview | null>(null)
const users = ref<AdminUser[]>([])
const accounts = ref<AdminAccount[]>([])
const plans = ref<AdminPlan[]>([])
const keys = ref<AdminAPIKey[]>([])
const activeTab = ref<'users' | 'accounts' | 'plans' | 'keys'>('users')
const query = ref('')
const loading = ref(false)
const actionID = ref('')
const saving = ref(false)
const editingAccount = ref<AdminAccount | null>(null)
const accountConfig = ref<AccountConfigInput>(emptyAccountConfig())
const editingPlan = ref<AdminPlan | null>(null)
const planForm = ref({ name: '', description: '', status: 'active' as 'active' | 'archived', account_id: '', visibility: 'private' as 'private' | 'public', public_slots: 1, public_share_basis_points: 0 })
const planStatusOptions = [{ label: '正常', value: 'active' }, { label: '已归档', value: 'archived' }]
const visibilityOptions = [{ label: '私密', value: 'private' }, { label: '公开', value: 'public' }]
const planAccountOptions = computed(() => {
  if (!editingPlan.value) return []
  return accounts.value
    .filter(account => account.owner_user_id === editingPlan.value!.owner_user_id && account.status === 'active' && (!account.plan_id || account.plan_id === editingPlan.value!.id))
    .map(account => ({ label: `${account.name} · ${account.email}`, value: account.id }))
})
const normalizedQuery = computed(() => query.value.trim().toLowerCase())
const matches = (...values: string[]) => !normalizedQuery.value || values.some(value => value.toLowerCase().includes(normalizedQuery.value))
const filteredUsers = computed(() => users.value.filter(item => matches(item.username, item.email, item.id)))
const filteredAccounts = computed(() => accounts.value.filter(item => matches(item.name, item.email, item.owner_username, item.owner_email, item.plan_name)))
const filteredPlans = computed(() => plans.value.filter(item => matches(item.name, item.owner_username, item.account_email, item.id)))
const filteredKeys = computed(() => keys.value.filter(item => matches(item.name, item.key_prefix, item.username, item.email)))
const currentCount = computed(() => ({ users: filteredUsers.value.length, accounts: filteredAccounts.value.length, plans: filteredPlans.value.length, keys: filteredKeys.value.length })[activeTab.value])
const numberFormatter = new Intl.NumberFormat('zh-CN')
const usdFormatter = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 4 })
const dateFormatter = new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })

function formatNumber(value: number) { return numberFormatter.format(value) }
function formatUSD(value: number) { return usdFormatter.format(value / 1_000_000) }
function formatDate(value: string) { return dateFormatter.format(new Date(value)) }
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
function emptyAccountConfig(): AccountConfigInput { return { name: '', notes: '', proxy_url: '', max_concurrency: 0, rpm_limit: 0, fast_policy: [], codex_fingerprint_mode: 'session', status: 'active' } }
function openAccountEdit(item: AdminAccount) {
  editingAccount.value = item
  accountConfig.value = {
    name: item.name, notes: item.notes, proxy_url: item.proxy_url, max_concurrency: item.max_concurrency, rpm_limit: item.rpm_limit, codex_fingerprint_mode: item.codex_fingerprint_mode, status: item.status,
    fast_policy: item.fast_policy.map(rule => ({ ...rule, user_ids: [...rule.user_ids], model_whitelist: [...rule.model_whitelist] })),
  }
}
async function saveAccountEdit() {
  if (!editingAccount.value) return
  saving.value = true
  try {
    await api.adminUpdateAccount(editingAccount.value.id, { ...accountConfig.value })
    editingAccount.value = null
    await loadAll()
    emit('message', 'success', 'OpenAI 账号配置已更新')
  } catch (error) { emit('message', 'error', error instanceof Error ? error.message : String(error)) }
  finally { saving.value = false }
}
function openPlanEdit(item: AdminPlan) {
  editingPlan.value = item
  planForm.value = { name: item.name, description: item.description, status: item.status as 'active' | 'archived', account_id: item.account_id, visibility: item.visibility, public_slots: item.public_slots || 1, public_share_basis_points: item.public_share_basis_points }
}
async function savePlanEdit() {
  const original = editingPlan.value
  if (!original) return
  saving.value = true
  try {
    const form = planForm.value
    if (form.name.trim() !== original.name) await api.adminUpdatePlan(original.id, { name: form.name })
    if (form.description.trim() !== original.description) await api.adminUpdatePlan(original.id, { description: form.description })
    if (form.account_id && form.account_id !== original.account_id) await api.adminRebindPlanAccount(original.id, form.account_id)
    if (original.status === 'archived' && form.status === 'active') await api.adminUpdatePlanStatus(original.id, 'active')
    const publicationChanged = form.visibility !== original.visibility || (form.visibility === 'public' && (form.public_slots !== original.public_slots || form.public_share_basis_points !== original.public_share_basis_points))
    if (publicationChanged && form.status === 'active') await api.adminUpdatePlanPublication(original.id, form.visibility, form.visibility === 'public' ? form.public_slots : 0, form.visibility === 'public' && original.allocation_mode === 'fixed' ? form.public_share_basis_points : 0)
    if (original.status === 'active' && form.status === 'archived') await api.adminUpdatePlanStatus(original.id, 'archived')
    editingPlan.value = null
    await loadAll()
    emit('message', 'success', 'Plan 配置已更新')
  } catch (error) {
    await loadAll()
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally { saving.value = false }
}
async function revokeKey(item: AdminAPIKey) {
  actionID.value = item.id
  try { await api.adminRevokeKey(item.id); item.status = 'revoked'; overview.value = await api.adminOverview(); emit('message', 'success', 'API Key 已吊销') }
  catch (error) { emit('message', 'error', error instanceof Error ? error.message : String(error)) }
  finally { actionID.value = '' }
}
onMounted(loadAll)
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
.admin-plan-form { display: grid; gap: 14px; }
.admin-plan-form section { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-soft); }
.admin-plan-form section > header strong { display: block; color: var(--ink-strong); font-size: 12px; }
.admin-plan-form section > header small { display: block; margin-top: 3px; color: var(--muted); font-size: 11px; }
.admin-plan-form label { display: grid; gap: 7px; color: var(--ink); font-size: 11px; font-weight: 700; }
.admin-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.admin-form-full { grid-column: 1 / -1; }
.admin-plan-form label > small { color: var(--muted); font-weight: 450; }
.admin-loading { min-height: 240px; display: grid; place-items: center; }
:deep(.n-empty) { padding: 48px 20px; }
@media (max-width: 1200px) { .admin-overview-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 720px) { .admin-overview-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .admin-resource-toolbar,.admin-form-grid { grid-template-columns: 1fr; } .admin-form-full { grid-column: auto; } }
</style>
