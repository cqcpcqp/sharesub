<template>
  <section class="view-content">
    <div class="collection-toolbar"><p><strong>{{ keys.length }}</strong><span>个访问密钥</span></p><NButton type="primary" :disabled="routablePlans.length === 0" @click="showCreate = true"><template #icon><Plus :size="17" /></template>创建 API Key</NButton></div>
    <div v-if="keys.length" class="key-list">
      <article v-for="key in keys" :key="key.id" class="key-row">
        <header>
          <div class="key-main">
            <span class="key-icon"><KeyRound :size="19" /></span>
            <div>
              <div class="key-title">
                <strong>{{ key.name }}</strong>
                <StatusBadge :value="key.status" />
                <NTag v-if="!key.key_available" size="tiny" :bordered="false" type="warning">需升级</NTag>
                <NTag v-if="key.fast_policy.length" size="tiny" :bordered="false" type="success">Key 策略 {{ key.fast_policy.length }}</NTag>
              </div>
              <code>{{ key.key_prefix }}...</code>
            </div>
          </div>
          <div class="row-actions">
            <NButton secondary class="icon-button" title="编辑配置" aria-label="编辑配置" :disabled="key.status !== 'active'" @click="openEdit(key)"><template #icon><Settings2 :size="17" /></template></NButton>
            <NPopconfirm
              positive-text="撤销密钥"
              negative-text="取消"
              @positive-click="revoke(key)"
            >
              <template #trigger>
                <NButton quaternary type="error" class="icon-button" title="撤销 Key" aria-label="撤销 Key" :disabled="key.status !== 'active'" :loading="revokingID === key.id"><template #icon><Trash2 :size="17" /></template></NButton>
              </template>
              撤销后无法恢复，所有使用这个密钥的客户端都会停止访问。
            </NPopconfirm>
          </div>
        </header>
        <div class="key-details"><div class="key-strategy"><small><GitFork :size="14" />路由策略</small><strong>{{ key.strategy === 'balanced' ? '剩余额度均衡' : '优先级故障转移' }}</strong></div><div class="key-routes"><small><Waypoints :size="14" />连接的 Plans</small><div class="route-chips"><NTag v-for="route in key.routes" :key="route.plan_id" size="small" :bordered="false" type="info">{{ route.plan_name }}<small>#{{ route.priority }}</small></NTag></div></div><div class="key-used"><small><Clock3 :size="14" />最近使用</small><span>{{ key.last_used_at ? formatDate(key.last_used_at) : '尚未使用' }}</span></div></div>
        <footer class="key-quick-actions">
          <template v-if="key.key_available">
            <NButton secondary :disabled="key.status !== 'active'" :title="key.status === 'active' ? '' : '已撤销的密钥不可使用'" @click="useKey(key)">
              <template #icon><SquareTerminal :size="16" /></template>
              使用密钥
            </NButton>
            <NButton secondary :disabled="key.status !== 'active'" :title="key.status === 'active' ? '' : '已撤销的密钥不可使用'" @click="importToCCS(key)">
              <template #icon><Upload :size="16" /></template>
              导入到 CCS
            </NButton>
          </template>
          <NButton v-else secondary class="legacy-key-upgrade" :disabled="!canUpgradeKey(key)" :title="upgradeActionTitle(key)" @click="upgradeTarget = key">
            <template #icon><RotateCw :size="16" /></template>
            升级为可查看密钥
          </NButton>
        </footer>
      </article>
    </div>
    <EmptyState v-else title="还没有 API Key" :description="routablePlans.length ? '创建属于你的 Key，并选择它可以使用的 Plan。' : '先恢复一个已绑定账号的 Plan，或为状态正常的 Plan 绑定账号，再创建访问密钥。'" :icon="KeyRound" />
  </section>

  <ModalShell v-if="editing" title="编辑 API Key" subtitle="配置路由目标、选路策略和 Fast/Flex 规则" wide @close="closeEdit">
    <div class="key-form-grid"><label>名称<AppInput :value="nameDraft" clearable :maxlength="100" :input-props="{ 'aria-label': 'API Key 名称' }" placeholder="例如：个人 Codex" @update:value="updateNameDraft" /></label><label>路由策略<NSelect :value="strategyDraft" :options="strategyOptions" to="body" @update:value="updateStrategyDraft" /></label></div>
    <div class="route-editor"><div class="section-heading"><div><h3>路由 Plans</h3><p>归档路由会暂停；数字越小，优先级越高</p></div></div><div class="route-option" v-for="plan in editablePlans" :key="plan.id"><NCheckbox :checked="routeDrafts[plan.id].enabled" class="check-label" @update:checked="updateRouteEnabled(plan.id, $event)"><span><strong>{{ plan.name }}</strong><small>{{ plan.status === 'archived' ? '已归档，恢复后自动生效' : plan.visibility === 'public' ? '公开' : '私密' }}</small></span></NCheckbox><label class="priority-input">优先级<NInputNumber :value="routeDrafts[plan.id].priority" size="small" :min="1" :max="10000" :disabled="!routeDrafts[plan.id].enabled" @update:value="updateRoutePriority(plan.id, $event)" /></label></div></div>
    <FastPolicyFields v-model="fastPolicyDraft" scope="key" />
    <template #footer><NButton :disabled="saving" @click="closeEdit">取消</NButton><NButton type="primary" :loading="saving" :disabled="!canSave" @click="save"><template #icon><Save :size="17" /></template>保存配置</NButton></template>
  </ModalShell>

  <ModalShell v-if="upgradeTarget" title="升级为可查看密钥" :subtitle="upgradeTarget.name" @close="closeUpgrade">
    <NAlert type="warning" title="旧密钥原文无法恢复">
      系统会保留当前名称、路由与策略，生成一条加密保存的新密钥并撤销旧密钥。仍在使用旧密钥的客户端需要更新配置。
    </NAlert>
    <template #footer><NButton :disabled="upgrading" @click="closeUpgrade">取消</NButton><NButton type="primary" :loading="upgrading" @click="confirmUpgrade"><template #icon><RotateCw :size="17" /></template>生成并保存新密钥</NButton></template>
  </ModalShell>

  <APIKeySetupWizard v-model:show="showCreate" :plans="routablePlans" @created="emit('changed')" @message="forwardMessage" />
  <UseKeyModal v-if="usingKey" :show="true" :api-key="usingKey" @close="usingKey = null" @message="forwardMessage" />
