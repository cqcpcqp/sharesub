<template>
  <section class="pricing-page" aria-labelledby="pricing-title">
    <header class="pricing-heading">
      <div>
        <h1 id="pricing-title">模型计价</h1>
        <p>全平台统一计价，适用于所有 Plan。</p>
      </div>
      <div class="pricing-actions">
        <NButton :disabled="loading || editing" @click="openHistory">版本历史</NButton>
        <NButton v-if="user.is_admin && !editing" type="primary" :disabled="!current || loading" @click="startEditing">编辑计价</NButton>
      </div>
    </header>

    <NAlert v-if="error" type="error" class="pricing-feedback" title="操作未完成">{{ error }} <NButton v-if="!current" text @click="loadCurrent">重试加载</NButton></NAlert>
    <NAlert v-if="success" type="success" class="pricing-feedback" closable @close="success = ''">{{ success }}</NAlert>
    <div v-if="loading && !current" class="pricing-loading"><NSpin size="small" /> 正在读取全局价格</div>

    <template v-if="current && displayed">
      <div class="pricing-version-bar">
        <span><strong>{{ editing ? '未发布草稿' : '当前计价' }}</strong> · 版本 {{ current.id }} · {{ dateTime(current.published_at) }} 生效</span>
        <NButton v-if="!editing" text :loading="loading" @click="loadCurrent">刷新价格</NButton>
        <span v-else>基于版本 {{ current.id }}，修改尚未影响任何请求</span>
      </div>
      <NAlert v-if="editing" type="warning" class="pricing-feedback">发布后，全平台新请求使用新价，影响费用、成员额度分摊和美元限额。已开始的请求及历史费用不变。</NAlert>

      <div class="pricing-toolbar">
        <NInput v-model:value="search" clearable placeholder="搜索模型，例如 astra" aria-label="搜索模型" />
        <span class="pricing-unit-label">Standard 基础价格</span>
        <span>USD / 1M Token</span>
      </div>
      <div class="pricing-table-wrap" tabindex="0" aria-label="模型价格表，可横向滚动">
        <table class="pricing-table">
          <thead><tr><th scope="col">模型</th><th v-for="field in primaryFields" :key="field.key" scope="col">{{ field.label }}</th><th scope="col">规则</th></tr></thead>
          <tbody>
            <tr v-for="model in visibleModels" :key="model.model">
              <th scope="row"><span>{{ model.model }}</span><small v-if="model.long_context_tokens">输入超过 {{ formatPrice(model.long_context_tokens, 1) }} Token 时加价</small></th>
              <td v-for="field in primaryFields" :key="field.key">{{ formatPrice(model.standard[field.key]) }}</td>
              <td><NButton text type="primary" :aria-label="`${editing ? '编辑' : '查看'} ${model.model} 计价规则`" @click="selectedModelID = model.model">{{ editing ? '编辑' : '详情' }}</NButton></td>
            </tr>
            <tr v-if="!filteredModels.length"><td colspan="6" class="pricing-empty">没有匹配的模型，请调整搜索词。</td></tr>
          </tbody>
        </table>
      </div>
      <div class="pricing-pagination"><span>{{ filteredModels.length }} 个模型，含兼容版本</span><NPagination v-model:page="page" :page-size="15" :item-count="filteredModels.length" simple /></div>

      <section class="pricing-multipliers"><h2>服务档位倍率</h2><p>所有模型基于 Standard 价格计算，Fast / Priority 和 Flex 共用全局倍率。</p><div class="pricing-addon-fields"><label><span>Fast / Priority <small>倍率</small></span><NInputNumber v-if="editing && draft" :value="draft.fast_multiplier_bps / 10000" :min="1" :max="100" :precision="4" :show-button="false" @update:value="value => updateMultiplier('fast_multiplier_bps', value)" /><strong v-else>{{ (displayed.fast_multiplier_bps / 10000).toFixed(2) }} 倍</strong></label><label><span>Flex <small>倍率</small></span><NInputNumber v-if="editing && draft" :value="draft.flex_multiplier_bps / 10000" :min="0" :max="100" :precision="4" :show-button="false" @update:value="value => updateMultiplier('flex_multiplier_bps', value)" /><strong v-else>{{ (displayed.flex_multiplier_bps / 10000).toFixed(2) }} 倍</strong></label></div></section><section class="pricing-addons" aria-labelledby="pricing-addons-title">
        <h2 id="pricing-addons-title">图片与搜索</h2>
        <p>生成图片按尺寸单张计价，替代该响应的 Token 费用；Web Search 费用另行累计。</p>
        <div class="pricing-addon-fields">
          <label v-for="field in addonPriceFields" :key="field.key">
            <span>{{ field.label }} <small>{{ field.unit }}</small></span>
            <NInputNumber v-if="editing && draft" :value="draft[field.key] / 1_000_000" :min="0" :max="1000" :precision="6" :show-button="false" :aria-label="field.label" @update:value="value => updateAddon(field.key, value)" />
            <strong v-else>{{ formatPrice(displayed[field.key]) }}</strong>
          </label>
        </div>
      </section>

      <section v-if="editing" class="pricing-publish" aria-labelledby="pricing-publish-title">
        <div><h2 id="pricing-publish-title">发布全局价格</h2><p>{{ changes.length }} 项变更 · 草稿仅保留在当前页面，离开前请发布或取消。</p></div>
        <NInput v-model:value="reason" type="textarea" :maxlength="500" show-count placeholder="填写修改原因，所有用户可在版本历史中查看" aria-label="修改原因" />
        <div class="pricing-actions"><NButton :disabled="publishing" @click="cancelEditing">取消编辑</NButton><NButton type="primary" :disabled="!changes.length || !reason.trim()" @click="previewVisible = true">预览变更</NButton></div>
      </section>

      <section class="pricing-explanation">
        <h2>如何理解计价与额度</h2>
        <p>此表用于计算平台记录的费用、成员额度分摊和美元限额，不代表实际支付金额。上游 5h / 7d 百分比独立读取，不会因修改本表而改变。</p>
        <p>Standard 是模型基础价格；Fast / Priority 和 Flex 使用全局倍率。输入总量包含缓存部分，普通输入费用扣除缓存读取、缓存写入及图片输入后计算，不重复计费。长上下文规则按每个响应判断，输入与缓存使用输入倍率，文本输出使用输出倍率，图片 Token 保持单独价格。</p>
        <p>费用按分项舍入到微美元后累计。历史费用不会按新价格重算；本功能上线前的请求未记录计价版本，不追溯归入当前版本。页面不会自动刷新正在编辑的草稿，查看最新价格请点击刷新。</p>
      </section>
    </template>

    <NDrawer :show="selectedModel !== null" width="min(560px, 100vw)" placement="right" @update:show="value => { if (!value) selectedModelID = '' }">
      <NDrawerContent v-if="selectedModel" :title="selectedModel.model" closable>
        <p class="pricing-drawer-intro">{{ editing ? '编辑草稿，确认发布后才会全局生效。' : '所有 Plan 使用同一套规则。' }} 单位：USD / 1M Token。</p>
        <span class="pricing-unit-label">Standard 基础价格</span>
        <div class="pricing-editor-grid">
          <label v-for="field in tokenPriceFields" :key="field.key"><span>{{ field.label }}</span>
            <NInputNumber v-if="editing" :value="selectedModel.standard[field.key] / 1_000_000" :min="0" :max="1_000_000" :precision="6" :show-button="false" :aria-label="`Standard ${field.label}`" @update:value="value => updateTokenPrice(field.key, value)" />
            <strong v-else>{{ formatPrice(selectedModel.standard[field.key]) }}</strong>
          </label>
        </div>
        <h3>长上下文</h3>
        <div class="pricing-long-fields">
          <label><span>输入阈值 · Token（0 为关闭，超过才加价）</span><NInputNumber v-if="editing" :value="selectedModel.long_context_tokens" :min="0" :max="10_000_000" :precision="0" aria-label="长上下文阈值" @update:value="value => updateLongContext('long_context_tokens', value, 1)" /><strong v-else>{{ selectedModel.long_context_tokens ? formatPrice(selectedModel.long_context_tokens, 1) : '未启用' }}</strong></label>
          <label><span>输入 / 缓存倍率</span><NInputNumber v-if="editing" :value="selectedModel.long_input_bps / 10_000" :min="1" :max="100" :precision="4" aria-label="长上下文输入倍率" @update:value="value => updateLongContext('long_input_bps', value, 10_000)" /><strong v-else>{{ selectedModel.long_input_bps / 10_000 }} 倍</strong></label>
          <label><span>文本输出倍率</span><NInputNumber v-if="editing" :value="selectedModel.long_output_bps / 10_000" :min="1" :max="100" :precision="4" aria-label="长上下文输出倍率" @update:value="value => updateLongContext('long_output_bps', value, 10_000)" /><strong v-else>{{ selectedModel.long_output_bps / 10_000 }} 倍</strong></label>
        </div>
        <div class="pricing-example"><h3>费用示例</h3><p>100,000 输入（其中 50,000 缓存读取），10,000 输出，无图片、缓存写入及搜索。</p><strong>Standard：${{ formatPrice(exampleCost(selectedModel, 'standard')) }}</strong></div>
        <template #footer><NButton @click="selectedModelID = ''">{{ editing ? '完成草稿编辑' : '关闭' }}</NButton></template>
      </NDrawerContent>
    </NDrawer>

    <NModal v-model:show="previewVisible" preset="card" title="确认发布全局价格" class="pricing-preview-modal" :mask-closable="!publishing" :closable="!publishing" :close-on-esc="!publishing">
      <NAlert type="warning">适用于所有 Plan。发布后新请求使用新价，影响成员额度分摊和美元限额；在途请求及历史费用不变。</NAlert>
      <p>修改原因：{{ reason }}</p>
      <div class="pricing-table-wrap"><table class="pricing-table pricing-diff-table"><thead><tr><th>模型 / 项目</th><th>字段</th><th>修改前</th><th>修改后</th><th>变化</th></tr></thead><tbody><tr v-for="change in changes" :key="`${change.item}-${change.field}`"><th scope="row">{{ change.item }}</th><td>{{ change.field }}<small>{{ change.unit }}</small></td><td>{{ formatPrice(change.before, change.scale) }}</td><td>{{ formatPrice(change.after, change.scale) }}</td><td>{{ priceChangePercent(change) }}</td></tr></tbody></table></div>
      <section v-if="changedModels.length" class="pricing-comparison"><h3>相同用量，新旧价格对比</h3><p>每个样例：100,000 输入（含 50,000 缓存读取）+ 10,000 输出，Standard 基础价格。</p><ul><li v-for="model in changedModels" :key="model.model">{{ model.model }}：${{ formatPrice(exampleCost(current!.config.models.find(item => item.model === model.model)!, 'standard')) }} → ${{ formatPrice(exampleCost(model, 'standard')) }}</li></ul></section>
      <NAlert v-if="publishError" type="error">{{ publishError }}</NAlert>
      <template #footer><div class="pricing-actions"><NButton :disabled="publishing" @click="previewVisible = false">返回编辑</NButton><NButton type="primary" :loading="publishing" :disabled="!changes.length || !reason.trim()" @click="publish">确认发布并生效</NButton></div></template>
    </NModal>

    <NDrawer v-model:show="historyVisible" width="min(640px, 100vw)" placement="right">
      <NDrawerContent title="计价版本历史" closable>
        <p>恢复历史价格会生成一个新版本，不覆盖旧版本，也不重算历史费用。</p>
        <NAlert v-if="historyError" type="error">{{ historyError }} <NButton text @click="loadHistory(false)">重试</NButton></NAlert>
        <div v-for="version in history" :key="version.id" class="pricing-history-item"><div><strong>版本 {{ version.id }}{{ version.id === current?.id ? ' · 当前' : '' }}</strong><time>{{ dateTime(version.published_at) }}</time></div><p>{{ version.reason }}</p><small>发布人：{{ version.published_by }}</small><div class="pricing-actions"><NButton size="small" :loading="historyLoading" @click="viewHistoryVersion(version.id)">查看价格</NButton><NButton v-if="user.is_admin && version.id !== current?.id" size="small" :disabled="editing || historyLoading" @click="restoreVersion(version.id)">恢复为草稿</NButton></div></div>
        <NButton v-if="hasMoreHistory" :loading="historyLoading" @click="loadHistory(true)">加载更早版本</NButton>
        <NSpin v-if="historyLoading && !history.length" size="small" />
        <section v-if="historicalVersion" class="pricing-history-detail"><h3>版本 {{ historicalVersion.id }} · Standard</h3><span class="pricing-unit-label">Standard 基础价格</span><NInput v-model:value="historySearch" clearable placeholder="搜索历史模型" aria-label="搜索历史模型" /><div class="pricing-table-wrap"><table class="pricing-table"><thead><tr><th>模型</th><th v-for="field in tokenPriceFields" :key="field.key">{{ field.label }}</th><th>长上下文阈值</th><th>输入倍率</th><th>输出倍率</th></tr></thead><tbody><tr v-for="model in historicalVersion.config.models.filter(item => item.model.includes(historySearch.trim().toLowerCase()))" :key="model.model"><th scope="row">{{ model.model }}</th><td v-for="field in tokenPriceFields" :key="field.key">{{ formatPrice(model.standard[field.key]) }}</td><td>{{ model.long_context_tokens }}</td><td>{{ model.long_input_bps / 10_000 }}</td><td>{{ model.long_output_bps / 10_000 }}</td></tr></tbody></table></div><p v-for="field in addonPriceFields" :key="field.key">{{ field.label }}：{{ formatPrice(historicalVersion.config[field.key]) }} {{ field.unit }}</p></section>
      </NDrawerContent>
    </NDrawer>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NAlert, NButton, NDrawer, NDrawerContent, NInput, NInputNumber, NModal, NPagination, NSpin } from 'naive-ui'
