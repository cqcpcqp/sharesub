<template>
  <ModalShell
    v-if="show"
    :title="secret ? '连接 Codex' : '创建访问密钥'"
    :subtitle="secret ? '保存密钥并完成 Codex 配置' : '选择这个密钥可以使用的共享 Plan'"
    wide
    @close="close"
  >
    <NSteps :current="secret ? 2 : 1" size="small" class="setup-steps">
      <NStep title="配置访问" />
      <NStep title="连接 Codex" />
    </NSteps>

    <template v-if="!secret">
      <div class="key-form-grid">
        <label>密钥名称<AppInput :value="form.name" clearable :maxlength="100" placeholder="例如：我的 Codex" @update:value="updateName" /></label>
        <label>路由策略<NSelect :value="form.strategy" :options="strategyOptions" to="body" @update:value="updateStrategy" /></label>
      </div>
      <div class="route-editor">
        <div class="section-heading"><div><h3>选择 Plan</h3><p>密钥只会访问已勾选的共享空间</p></div></div>
        <div v-for="plan in usablePlans" :key="plan.id" class="route-option">
          <NCheckbox :checked="routeDrafts[plan.id].enabled" class="check-label" @update:checked="updateRouteEnabled(plan.id, $event)">
            <span><strong>{{ plan.name }}</strong><small>{{ plan.visibility === 'public' ? '公开 Plan' : '私密 Plan' }}</small></span>
          </NCheckbox>
          <label class="priority-input">优先级<NInputNumber :value="routeDrafts[plan.id].priority" size="small" :min="1" :max="10000" :disabled="!routeDrafts[plan.id].enabled" @update:value="updateRoutePriority(plan.id, $event)" /></label>
        </div>
      </div>
    </template>

    <div v-else class="key-connection-guide">
      <NAlert type="success" title="完整密钥已加密保存">
        之后仍可在 API Keys 页面使用密钥或导入到 CCS。
      </NAlert>
      <section class="connection-block">
        <header><div><span>API KEY</span><strong>访问密钥</strong></div><NButton quaternary class="icon-button" title="复制 API Key" aria-label="复制 API Key" @click="copy(secret)"><template #icon><Copy :size="17" /></template></NButton></header>
        <code class="secret-value">{{ secret }}</code>
      </section>
      <section class="connection-block">
        <header><div><span>BASE URL</span><strong>网关地址</strong></div><NButton quaternary class="icon-button" title="复制 Base URL" aria-label="复制 Base URL" @click="copy(baseURL)"><template #icon><Copy :size="17" /></template></NButton></header>
        <code class="connection-value">{{ baseURL }}</code>
      </section>
      <NTabs type="segment" animated class="config-tabs">
        <NTabPane name="config" tab="config.toml">
          <div class="code-block"><NButton quaternary class="icon-button" title="复制 config.toml" aria-label="复制 config.toml" @click="copy(codexConfig)"><template #icon><Copy :size="16" /></template></NButton><pre><code>{{ codexConfig }}</code></pre></div>
        </NTabPane>
        <NTabPane name="auth" tab="auth.json">
          <div class="code-block"><NButton quaternary class="icon-button" title="复制 auth.json" aria-label="复制 auth.json" @click="copy(codexAuth)"><template #icon><Copy :size="16" /></template></NButton><pre><code>{{ codexAuth }}</code></pre></div>
        </NTabPane>
      </NTabs>
    </div>

    <template #footer>
      <NButton v-if="!secret" @click="close">取消</NButton>
      <NButton v-if="!secret" type="primary" :loading="saving" :disabled="!canCreate" @click="createKey"><template #icon><KeyRound :size="17" /></template>创建密钥</NButton>
      <NButton v-if="secret" secondary @click="importToCCS"><template #icon><Upload :size="17" /></template>导入到 CCS</NButton>
      <NButton v-if="secret" type="primary" @click="close"><template #icon><Check :size="17" /></template>完成</NButton>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import { NAlert, NButton, NCheckbox, NInputNumber, NSelect, NStep, NSteps, NTabPane, NTabs } from 'naive-ui'
