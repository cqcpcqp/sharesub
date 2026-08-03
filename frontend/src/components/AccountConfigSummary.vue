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
    <NAlert v-if="account.last_error" type="warning" :show-icon="true">{{ account.last_error }}</NAlert>
  </section>
</template>

<script setup lang="ts">
import { NAlert } from 'naive-ui'
import { CalendarClock, Fingerprint, Gauge, Hash, Mail, Network, Sparkles, TimerReset } from 'lucide-vue-next'
import type { Account } from '../types'
import StatusBadge from './StatusBadge.vue'

defineProps<{ account: Account }>()
function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function limitLabel(value: number, suffix: string) { return value === 0 ? '不限制' : `${value} ${suffix}` }
</script>
