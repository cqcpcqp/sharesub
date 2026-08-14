<template>
  <section class="account-config-summary">
    <header class="section-heading">
      <div><h3>{{ account.name }}</h3><p>{{ account.notes || '暂无备注' }}</p></div>
      <StatusBadge :value="account.status" />
    </header>
    <dl class="account-config-list">
      <div><dt><Mail :size="15" />账号邮箱</dt><dd>{{ account.email }}</dd></div>
      <div><dt><Sparkles :size="15" />套餐类型</dt><dd>{{ account.plan_type }}</dd></div>
      <div><dt><CalendarRange :size="15" />订阅有效期至</dt><dd>{{ account.subscription_expires_at ? formatDate(account.subscription_expires_at) : '暂无订阅有效期' }}</dd></div>
      <div><dt><Network :size="15" />账号代理</dt><dd><code>{{ account.proxy_url || '继承系统代理' }}</code></dd></div>
      <div class="account-config-fingerprint">
        <dt><Fingerprint :size="15" />Codex 指纹收敛</dt>
        <dd><strong>{{ codexFingerprintMode(account.codex_fingerprint_mode).label }}</strong><CodexFingerprintGuide :model-value="account.codex_fingerprint_mode" /></dd>
      </div>
      <div>
        <dt>
          <Gauge :size="15" />
          <span>最大并发</span>
          <NTooltip placement="top" trigger="hover">
            <template #trigger>
              <button type="button" class="account-limit-help" aria-label="查看最大并发说明">
                <CircleHelp :size="13" />
              </button>
            </template>
            <span class="account-limit-help-copy">同一时刻允许通过此账号执行的请求数。流式请求在结束前会一直占用一个名额；0 表示不限制。</span>
          </NTooltip>
        </dt>
        <dd>{{ limitLabel(account.max_concurrency, '请求') }}</dd>
      </div>
      <div>
        <dt>
          <TimerReset :size="15" />
          <span>RPM 上限</span>
          <NTooltip placement="top" trigger="hover">
            <template #trigger>
              <button type="button" class="account-limit-help" aria-label="查看 RPM 上限说明">
                <CircleHelp :size="13" />
              </button>
            </template>
            <span class="account-limit-help-copy">此账号每个自然分钟最多可发起的请求数。达到上限后会尝试其他可用路由；0 表示不限制。</span>
          </NTooltip>
        </dt>
        <dd>{{ limitLabel(account.rpm_limit, '次/分钟') }}</dd>
      </div>
    </dl>
    <section class="fast-policy-summary">
      <header>
        <div class="fast-policy-summary-title">
          <span><Zap :size="17" /></span>
          <div><strong>OpenAI Fast/Flex 策略</strong><small>账号层优先于成员 API Key</small></div>
        </div>
        <div class="fast-policy-summary-count"><strong>{{ account.fast_policy.length }}</strong><span>条规则</span></div>
      </header>
      <div v-if="account.fast_policy.length === 0" class="fast-policy-summary-empty">未配置账号策略：交由成员 Key 规则处理；Key 也未配置时保留请求原选择。</div>
      <div v-else class="fast-policy-summary-rules">
        <article v-for="(rule, index) in account.fast_policy" :key="index" class="fast-policy-summary-rule">
          <header>
            <div class="fast-policy-summary-rule-title"><span>{{ String(index + 1).padStart(2, '0') }}</span><strong>规则 {{ index + 1 }}</strong></div>
            <NTag class="fast-policy-summary-action" size="small" round :bordered="false" :type="actionTagType(rule.action)">{{ actionLabel(rule.action) }}</NTag>
          </header>
          <div class="fast-policy-summary-body">
            <div class="fast-policy-summary-flow">
              <div><small>当请求匹配</small><strong>{{ tierLabel(rule.service_tier) }}</strong></div>
              <ArrowRight :size="17" />
              <div><small>执行操作</small><strong>{{ actionLabel(rule.action) }}</strong></div>
            </div>
            <dl class="fast-policy-summary-scope">
              <div>
                <dt><Users :size="14" />生效成员</dt>
                <dd class="fast-policy-tags"><NTag v-if="rule.user_ids.length === 0" size="small" :bordered="false">全部成员</NTag><template v-else><NTag v-for="userID in rule.user_ids" :key="userID" size="small" :bordered="false">{{ memberLabel(userID) }}</NTag></template></dd>
              </div>
              <div>
                <dt><Boxes :size="14" />适用模型</dt>
                <dd class="fast-policy-tags"><NTag v-if="rule.model_whitelist.length === 0" size="small" :bordered="false">全部模型</NTag><template v-else><NTag v-for="model in rule.model_whitelist" :key="model" size="small" :bordered="false"><code>{{ model }}</code></NTag></template></dd>
              </div>
            </dl>
          </div>
          <dl v-if="rule.action === 'block' || rule.model_whitelist.length" class="fast-policy-summary-details">
            <div v-if="rule.action === 'block'"><dt>拦截消息</dt><dd>{{ rule.error_message || '使用系统默认消息' }}</dd></div>
            <div v-if="rule.model_whitelist.length"><dt>模型未匹配时</dt><dd>{{ actionLabel(rule.fallback_action) }}</dd></div>
            <div v-if="rule.model_whitelist.length && rule.fallback_action === 'block'"><dt>未匹配拦截消息</dt><dd>{{ rule.fallback_error_message || '使用系统默认消息' }}</dd></div>
          </dl>
        </article>
      </div>
      <div v-if="account.fast_policy.length" class="fast-policy-summary-order"><ListOrdered :size="14" /><span>指定成员规则优先于全局规则，同组规则按从上到下首条命中</span></div>
    </section>
    <NAlert v-if="account.last_error" type="warning" :show-icon="true">{{ account.last_error }}</NAlert>
  </section>
</template>

<script setup lang="ts">
import { NAlert, NTag, NTooltip } from 'naive-ui'
import { ArrowRight, Boxes, CalendarRange, CircleHelp, Fingerprint, Gauge, ListOrdered, Mail, Network, Sparkles, TimerReset, Users, Zap } from 'lucide-vue-next'
import { codexFingerprintMode } from '../codexFingerprint'
import type { Account, FastPolicyAction, FastPolicyTier, Member } from '../types'
import CodexFingerprintGuide from './CodexFingerprintGuide.vue'
import StatusBadge from './StatusBadge.vue'

const props = withDefaults(defineProps<{ account: Account; members?: Member[] }>(), { members: () => [] })
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function limitLabel(value: number, suffix: string) { return value === 0 ? '不限制' : `${value} ${suffix}` }
function tierLabel(value: FastPolicyTier) { return value === 'all' ? '全部 Fast/Flex tier' : value === 'priority' ? 'Fast（含 priority）' : 'Flex' }
function actionLabel(value: FastPolicyAction) {
  return { pass: '透传到下一层', filter: '过滤 service_tier', force_priority: '强制 Fast', block: '拦截请求' }[value]
}
function actionTagType(value: FastPolicyAction): 'default' | 'success' | 'warning' | 'error' {
  return { pass: 'success', filter: 'warning', force_priority: 'default', block: 'error' }[value] as 'default' | 'success' | 'warning' | 'error'
}
function memberLabel(userID: string) {
  const member = props.members.find(candidate => candidate.user_id === userID)
  return member ? `${member.username}${member.email ? ` · ${member.email}` : ''}` : userID
}
</script>
