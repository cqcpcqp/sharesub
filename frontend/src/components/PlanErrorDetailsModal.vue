<template>
  <ModalShell
    title="请求错误明细"
    :subtitle="`${periodLabel}内未返回 2xx 的网关请求，错误总数与成功率使用同一统计口径。`"
    extra-wide
    @close="emit('close')"
  >
    <div class="error-modal-toolbar">
      <div>
        <strong>{{ formatNumber(total) }}</strong>
        <span>条错误</span>
      </div>
      <p>仅保存结构化错误信息，不保存请求正文或完整响应体。</p>
    </div>

    <div v-if="loading" class="error-modal-state" aria-live="polite">
      <NSpin size="small" />
      <span>正在加载错误明细…</span>
    </div>

    <div v-else-if="loadError" class="error-modal-state error-modal-failed" role="alert">
      <TriangleAlert :size="19" />
      <div><strong>错误明细加载失败</strong><span>{{ loadError }}</span></div>
      <NButton size="small" secondary @click="loadPage(page)">重试</NButton>
    </div>

    <div v-else-if="items.length === 0" class="error-modal-state error-modal-empty">
      <CircleCheck :size="22" />
      <div><strong>这个时间段没有错误</strong><span>当前所有已记录请求都返回了 2xx。</span></div>
    </div>

    <template v-else>
      <div class="error-table-scroll">
        <table class="error-table">
          <thead>
            <tr><th>时间</th><th>状态</th><th>来源</th><th>成员</th><th>模型</th><th>错误</th><th><span class="sr-only">操作</span></th></tr>
          </thead>
          <tbody>
            <template v-for="item in items" :key="item.id">
              <tr :class="{ 'error-row-selected': selectedID === item.id }">
                <td class="error-time">{{ formatDate(item.created_at) }}</td>
                <td><span class="status-code" :class="statusClass(item.status_code)">{{ item.status_code }}</span></td>
                <td><span class="source-badge" :class="`source-${item.error_source === '' ? 'historical' : item.error_source}`">{{ sourceLabels[item.error_source] }}</span></td>
                <td class="error-member">{{ item.member_username }}</td>
                <td class="error-model">{{ item.requested_model === '' ? '—' : item.requested_model }}</td>
                <td class="error-summary">
                  <strong>{{ item.error_code === '' ? `HTTP ${item.status_code}` : item.error_code }}</strong>
                  <span>{{ item.error_message === '' ? '未记录结构化错误信息' : item.error_message }}</span>
                </td>
                <td>
                  <NButton
                    quaternary
                    size="tiny"
                    :aria-expanded="selectedID === item.id"
                    :aria-label="`${selectedID === item.id ? '收起' : '查看'}请求 ${item.request_id} 详情`"
                    @click="toggleDetails(item.id)"
                  >
                    {{ selectedID === item.id ? '收起' : '详情' }}
                  </NButton>
                </td>
              </tr>
              <tr v-if="selectedID === item.id" class="error-detail-row">
                <td colspan="7">
                  <section class="error-detail" :aria-label="`请求 ${item.request_id} 详情`">
                    <dl class="error-detail-grid">
                      <div><dt>请求 ID</dt><dd class="mono-value">{{ item.request_id }}</dd></div>
                      <div><dt>端点</dt><dd class="mono-value">{{ item.endpoint === '' ? '历史记录未保存' : item.endpoint }}</dd></div>
                      <div><dt>请求方式</dt><dd>{{ item.is_stream ? '流式' : '同步' }}</dd></div>
                      <div><dt>耗时</dt><dd>{{ formatDuration(item.duration_ms) }}</dd></div>
                      <div><dt>请求模型</dt><dd>{{ item.requested_model === '' ? '—' : item.requested_model }}</dd></div>
                      <div><dt>上游模型</dt><dd>{{ item.upstream_model === '' ? '—' : item.upstream_model }}</dd></div>
                      <div><dt>Service Tier</dt><dd>{{ item.service_tier === '' ? '默认' : item.service_tier }}</dd></div>
                      <div><dt>账号</dt><dd>{{ item.account_name }}</dd></div>
                      <div><dt>API Key</dt><dd>{{ item.api_key_name }} · <span class="mono-value">{{ item.api_key_prefix }}</span></dd></div>
                    </dl>
                    <div class="error-message-block">
                      <span>错误信息</span>
                      <pre>{{ item.error_message === '' ? '该历史记录只有状态码，没有结构化错误信息。' : item.error_message }}</pre>
                    </div>
                  </section>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>

      <footer class="error-pagination">
        <span>第 {{ firstItem }}–{{ lastItem }} 条，共 {{ formatNumber(total) }} 条</span>
        <NPagination :page="page" :page-count="pageCount" :page-slot="5" @update:page="loadPage" />
      </footer>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { NButton, NPagination, NSpin } from 'naive-ui'
