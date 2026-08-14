<template>
  <div class="account-config-form">
    <section class="account-form-section">
      <header><span><BadgeInfo :size="17" /></span><div><strong>基本信息</strong><small>用于识别和说明这个账号</small></div></header>
      <div class="account-form-grid">
        <label>账号名称<AppInput :value="modelValue.name" clearable :maxlength="100" placeholder="例如：团队主账号" @update:value="updateText('name', $event)" /></label>
        <label v-if="showStatus">调度状态<NSelect :value="modelValue.status" :options="statusOptions" to="body" @update:value="updateStatus" /></label>
        <label class="account-form-full">备注<AppInput :value="modelValue.notes" type="textarea" clearable :autosize="{ minRows: 3, maxRows: 6 }" :maxlength="2000" show-count placeholder="记录账号用途或注意事项" @update:value="updateText('notes', $event)" /></label>
      </div>
    </section>

    <section class="account-form-section">
      <header><span><Gauge :size="17" /></span><div><strong>网关策略</strong><small>0 表示不限制</small></div></header>
      <div class="account-form-grid">
        <label>最大并发<NInputNumber :value="modelValue.max_concurrency" :min="0" :max="100" :precision="0" @update:value="updateNumber('max_concurrency', $event)"><template #suffix>请求</template></NInputNumber></label>
        <label>RPM 上限<NInputNumber :value="modelValue.rpm_limit" :min="0" :max="10000" :precision="0" @update:value="updateNumber('rpm_limit', $event)"><template #suffix>次/分钟</template></NInputNumber></label>
        <label class="account-form-full">账号代理<AppInput :value="modelValue.proxy_url" type="password" clearable show-password-on="mousedown" placeholder="http://、https:// 或 socks5://" @update:value="updateText('proxy_url', $event)" /></label>
        <div class="account-form-full account-form-field">
          <label>Codex 指纹收敛<NSelect :value="modelValue.codex_fingerprint_mode" :options="codexFingerprintOptions" to="body" @update:value="updateFingerprintMode" /></label>
          <CodexFingerprintGuide :model-value="modelValue.codex_fingerprint_mode" />
        </div>
      </div>
    </section>

    <FastPolicyFields :model-value="modelValue.fast_policy" scope="account" :policy-user-options="policyUserOptions" @update:model-value="updateFastPolicy" />
  </div>
</template>

<script setup lang="ts">
import { NInputNumber, NSelect } from 'naive-ui'
import { BadgeInfo, Gauge } from 'lucide-vue-next'
import { updateAccountText, type AccountTextField } from '../accountConfigForm'
import { codexFingerprintOptions, type CodexFingerprintMode } from '../codexFingerprint'
import type { AccountConfigInput, AccountStatus, FastPolicyRule } from '../types'
import AppInput from './AppInput.vue'
import CodexFingerprintGuide from './CodexFingerprintGuide.vue'
import FastPolicyFields from './FastPolicyFields.vue'

const props = withDefaults(defineProps<{ modelValue: AccountConfigInput; showStatus?: boolean; policyUserOptions?: Array<{ label: string; value: string }> }>(), { policyUserOptions: () => [] })
const emit = defineEmits<{ 'update:modelValue': [value: AccountConfigInput] }>()

const statusOptions = [
  { label: '正常', value: 'active' },
  { label: '已停用', value: 'disabled' },
  { label: '需重新授权', value: 'refresh_required', disabled: true },
]

function updateText(field: AccountTextField, value: string) {
  emit('update:modelValue', updateAccountText(props.modelValue, field, value))
}

function updateNumber(field: 'max_concurrency' | 'rpm_limit', value: number | null) {
  emit('update:modelValue', { ...props.modelValue, [field]: value ?? 0 })
}

function updateStatus(value: AccountStatus) {
  emit('update:modelValue', { ...props.modelValue, status: value })
}

function updateFingerprintMode(value: CodexFingerprintMode) {
  emit('update:modelValue', { ...props.modelValue, codex_fingerprint_mode: value })
}

function updateFastPolicy(fastPolicy: FastPolicyRule[]) {
  emit('update:modelValue', { ...props.modelValue, fast_policy: fastPolicy })
}
</script>
