<template>
  <section class="account-config-summary">
    <header class="section-heading">
      <div><span class="section-kicker">OPENAI ACCOUNT</span><h3>{{ account.name }}</h3><p>{{ account.notes || '暂无备注' }}</p></div>
      <StatusBadge :value="account.status" />
    </header>
    <dl class="account-config-list">
      <div><dt><Mail :size="15" />账号邮箱</dt><dd>{{ account.email }}</dd></div>
      <div><dt><Hash :size="15" />Account ID</dt><dd><code>{{ account.chatgpt_account_id }}</code></dd></div>
      <div><dt><Sparkles :size="15" />套餐类型</dt><dd>{{ account.plan_type }}</dd></div>
      <div><dt><CalendarClock :size="15" />Token 到期</dt><dd>{{ formatDate(account.token_expires_at) }}</dd></div>
      <div><dt><Network :size="15" />账号代理</dt><dd><code>{{ account.proxy_url || '继承系统代理' }}</code></dd></div>
      <div><dt><Gauge :size="15" />最大并发</dt><dd>{{ limitLabel(account.max_concurrency, '请求') }}</dd></div>
      <div><dt><TimerReset :size="15" />RPM 上限</dt><dd>{{ limitLabel(account.rpm_limit, '次/分钟') }}</dd></div>
      <div><dt><Fingerprint :size="15" />ShareSub ID</dt><dd><code>{{ account.id }}</code></dd></div>
    </dl>
    <section class="fast-policy-summary">
      <header>
        <div class="fast-policy-summary-title">
          <span><Zap :size="17" /></span>
          <div><strong>OpenAI Fast/Flex 策略</strong><small>仅作用于当前 OpenAI 账号</small></div>
        </div>
        <div class="fast-policy-summary-count"><strong>{{ account.fast_policy.length }}</strong><span>条规则</span></div>
      </header>
      <div v-if="account.fast_policy.length === 0" class="fast-policy-summary-empty">未配置策略，priority（fast）和 flex 请求将原样透传。</div>
      <div v-else class="fast-policy-summary-rules">
        <article v-for="(rule, index) in account.fast_policy" :key="index" class="fast-policy-summary-rule">
          <header>
            <div><span>{{ String(index + 1).padStart(2, '0') }}</span><strong>规则 {{ index + 1 }}</strong></div>
            <NTag size="small" round :bordered="false" :type="actionTagType(rule.action)">{{ actionLabel(rule.action) }}</NTag>
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
import { NAlert, NTag } from 'naive-ui'
import { ArrowRight, Boxes, CalendarClock, Fingerprint, Gauge, Hash, ListOrdered, Mail, Network, Sparkles, TimerReset, Users, Zap } from 'lucide-vue-next'
import type { Account, FastPolicyAction, FastPolicyTier, Member } from '../types'
import StatusBadge from './StatusBadge.vue'

const props = withDefaults(defineProps<{ account: Account; members?: Member[] }>(), { members: () => [] })
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function limitLabel(value: number, suffix: string) { return value === 0 ? '不限制' : `${value} ${suffix}` }
function tierLabel(value: FastPolicyTier) { return value === 'all' ? '全部 tier' : value === 'priority' ? 'priority（fast）' : 'flex' }
function actionLabel(value: FastPolicyAction) {
  return { pass: '透传', filter: '过滤 service_tier', force_priority: '强制 priority（fast）', block: '拦截请求' }[value]
}
function actionTagType(value: FastPolicyAction): 'default' | 'success' | 'warning' | 'error' {
  return { pass: 'success', filter: 'warning', force_priority: 'default', block: 'error' }[value] as 'default' | 'success' | 'warning' | 'error'
}
function memberLabel(userID: string) {
  const member = props.members.find(candidate => candidate.user_id === userID)
  return member ? `${member.username}${member.email ? ` · ${member.email}` : ''}` : userID
}
</script>