import { CircleCheck, TriangleAlert } from 'lucide-vue-next'
import { api } from '../api'
import { adminAPI } from '../api/admin'
import type { GatewayErrorSource, PerformancePeriod, PlanRequestError } from '../types'
import { formatMilliseconds } from '../dashboardFormat'
import ModalShell from './ModalShell.vue'

const props = withDefaults(defineProps<{ planId: string; period: PerformancePeriod; adminMode?: boolean }>(), { adminMode: false })
const emit = defineEmits<{ close: [] }>()

const pageSize = 20
const page = ref(1)
const items = ref<PlanRequestError[]>([])
const total = ref(0)
const loading = ref(true)
const loadError = ref('')
const selectedID = ref<number | null>(null)
let requestController: AbortController | null = null

const periodLabels: Record<PerformancePeriod, string> = {
  today: '本日',
  '30m': '最近 30 分钟',
  '6h': '最近 6 小时',
  '12h': '最近 12 小时',
  '24h': '最近 24 小时',
}
const sourceLabels: Record<GatewayErrorSource, string> = {
  '': '历史记录',
  request: '请求',
  upstream: '上游',
  gateway: '网关',
}
const periodLabel = computed(() => periodLabels[props.period])
const pageCount = computed(() => Math.ceil(total.value / pageSize))
const firstItem = computed(() => (page.value - 1) * pageSize + 1)
const lastItem = computed(() => Math.min(page.value * pageSize, total.value))
const numberFormatter = new Intl.NumberFormat('zh-CN')
const dateFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
})