import { api, APIRequestError } from '../api'
import type { User } from '../types'
import { addonPriceFields, exampleCost, formatPrice, priceChangePercent, pricingChanges, tokenPriceFields, type PricingConfig, type PricingVersion, type PricingVersionSummary, type TokenPrices } from '../pricing'
import './PricingView.css'

defineProps<{ user: User }>()
const current = ref<PricingVersion | null>(null)
const draft = ref<PricingConfig | null>(null)
const loading = ref(false)
const error = ref('')
const success = ref('')
const search = ref('')
const page = ref(1)
const reason = ref('')
const selectedModelID = ref('')
const previewVisible = ref(false)
const publishing = ref(false)
const publishError = ref('')
const historyVisible = ref(false)
const historyLoading = ref(false)
const historyError = ref('')
const history = ref<PricingVersionSummary[]>([])
const hasMoreHistory = ref(false)
const historicalVersion = ref<PricingVersion | null>(null)
const historySearch = ref('')
const editing = computed(() => draft.value !== null)
const displayed = computed(() => draft.value || current.value?.config)
const primaryFields = tokenPriceFields.slice(0, 4)
const featuredModels = ['gpt-6-astra', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna', 'gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-5.3-codex-spark', 'codex-auto-review', 'gpt-5.2', 'gpt-image-1', 'gpt-image-1.5', 'gpt-image-2']
const filteredModels = computed(() => {
  if (!displayed.value) return []
  return displayed.value.models.filter(model => model.model.includes(search.value.trim().toLowerCase())).slice().sort((left, right) => {
    const leftIndex = featuredModels.indexOf(left.model)
    const rightIndex = featuredModels.indexOf(right.model)
    return (leftIndex < 0 ? 999 : leftIndex) - (rightIndex < 0 ? 999 : rightIndex) || left.model.localeCompare(right.model)
  })
})
const visibleModels = computed(() => filteredModels.value.slice((page.value - 1) * 15, page.value * 15))
const selectedModel = computed(() => {
  if (!displayed.value || !selectedModelID.value) return null
  return displayed.value.models.find(model => model.model === selectedModelID.value)!
})
const changes = computed(() => current.value && draft.value ? pricingChanges(current.value.config, draft.value) : [])
const changedModels = computed(() => {
  if (!draft.value) return []
  const names = new Set(changes.value.map(change => change.item))
  return draft.value.models.filter(model => names.has(model.model))
})
watch(search, () => { page.value = 1 })
function dateTime(value: string) { return new Date(value).toLocaleString('zh-CN', { hour12: false }) }
function message(cause: unknown) { return cause instanceof Error ? cause.message : '操作失败，请重试。' }
function cloneConfig(config: PricingConfig): PricingConfig { return JSON.parse(JSON.stringify(config)) }
async function loadCurrent() {
  loading.value = true
  error.value = ''
  try { current.value = await api.pricing() } catch (cause) { error.value = message(cause) } finally { loading.value = false }
}
function startEditing() {
  if (!current.value) return
  draft.value = cloneConfig(current.value.config)
  reason.value = ''
  publishError.value = ''
  success.value = ''
}
function cancelEditing() { draft.value = null; selectedModelID.value = ''; previewVisible.value = false; publishError.value = '' }
function updateTokenPrice(key: keyof TokenPrices, value: number | null) {
  if (value !== null && selectedModel.value) selectedModel.value.standard[key] = Math.round(value * 1_000_000)
}
function updateAddon(key: Exclude<keyof PricingConfig, 'models' | 'fast_multiplier_bps' | 'flex_multiplier_bps'>, value: number | null) {
  if (value !== null && draft.value) draft.value[key] = Math.round(value * 1_000_000)
}
function updateMultiplier(key: 'fast_multiplier_bps' | 'flex_multiplier_bps', value: number | null) { if (value !== null && draft.value) draft.value[key] = Math.round(value * 10000) }
function updateLongContext(key: 'long_context_tokens' | 'long_input_bps' | 'long_output_bps', value: number | null, scale: number) {
  if (value !== null && selectedModel.value) selectedModel.value[key] = Math.round(value * scale)
}
async function publish() {
  if (!current.value || !draft.value || publishing.value) return
  publishing.value = true
  publishError.value = ''
  try {
    current.value = await api.publishPricing({ base_version_id: current.value.id, reason: reason.value.trim(), config: cloneConfig(draft.value) })
    cancelEditing()
    success.value = `版本 ${current.value.id} 已发布，全平台新请求开始使用新价。历史费用保持不变。`
  } catch (cause) {
    publishError.value = cause instanceof APIRequestError && cause.status === 409 ? '价格已被其他管理员更新，未覆盖其修改。请返回编辑并保留所需修改，取消草稿、刷新价格后重新编辑。' : message(cause)
  } finally { publishing.value = false }
}
async function openHistory() { historyVisible.value = true; historicalVersion.value = null; await loadHistory(false) }
async function loadHistory(append: boolean) {
  historyLoading.value = true
  historyError.value = ''
  try {
    const before = append && history.value.length ? history.value[history.value.length - 1].id : 0
    const versions = await api.pricingHistory(before)
    history.value = append ? [...history.value, ...versions] : versions
    hasMoreHistory.value = versions.length === 50
  } catch (cause) { historyError.value = message(cause) } finally { historyLoading.value = false }
}
async function viewHistoryVersion(id: number) {
  historyLoading.value = true
  historyError.value = ''
  try { historicalVersion.value = await api.pricingVersion(id) } catch (cause) { historyError.value = message(cause) } finally { historyLoading.value = false }
}
async function restoreVersion(id: number) {
  historyLoading.value = true
  historyError.value = ''
  try {
    const version = await api.pricingVersion(id)
    current.value = await api.pricing()
    draft.value = cloneConfig(version.config)
    reason.value = `恢复版本 ${id} 的价格`
    historyVisible.value = false
    publishError.value = ''
  } catch (cause) { historyError.value = message(cause) } finally { historyLoading.value = false }
}
function confirmLeave(event: BeforeUnloadEvent) {
  if (!changes.value.length) return
  event.preventDefault()
}
onMounted(() => { void loadCurrent(); window.addEventListener('beforeunload', confirmLeave) })
onBeforeUnmount(() => window.removeEventListener('beforeunload', confirmLeave))
</script>