</template>

<script setup lang="ts">
import { NAlert, NButton, NCheckbox, NInputNumber, NPopconfirm, NSelect, NTag } from 'naive-ui'
import { computed, reactive, ref } from 'vue'
import { Clock3, GitFork, KeyRound, Plus, RotateCw, Save, Settings2, SquareTerminal, Trash2, Upload, Waypoints } from 'lucide-vue-next'
import { api } from '../api'
import type { APIKey, FastPolicyRule, Plan, RouteStrategy } from '../types'
import { buildCCSwitchImportDeepLink, gatewayBaseURL, openCCSwitchImport } from '../keyUsage'
import { canSubmitKeyConfig, type KeyRouteDraft } from '../keyConfigValidation'
import { availableKeyRoutes, canUpgradeAPIKey } from '../keyReissue'
import { isPlanRoutable } from '../planAvailability'
import EmptyState from '../components/EmptyState.vue'
import FastPolicyFields from '../components/FastPolicyFields.vue'
import AppInput from '../components/AppInput.vue'
import APIKeySetupWizard from '../components/APIKeySetupWizard.vue'
import ModalShell from '../components/ModalShell.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UseKeyModal from '../components/UseKeyModal.vue'

const props = defineProps<{ keys: APIKey[]; plans: Plan[] }>()
const emit = defineEmits<{ changed: []; message: [type: 'success' | 'error', text: string] }>()
const editing = ref<APIKey | null>(null)
const usingKey = ref<APIKey | null>(null)
const showCreate = ref(false)
const saving = ref(false)
const revokingID = ref('')
const upgradeTarget = ref<APIKey | null>(null)
const upgrading = ref(false)
const nameDraft = ref('')
const strategyDraft = ref<RouteStrategy>('balanced')
const fastPolicyDraft = ref<FastPolicyRule[]>([])
const routeDrafts = reactive<Record<string, KeyRouteDraft>>({})
const routablePlans = computed(() => props.plans.filter(isPlanRoutable))
const editablePlans = computed(() => {
  const existingPlanIDs = new Set(editing.value?.routes.map(route => route.plan_id) ?? [])
  return props.plans.filter(plan => isPlanRoutable(plan) || existingPlanIDs.has(plan.id))
})
const strategyOptions = [
  { label: '剩余额度均衡', value: 'balanced' },
  { label: '优先级故障转移', value: 'priority' },
]
const routeValues = computed(() => editablePlans.value.map(plan => routeDrafts[plan.id]))
const canSave = computed(() => canSubmitKeyConfig(nameDraft.value, routeValues.value))

