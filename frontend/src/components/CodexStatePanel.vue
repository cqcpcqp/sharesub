<template>
  <section class="account-form-section" aria-label="STATE 运行状态">
    <header><span><Activity :size="17" /></span><div><strong>STATE 运行状态</strong><small>使用账号当前出口，无需代理池</small></div></header>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-if="status">
      <p v-if="!status.supported">当前套餐不支持票据验证，仅支持 Pro 和 Team / Business。</p>
      <p v-else-if="status.models.length === 0">保存并启用后，首次 HTTP Responses 请求完整成功且模型匹配后，会登记模型并触发后台验证。在票据就绪前仍正常转发。</p>
      <ul v-else class="state-models">
        <li v-for="model in status.models" :key="model.model">
          <strong>{{ model.model }} · {{ statusLabels[model.status] }}</strong>
          <span>{{ resultLabels[model.result] }}</span>
          <small>已探测 {{ model.attempts }} 轮<span v-if="model.expires_at"> · 票据到期 {{ new Date(model.expires_at).toLocaleString() }}</span></small>
          <small>下次探测 {{ new Date(model.next_attempt_at).toLocaleString() }}</small>
        </li>
      </ul>
      <div class="state-actions">
        <NButton size="small" :loading="loading" @click="load(false)">更新状态</NButton>
        <NButton size="small" :loading="loading" :disabled="!status.enabled || !status.supported || !status.models.some(model => model.result === 'ready')" @click="load(true)">重新验证</NButton>
      </div>
    </template>
    <p v-else-if="loading" role="status">正在读取状态…</p>
    <NButton v-if="error" size="small" @click="load(false)">重试</NButton>
  </section>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { NButton } from 'naive-ui'
import { Activity } from 'lucide-vue-next'
import { stateAPI } from '../api/codexState'
import type { CodexStateStatus } from '../types'
const props = defineProps<{ accountId: string }>()
const status = ref<CodexStateStatus | null>(null)
const error = ref('')
const loading = ref(false)
const statusLabels = { pending: '等待验证', ready: '票据可用', unavailable: '普通转发' }
const resultLabels: Record<string, string> = {
  account_busy: '账号并发或 RPM 已满，稍后重试', pending: '已登记模型', ready: '固定出口验证通过', probe_failed: '探测失败，稍后重试',
  identity_unavailable: '账号不可用或授权即将到期', identity_changed: '账号配置已变化',
  unexpected_state: '未获得符合套餐格式的票据', validation_rejected: '固定出口验证未通过',
  unauthorized: '上游要求重新授权', forbidden: '上游拒绝访问', rate_limited: '上游限流，已停止本轮',
  model_mismatch: '实际模型与目标不一致', incomplete_response: '探测响应未完整结束',
  transport_failed: '探测连接失败', upstream_rejected: '上游拒绝探测', state_312: '上游返回失效信号',
}
let generation = 0
async function load(refresh: boolean) {
  const current = ++generation
  loading.value = true
  error.value = ''
  try {
    const value = await (refresh ? stateAPI.refresh(props.accountId) : stateAPI.status(props.accountId))
    if (current === generation) status.value = value
  } catch (cause) {
    if (current === generation) error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current === generation) loading.value = false
  }
}
watch(() => props.accountId, () => { status.value = null; void load(false) }, { immediate: true })
</script>
<style scoped>
.state-models { list-style: none; padding: 0; display: grid; gap: 12px; }
.state-models li { display: grid; gap: 4px; overflow-wrap: anywhere; }
.state-models small, p { color: var(--muted); }
.state-actions { display: flex; flex-wrap: wrap; gap: 8px; }
</style>
