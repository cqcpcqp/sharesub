<template>
  <ModalShell title="易支付设置" subtitle="个人会员 · VIP ¥9.9 / SVIP ¥19.9 · 30 天" :closable="!busy" @close="close">
    <div class="payment-settings-form">
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <NAlert v-if="success" type="success">{{ success }}</NAlert>
      <p v-if="loading" role="status">正在读取支付配置…</p>
      <template v-if="settings">
        <div class="payment-settings-status">
          <strong>{{ settings.enabled ? '在线收款已开启' : '在线收款未开启' }}</strong>
          <span>{{ settings.started_at ? `收费启用于 ${new Date(settings.started_at).toLocaleString()}` : '尚未启用收费，存量会员赠送尚未开始。' }}</span>
        </div>
        <label>网关地址<AppInput v-model:value="baseURL" placeholder="https://你的易支付网关域名" :disabled="busy" /></label>
        <label>商户 ID<AppInput v-model:value="pid" placeholder="易支付商户 PID" :disabled="busy" /></label>
        <label>商户密钥<AppInput v-model:value="key" type="password" autocomplete="new-password" :placeholder="settings.key_configured ? '已保存密钥；留空保持不变' : '填写易支付商户密钥'" :disabled="busy" /></label>
        <p class="payment-settings-hint">密钥加密保存，不回显。配置保存在数据库，保存后即时生效，无需重启服务。</p>
        <div class="payment-settings-callback"><strong>异步通知地址</strong><code>{{ settings.public_url }}/api/payment/easypay/notify</code><span>下单时自动发送给支付平台，无需修改 sub2api 配置。</span></div>
        <NAlert v-if="!settings.callback_ready" type="warning">当前站点地址不是 HTTPS，暂不能启用收款。请先由运维配置公开 HTTPS 站点地址，并确认支付平台可访问回调。</NAlert>
        <NAlert v-if="settings.started_at" type="info">停用在线收款只停止新下单，不取消会员门禁、不重复赠送；已付款订单和服务权益保持不变。</NAlert>
        <p v-else class="payment-settings-hint">保存配置不会开始收费。点击“启用收款”并确认后，存量房主获赠 30 天 SVIP，其他有效成员获赠 30 天 VIP；超过 2 个 Plan 的房主保留个人名额。</p>
        <NAlert v-if="confirming" type="warning" title="确认启用在线收款？">
          {{ settings.started_at ? '恢复用户在线开通和续费，不会再次赠送会员。' : '立即正式启用全员会员制（包含 Plus 和 Pro），并开始计算存量用户获赠的 30 天会员。启用后无法通过关闭收款撤销收费门禁。' }}
          <div class="payment-settings-confirm"><NButton :disabled="busy" @click="confirming = false">取消</NButton><NButton type="warning" :loading="busy" @click="save(true)">确认启用</NButton></div>
        </NAlert>
      </template>
    </div>
    <template #footer>
      <NButton :disabled="busy" @click="close">关闭</NButton>
      <NButton v-if="!settings && !loading" @click="load">重试</NButton>
      <template v-if="settings">
        <NButton v-if="settings.enabled" :disabled="busy" @click="save(false)">停用在线收款</NButton>
        <NButton type="primary" :disabled="!canSave || confirming" :loading="busy" @click="save(settings.enabled)">保存配置</NButton>
        <NButton v-if="!settings.enabled" :disabled="!canSave || !settings.callback_ready || busy || confirming" @click="confirming = true">启用收款</NButton>
      </template>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton } from 'naive-ui'
import { paymentSettingsAPI, type PaymentSettings } from '../api/paymentSettings'
import AppInput from './AppInput.vue'
import ModalShell from './ModalShell.vue'

const emit = defineEmits<{ close: [] }>()
const settings = ref<PaymentSettings | null>(null)
const baseURL = ref('')
const pid = ref('')
const key = ref('')
const loading = ref(false)
const busy = ref(false)
const confirming = ref(false)
const error = ref('')
const success = ref('')
const canSave = computed(() => baseURL.value.trim() !== '' && pid.value.trim() !== '' && (key.value.trim() !== '' || settings.value?.key_configured === true))

function applySettings(value: PaymentSettings) {
  settings.value = value
  baseURL.value = value.base_url
  pid.value = value.pid
  key.value = ''
}
function close() { if (!busy.value) emit('close') }
async function load() {
  loading.value = true
  error.value = ''
  try { applySettings(await paymentSettingsAPI.get()) }
  catch (value) { error.value = value instanceof Error ? value.message : String(value) }
  finally { loading.value = false }
}
async function save(enabled: boolean) {
  if (!settings.value || busy.value) return
  busy.value = true
  error.value = ''
  success.value = ''
  try {
    applySettings(await paymentSettingsAPI.save({ base_url: baseURL.value.trim(), pid: pid.value.trim(), key: key.value, enabled, revision: settings.value.revision }))
    confirming.value = false
    success.value = enabled ? '配置已保存，在线收款已生效。' : '配置已保存，在线收款未开启。'
  } catch (value) { error.value = value instanceof Error ? value.message : String(value) }
  finally { busy.value = false }
}
onMounted(load)
</script>

<style scoped>
.payment-settings-form { display: grid; gap: 16px; padding-top: 16px; }
.payment-settings-form label { display: grid; gap: 7px; font-size: 12px; font-weight: 600; color: var(--ink); }
.payment-settings-status, .payment-settings-callback { display: grid; gap: 6px; }
.payment-settings-status span, .payment-settings-callback span, .payment-settings-hint { color: var(--muted); font-size: 12px; line-height: 1.7; margin: 0; }
.payment-settings-callback { padding: 12px; border: 1px solid var(--line); border-radius: 8px; font-size: 12px; }
.payment-settings-callback code { overflow-wrap: anywhere; }
.payment-settings-confirm { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
</style>