async function loadPage(nextPage: number) {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  page.value = nextPage
  selectedID.value = null
  loading.value = true
  loadError.value = ''
  try {
    const result = props.adminMode
      ? await adminAPI.adminPlanRequestErrors(props.planId, props.period, nextPage, pageSize, controller.signal)
      : await api.planRequestErrors(props.planId, props.period, nextPage, pageSize, controller.signal)
    items.value = result.items
    total.value = result.total
  } catch (error) {
    if (controller.signal.aborted) return
    loadError.value = error instanceof Error ? error.message : String(error)
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

function toggleDetails(id: number) {
  selectedID.value = selectedID.value === id ? null : id
}

function formatNumber(value: number) { return numberFormatter.format(value) }
function formatDate(value: string) { return dateFormatter.format(new Date(value)) }
function formatDuration(value: number) { return formatMilliseconds(value) }
function statusClass(status: number) {
  if (status >= 500) return 'status-server'
  if (status === 429) return 'status-rate-limit'
  return 'status-request'
}

onMounted(() => loadPage(1))
onBeforeUnmount(() => requestController?.abort())
</script>

<style scoped>
.error-modal-toolbar { min-width: 0; display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 11px 13px; border: 1px solid var(--line-soft); border-radius: 7px; background: var(--surface-soft); }
.error-modal-toolbar > div { display: flex; align-items: baseline; gap: 6px; white-space: nowrap; }
.error-modal-toolbar strong { color: var(--red); font-size: 18px; font-variant-numeric: tabular-nums; }
.error-modal-toolbar span, .error-modal-toolbar p { color: var(--muted); font-size: 11px; }
.error-modal-toolbar p { margin: 0; text-align: right; }
.error-modal-state { min-height: 260px; display: flex; align-items: center; justify-content: center; gap: 10px; color: var(--muted); font-size: 12px; }
.error-modal-state > div { display: grid; gap: 4px; }
.error-modal-state strong { color: var(--ink-strong); font-size: 12px; }
.error-modal-state span { color: var(--muted); font-size: 11px; }
.error-modal-failed { color: var(--red); }
.error-modal-failed .n-button { margin-left: 8px; }
.error-modal-empty { color: var(--teal); }
.error-table-scroll { min-width: 0; max-height: min(62vh, 620px); overflow: auto; border: 1px solid var(--line); border-radius: 8px; }
.error-table { width: 100%; min-width: 880px; border-collapse: collapse; }
.error-table th { position: sticky; top: 0; z-index: 1; height: 38px; padding: 0 11px; border-bottom: 1px solid var(--line); background: var(--surface-soft); color: var(--muted); font-size: 10px; font-weight: 800; text-align: left; white-space: nowrap; }
.error-table td { height: 58px; padding: 9px 11px; border-top: 1px solid var(--line-soft); color: var(--ink); font-size: 11px; vertical-align: middle; }
.error-table tbody tr:first-child td { border-top: 0; }
.error-table tbody tr:not(.error-detail-row):hover, .error-row-selected { background: var(--surface-hover); }
.error-time { width: 142px; color: var(--muted); font-variant-numeric: tabular-nums; white-space: nowrap; }
.status-code, .source-badge { display: inline-flex; align-items: center; min-height: 23px; padding: 0 8px; border-radius: 6px; font-size: 10px; font-weight: 800; white-space: nowrap; }
.status-server { background: var(--red-soft); color: var(--red); }
.status-rate-limit { background: var(--amber-soft); color: var(--amber); }
.status-request { background: var(--blue-soft); color: var(--blue); }
.source-upstream { background: var(--red-soft); color: var(--red); }
.source-request { background: var(--amber-soft); color: var(--amber); }
.source-gateway { background: var(--blue-soft); color: var(--blue); }
.source-historical { background: var(--surface-hover); color: var(--muted); }
.error-member, .error-model { max-width: 130px; overflow: hidden; color: var(--ink-strong); font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
.error-summary { min-width: 220px; max-width: 340px; }
.error-summary strong, .error-summary span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.error-summary strong { color: var(--ink-strong); font-size: 11px; }
.error-summary span { margin-top: 3px; color: var(--muted); font-size: 10px; }
.error-detail-row td { height: auto; padding: 0; background: var(--surface-soft); }
.error-detail { display: grid; gap: 13px; padding: 16px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.error-detail-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 11px 18px; margin: 0; }
.error-detail-grid > div { min-width: 0; }
.error-detail-grid dt, .error-message-block > span { color: var(--muted-light); font-size: 10px; font-weight: 760; }
.error-detail-grid dd { overflow-wrap: anywhere; margin: 4px 0 0; color: var(--ink-strong); font-size: 11px; }
.mono-value { font-family: "SFMono-Regular", Consolas, monospace; }
.error-message-block { min-width: 0; }
.error-message-block pre { max-height: 180px; overflow: auto; margin: 6px 0 0; padding: 11px 12px; border: 1px solid var(--line); border-radius: 7px; background: var(--surface); color: var(--ink); font-family: "SFMono-Regular", Consolas, monospace; font-size: 11px; line-height: 1.6; overflow-wrap: anywhere; white-space: pre-wrap; }
.error-pagination { display: flex; align-items: center; justify-content: space-between; gap: 14px; }
.error-pagination > span { color: var(--muted); font-size: 11px; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; }

@media (max-width: 720px) {
  .error-modal-toolbar { align-items: flex-start; flex-direction: column; gap: 5px; }
  .error-modal-toolbar p { text-align: left; }
  .error-detail-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .error-pagination { align-items: flex-start; flex-direction: column; }
}

@media (max-width: 480px) {
  .error-detail-grid { grid-template-columns: 1fr; }
  .error-modal-failed { align-items: flex-start; flex-wrap: wrap; padding: 28px 6px; }
}
</style>
