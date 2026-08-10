<template>
  <section class="account-form-section fast-policy-section">
    <header><span><Zap :size="17" /></span><div><strong>OpenAI Fast/Flex 策略</strong><small>{{ scopeDescription }}</small></div></header>
    <div class="service-tier-guide">
      <p>{{ precedenceDescription }}</p>
      <dl>
        <div><dt>未指定 tier</dt><dd>命中“强制 Fast”时主动添加 service_tier；否则由下一层规则或 OpenAI 默认模式处理。</dd></div>
        <div><dt>Fast</dt><dd>使用最新官方值 fast；priority 作为兼容别名按同一模式匹配。</dd></div>
        <div><dt>Flex</dt><dd>响应更慢且可能暂时无可用资源，适合低优先级或异步任务；可用性取决于 OpenAI 和模型。</dd></div>
      </dl>
    </div>
    <div v-if="modelValue.length === 0" class="fast-policy-empty">{{ emptyDescription }}</div>
    <article v-for="(rule, index) in modelValue" :key="index" class="fast-policy-rule">
      <header><strong>规则 #{{ index + 1 }}</strong><NButton quaternary type="error" size="tiny" aria-label="删除规则" @click="removeRule(index)"><template #icon><Trash2 :size="15" /></template></NButton></header>
      <div class="fast-policy-grid">
        <label>service_tier 匹配<NSelect :value="rule.service_tier" :options="tierOptions" to="body" @update:value="updateRule(index, 'service_tier', $event)" /></label>
        <label>处理方式<NSelect :value="rule.action" :options="actionOptions" to="body" @update:value="updateRule(index, 'action', $event)" /></label>
        <label v-if="scope === 'account'" class="account-form-full">指定成员<small>留空表示该账号所在 Plan 的全部成员。</small><NSelect :value="rule.user_ids" multiple filterable clearable :options="policyUserOptions" :disabled="policyUserOptions.length === 0" :placeholder="policyUserOptions.length ? '选择 Plan 成员' : '账号尚未绑定 Plan'" to="body" @update:value="updateRule(index, 'user_ids', $event)" /></label>
        <label class="account-form-full">模型白名单<small>留空表示全部模型；支持精确匹配和末尾通配符，如 gpt-5.6*。</small><NSelect :value="rule.model_whitelist" multiple filterable tag clearable placeholder="输入模型后回车" to="body" @update:value="updateRule(index, 'model_whitelist', $event)" /></label>
        <label v-if="rule.action === 'block'" class="account-form-full">拦截消息<AppInput :value="rule.error_message" clearable :maxlength="500" placeholder="留空使用默认消息" @update:value="updateRule(index, 'error_message', $event)" /></label>
        <template v-if="rule.model_whitelist.length">
          <label>未匹配模型处理<NSelect :value="rule.fallback_action" :options="actionOptions" to="body" @update:value="updateRule(index, 'fallback_action', $event)" /></label>
          <label v-if="rule.fallback_action === 'block'">未匹配拦截消息<AppInput :value="rule.fallback_error_message" clearable :maxlength="500" placeholder="留空使用默认消息" @update:value="updateRule(index, 'fallback_error_message', $event)" /></label>
        </template>
      </div>
    </article>
    <NButton dashed class="fast-policy-add" @click="addRule"><template #icon><Plus :size="16" /></template>新增规则</NButton>
    <small class="fast-policy-hint">{{ orderDescription }}</small>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NSelect } from 'naive-ui'
import { Plus, Trash2, Zap } from 'lucide-vue-next'
import type { FastPolicyAction, FastPolicyRule, FastPolicyTier } from '../types'
import AppInput from './AppInput.vue'

const props = withDefaults(defineProps<{
  modelValue: FastPolicyRule[]
  scope: 'account' | 'key'
  policyUserOptions?: Array<{ label: string; value: string }>
}>(), { policyUserOptions: () => [] })
const emit = defineEmits<{ 'update:modelValue': [value: FastPolicyRule[]] }>()

const scopeDescription = computed(() => props.scope === 'account' ? '账号规则优先于成员自己的 Key 规则' : '仅在绑定账号透传或未命中时生效')
const precedenceDescription = computed(() => props.scope === 'account'
  ? '过滤、强制 Fast 和拦截是账号层最终决定；规则为空、未命中或透传时，继续执行成员 Key 的规则。'
  : '绑定账号的规则优先；账号规则为空、未命中或透传后，才执行这里的 Key 规则。')
const emptyDescription = computed(() => props.scope === 'account'
  ? '未配置账号规则：交由成员 Key 规则处理；Key 也未配置时保留请求原选择。'
  : '未配置 Key 规则：保留客户端原始 service_tier；未携带时交由 OpenAI 默认处理。')
const orderDescription = computed(() => props.scope === 'account'
  ? '指定成员规则优先于全局规则；同组规则按从上到下首条命中。'
  : '规则按从上到下首条命中；账号层的最终决定始终优先。')

const tierOptions = [
  { label: '全部 Fast/Flex tier', value: 'all' },
  { label: 'Fast（含 priority 别名）', value: 'priority' },
  { label: 'Flex', value: 'flex' },
]
const actionOptions = [
  { label: '透传（交给下一层）', value: 'pass' },
  { label: '过滤（移除 service_tier）', value: 'filter' },
  { label: '强制设置 Fast', value: 'force_priority' },
  { label: '拦截（拒绝请求）', value: 'block' },
]

function addRule() {
  const action: FastPolicyAction = props.scope === 'account' ? 'filter' : 'force_priority'
  const rule: FastPolicyRule = { service_tier: 'priority', action, user_ids: [], error_message: '', model_whitelist: [], fallback_action: 'pass', fallback_error_message: '' }
  emit('update:modelValue', [...props.modelValue, rule])
}

function removeRule(index: number) {
  emit('update:modelValue', props.modelValue.filter((_, ruleIndex) => ruleIndex !== index))
}

function updateRule(index: number, field: keyof FastPolicyRule, value: string | string[] | FastPolicyTier | FastPolicyAction) {
  emit('update:modelValue', props.modelValue.map((rule, ruleIndex) => ruleIndex === index ? { ...rule, [field]: value } : rule))
}
</script>
