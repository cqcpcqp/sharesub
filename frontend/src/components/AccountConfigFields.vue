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
      </div>
    </section>

    <section class="account-form-section fast-policy-section">
      <header><span><Zap :size="17" /></span><div><strong>OpenAI Fast/Flex 策略</strong><small>按顺序匹配请求体 service_tier；仅对当前账号生效</small></div></header>
      <div v-if="modelValue.fast_policy.length === 0" class="fast-policy-empty">尚未配置规则，priority（fast）和 flex 请求将原样透传。</div>
      <article v-for="(rule, index) in modelValue.fast_policy" :key="index" class="fast-policy-rule">
        <header><strong>规则 #{{ index + 1 }}</strong><NButton quaternary type="error" size="tiny" aria-label="删除规则" @click="removeRule(index)"><template #icon><Trash2 :size="15" /></template></NButton></header>
        <div class="fast-policy-grid">
          <label>service_tier 匹配<NSelect :value="rule.service_tier" :options="tierOptions" to="body" @update:value="updateRule(index, 'service_tier', $event)" /></label>
          <label>处理方式<NSelect :value="rule.action" :options="actionOptions" to="body" @update:value="updateRule(index, 'action', $event)" /></label>
          <label class="account-form-full">指定成员<small>留空表示该账号所在 Plan 的全部成员。</small><NSelect :value="rule.user_ids" multiple filterable clearable :options="policyUserOptions" :disabled="policyUserOptions.length === 0" :placeholder="policyUserOptions.length ? '选择 Plan 成员' : '账号尚未绑定 Plan'" to="body" @update:value="updateRule(index, 'user_ids', $event)" /></label>
          <label class="account-form-full">模型白名单<small>留空表示全部模型；支持精确匹配和末尾通配符，如 gpt-5.5*。</small><NSelect :value="rule.model_whitelist" multiple filterable tag clearable placeholder="输入模型后回车" to="body" @update:value="updateRule(index, 'model_whitelist', $event)" /></label>
          <label v-if="rule.action === 'block'" class="account-form-full">拦截消息<AppInput :value="rule.error_message" clearable :maxlength="500" placeholder="留空使用默认消息" @update:value="updateRule(index, 'error_message', $event)" /></label>
          <template v-if="rule.model_whitelist.length">
            <label>未匹配模型处理<NSelect :value="rule.fallback_action" :options="actionOptions" to="body" @update:value="updateRule(index, 'fallback_action', $event)" /></label>
            <label v-if="rule.fallback_action === 'block'">未匹配拦截消息<AppInput :value="rule.fallback_error_message" clearable :maxlength="500" placeholder="留空使用默认消息" @update:value="updateRule(index, 'fallback_error_message', $event)" /></label>
          </template>
        </div>
      </article>
      <NButton dashed class="fast-policy-add" @click="addRule"><template #icon><Plus :size="16" /></template>新增规则</NButton>
      <small class="fast-policy-hint">指定成员规则优先于全局规则；同组规则按从上到下首条命中。</small>
    </section>
  </div>
</template>

<script setup lang="ts">
import { NButton, NInputNumber, NSelect } from 'naive-ui'
import { BadgeInfo, Gauge, Plus, Trash2, Zap } from 'lucide-vue-next'
import { updateAccountText, type AccountTextField } from '../accountConfigForm'
import type { AccountConfigInput, AccountStatus, FastPolicyAction, FastPolicyRule, FastPolicyTier } from '../types'
import AppInput from './AppInput.vue'

const props = withDefaults(defineProps<{ modelValue: AccountConfigInput; showStatus?: boolean; policyUserOptions?: Array<{ label: string; value: string }> }>(), { policyUserOptions: () => [] })
const emit = defineEmits<{ 'update:modelValue': [value: AccountConfigInput] }>()

const statusOptions = [
  { label: '正常', value: 'active' },
  { label: '已停用', value: 'disabled' },
  { label: '需重新授权', value: 'refresh_required', disabled: true },
]

const tierOptions = [
  { label: '全部 tier', value: 'all' },
  { label: 'priority（fast）', value: 'priority' },
  { label: 'flex', value: 'flex' },
]
const actionOptions = [
  { label: '透传（保留 service_tier）', value: 'pass' },
  { label: '过滤（移除 service_tier）', value: 'filter' },
  { label: '强制设置 priority（fast）', value: 'force_priority' },
  { label: '拦截（拒绝请求）', value: 'block' },
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

function addRule() {
  const rule: FastPolicyRule = { service_tier: 'priority', action: 'filter', user_ids: [], error_message: '', model_whitelist: [], fallback_action: 'pass', fallback_error_message: '' }
  emit('update:modelValue', { ...props.modelValue, fast_policy: [...props.modelValue.fast_policy, rule] })
}

function removeRule(index: number) {
  emit('update:modelValue', { ...props.modelValue, fast_policy: props.modelValue.fast_policy.filter((_, ruleIndex) => ruleIndex !== index) })
}

function updateRule(index: number, field: keyof FastPolicyRule, value: string | string[] | FastPolicyTier | FastPolicyAction) {
  emit('update:modelValue', { ...props.modelValue, fast_policy: props.modelValue.fast_policy.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, [field]: value } : rule) })
}
</script>