import { computed, reactive, ref, watch } from 'vue'
import { Check, Copy, KeyRound, Upload } from 'lucide-vue-next'
import { api } from '../api'
import { buildCCSwitchImportDeepLink, codexConfigFiles, gatewayBaseURL, openCCSwitchImport } from '../keyUsage'
import { canSubmitKeyConfig, type KeyRouteDraft } from '../keyConfigValidation'
import { isPlanRoutable } from '../planAvailability'
import type { Plan, RouteStrategy } from '../types'
import AppInput from './AppInput.vue'
import ModalShell from './ModalShell.vue'

const props = withDefaults(defineProps<{ show: boolean; plans: Plan[]; initialPlanId?: string }>(), { initialPlanId: '' })
const emit = defineEmits<{
  'update:show': [show: boolean]
  created: []
  message: [type: 'success' | 'error', text: string]
}>()
const form = reactive<{ name: string; strategy: RouteStrategy }>({ name: '我的 Codex', strategy: 'balanced' })
const routeDrafts = reactive<Record<string, KeyRouteDraft>>({})
const saving = ref(false)
const secret = ref('')
const strategyOptions = [
  { label: '剩余额度均衡', value: 'balanced' },
  { label: '优先级故障转移', value: 'priority' },
]
const usablePlans = computed(() => props.plans.filter(isPlanRoutable))
const selectedPlans = computed(() => usablePlans.value.filter(plan => routeDrafts[plan.id]?.enabled))
const routeValues = computed(() => usablePlans.value.map(plan => routeDrafts[plan.id]))
const canCreate = computed(() => canSubmitKeyConfig(form.name, routeValues.value))
const homepage = computed(() => window.location.origin.replace(/\/+$/, ''))
const baseURL = computed(() => gatewayBaseURL(homepage.value))
const codexFiles = computed(() => codexConfigFiles(baseURL.value, secret.value, 'unix'))
const codexConfig = computed(() => codexFiles.value[0].content)
const codexAuth = computed(() => codexFiles.value[1].content)

watch(
  () => [props.show, props.initialPlanId, props.plans.map(plan => `${plan.id}:${plan.status}:${plan.account_id}`).join(',')] as const,
  ([show]) => { if (show && !secret.value) reset() },
  { immediate: true },
)

function reset() {
  form.name = '我的 Codex'
  form.strategy = 'balanced'
  for (const key of Object.keys(routeDrafts)) delete routeDrafts[key]
  const requestedPlan = usablePlans.value.find(plan => plan.id === props.initialPlanId)
  const defaultPlanID = requestedPlan?.id || (usablePlans.value.length === 1 ? usablePlans.value[0].id : '')
  for (const plan of usablePlans.value) routeDrafts[plan.id] = { enabled: plan.id === defaultPlanID, priority: 100 }
}

function updateName(value: string) { form.name = value }
function updateStrategy(value: RouteStrategy) { form.strategy = value }
function updateRouteEnabled(planID: string, enabled: boolean) { routeDrafts[planID].enabled = enabled }
function updateRoutePriority(planID: string, priority: number | null) { routeDrafts[planID].priority = priority }

async function createKey() {
  if (!canCreate.value) return
  saving.value = true
  const routes = selectedPlans.value.map(plan => ({
    plan_id: plan.id,
    plan_name: plan.name,
    priority: routeDrafts[plan.id].priority!,
    enabled: true,
  }))
  try {
    const result = await api.createKey({ name: form.name.trim(), strategy: form.strategy, routes })
    secret.value = result.key
    emit('created')
    emit('message', 'success', 'API Key 已创建')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    saving.value = false
  }
}

function close() {
  if (saving.value) return
  secret.value = ''
  emit('update:show', false)
}

function importToCCS() {
  const deepLink = buildCCSwitchImportDeepLink({ homepage: homepage.value, endpoint: homepage.value, apiKey: secret.value, providerName: 'ShareSub' })
  if (!openCCSwitchImport(deepLink)) emit('message', 'error', '无法唤起 CC Switch，请确认已安装并允许浏览器打开外部应用')
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    emit('message', 'success', '已复制到剪贴板')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  }
}
</script>