function resetRoutes(key: APIKey) {
  for (const planID of Object.keys(routeDrafts)) delete routeDrafts[planID]
  for (const plan of editablePlans.value) {
    const route = key.routes.find(item => item.plan_id === plan.id)
    routeDrafts[plan.id] = { enabled: route?.enabled ?? false, priority: route?.priority ?? 100 }
  }
}
function cloneFastPolicy(policy: FastPolicyRule[]) { return policy.map(rule => ({ ...rule, user_ids: [...rule.user_ids], model_whitelist: [...rule.model_whitelist] })) }
function openEdit(key: APIKey) { editing.value = key; nameDraft.value = key.name; strategyDraft.value = key.strategy; fastPolicyDraft.value = cloneFastPolicy(key.fast_policy); resetRoutes(key) }
function closeEdit() { if (!saving.value) editing.value = null }
function updateNameDraft(value: string) { nameDraft.value = value }
function updateStrategyDraft(value: RouteStrategy) { strategyDraft.value = value }
function updateRouteEnabled(planID: string, enabled: boolean) { routeDrafts[planID].enabled = enabled }
function updateRoutePriority(planID: string, priority: number | null) { routeDrafts[planID].priority = priority }
function availableRoutes(key: APIKey) { return availableKeyRoutes(key, props.plans) }
function canUpgradeKey(key: APIKey) { return canUpgradeAPIKey(key, props.plans) }
function upgradeActionTitle(key: APIKey) {
  if (key.status !== 'active') return '已撤销的密钥不可使用'
  if (availableRoutes(key).length === 0) return '没有可用于升级的 Plan'
  return '生成一条可反复查看的新密钥'
}
function useKey(key: APIKey) { if (key.status === 'active' && key.key_available) usingKey.value = key }
function importToCCS(key: APIKey) {
  if (key.status !== 'active' || !key.key_available) return
  const homepage = window.location.origin.replace(/\/+$/, '')
  const deepLink = buildCCSwitchImportDeepLink({ homepage, endpoint: homepage, apiKey: key.key, providerName: 'ShareSub' })
  if (!openCCSwitchImport(deepLink)) emit('message', 'error', '无法唤起 CC Switch，请确认已安装并允许浏览器打开外部应用')
}
function closeUpgrade() { if (!upgrading.value) upgradeTarget.value = null }
async function confirmUpgrade() {
  const target = upgradeTarget.value
  if (!target || upgrading.value) return
  const routes = availableRoutes(target)
  if (routes.length === 0) return
  upgrading.value = true
  try {
    const result = await api.createKey({ name: target.name, strategy: target.strategy, routes, fast_policy: cloneFastPolicy(target.fast_policy) })
    const replacement = { ...result.api_key, key: result.key, key_available: true }
    let oldKeyRevoked = true
    try {
      await api.revokeKey(target.id)
    } catch {
      oldKeyRevoked = false
    }
    upgradeTarget.value = null
    emit('changed')
    if (oldKeyRevoked) emit('message', 'success', '密钥已升级并加密保存')
    else emit('message', 'error', '新密钥已保存，但旧密钥撤销失败，请稍后手动撤销')
    usingKey.value = replacement
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    upgrading.value = false
  }
}
async function save() {
  if (!editing.value || !canSave.value || saving.value) return
  const routes = editablePlans.value.filter(plan => routeDrafts[plan.id].enabled).map(plan => ({ plan_id: plan.id, plan_name: plan.name, priority: routeDrafts[plan.id].priority!, enabled: true }))
  saving.value = true
  try {
    await api.updateKey(editing.value.id, { name: nameDraft.value.trim(), strategy: strategyDraft.value, routes, fast_policy: cloneFastPolicy(fastPolicyDraft.value) })
    emit('message', 'success', 'API Key 配置已更新')
    editing.value = null
    emit('changed')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    saving.value = false
  }
}
async function revoke(key: APIKey) {
  if (revokingID.value) return
  revokingID.value = key.id
  try {
    await api.revokeKey(key.id)
    emit('changed')
    emit('message', 'success', 'API Key 已撤销')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    revokingID.value = ''
  }
}
function forwardMessage(type: 'success' | 'error', text: string) { emit('message', type, text) }
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }
</script>

<style scoped>
.key-quick-actions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 16px -4px -4px;
  padding-top: 14px;
  border-top: 1px solid var(--line-soft);
}

.key-quick-actions .n-button {
  width: 100%;
}

.legacy-key-upgrade {
  grid-column: 1 / -1;
}

@media (max-width: 440px) {
  .key-quick-actions {
    grid-template-columns: 1fr;
  }
}
</style>
