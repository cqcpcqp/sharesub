<template>
  <section class="membership-panel" aria-label="会员中心">
    <header class="membership-heading"><div><h2>会员中心</h2><p>会员跟随用户 · 不另收 Plan 服务费</p></div><NButton size="small" :loading="loading" :disabled="busy" @click="load">刷新</NButton></header>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NAlert v-if="message" type="success">{{ message }}</NAlert>
    <p v-if="loading && !membership" role="status">正在读取会员状态…</p>
    <template v-if="membership">
      <div class="membership-status"><strong>{{ membership.active ? tierLabel(membership.tier) : '暂无有效会员' }}</strong><span v-if="membership.expires_at">{{ membership.active ? '有效至' : '已到期于' }} {{ date(membership.expires_at) }}</span><span v-else>尚未开通</span></div>
      <p v-if="!membership.billing_started" class="membership-note">会员制尚未启用，现有使用不受影响。正式启用时，存量房主赠送 30 天 SVIP，其他有效成员赠送 30 天 VIP。</p>
      <p v-else-if="!membership.active" class="membership-note">会员已到期或尚未开通，仅暂停该用户的 API 调用；已有 Plan、数据和其他有效会员不受影响。</p>
      <dl class="membership-facts"><div><dt>未归档 Plan</dt><dd>{{ membership.owned_plans }} / {{ membership.owner_limit }}<span v-if="membership.billing_started && membership.owned_plans >= membership.owner_limit"> · 名额已用满，禁止新增</span></dd></div><div><dt>房主名额来源</dt><dd>{{ membership.owner_limit_override === null ? '套餐默认' : '个人配置' }}</dd></div><div><dt>权益来源</dt><dd>{{ sourceLabel(membership.source) }}</dd></div></dl>
      <p class="membership-note">会员制启用后，SVIP 默认可同时作为 2 个未归档 Plan 的房主，创建、接收转让及恢复时检查名额。到期保留已有 Plan 的必要管理权限，但不能新增 Plan 或成员。</p>
      <template v-if="!adminUserId">
        <div class="membership-products"><article><h3>VIP</h3><p class="membership-price">¥9.9 <small>/ 30 天</small></p><p>加入并使用已获授权的 Plan</p></article><article><h3>SVIP</h3><p class="membership-price">¥19.9 <small>/ 30 天</small></p><p>包含 VIP 权益，默认 2 个房主名额</p></article></div>
        <p class="membership-note">Plus、Pro 均需会员，不包含 OpenAI 订阅、账号或拼车席位。账号由房主提供；不自动扣款。</p>
        <p v-if="membership.active && membership.tier === 'vip'" class="membership-note">补 ¥10 升级只改变等级，到期时间不变，不增加 30 天。请确认剩余有效期后再购买。</p>
        <p v-if="!membership.payment_enabled" class="membership-note">在线收款未开启，请联系管理员。</p>
        <div class="membership-checkout"><NSelect v-model:value="method" :options="methods" aria-label="支付方式" :disabled="busy || !membership.payment_enabled" />
          <NPopconfirm v-for="product in products" :key="product.id" positive-text="确认并付款" negative-text="取消" @positive-click="checkout(product.id)"><template #trigger><NButton :type="product.id === 'upgrade' ? 'default' : 'primary'" :disabled="busy || !membership.payment_enabled">{{ product.label }}</NButton></template>
            <template v-if="product.id === 'upgrade'">补 ¥10 立即升级 SVIP，到期时间保持 {{ membership.expires_at ? date(membership.expires_at) : '' }} 不变，不增加 30 天。</template>
            <template v-else>购买 {{ product.id === 'vip' ? '¥9.9 VIP' : '¥19.9 SVIP' }}，有效期 30 天。提前同级续费接原到期时间延长；到期后从付款确认时起算。</template>
          </NPopconfirm>
        </div>
      </template>
      <details v-if="adminUserId" class="membership-disclosure" :open="reviewOrderID !== ''"><summary>调整会员与房主名额</summary>
        <div class="membership-admin-form">
          <NAlert v-if="reviewOrderID" type="warning">正在处理已到账订单 {{ reviewOrderID }}。请核对原权益，明确设置补偿后的等级与到期时间；保存后该订单标记为已处理。</NAlert>
          <label>会员等级<NSelect v-model:value="tier" :options="tiers" aria-label="会员等级" :disabled="busy" /></label>
          <label v-if="tier !== 'none'">有效期至<NDatePicker v-model:value="expiry" type="datetime" clearable placeholder="选择到期日期和时间" aria-label="会员到期时间" :disabled="busy" /></label>
          <label>个人房主数量上限<NInputNumber v-model:value="ownerLimit" :min="0" :max="10000" :precision="0" clearable placeholder="留空跟随套餐默认 2 个" aria-label="房主数量上限" :disabled="busy" /></label>
          <p class="membership-note">名额覆盖不授予永久 SVIP。调低上限不移除已有 Plan；VIP 到期后可重新选购 VIP 或 SVIP。管理员授权不生成付款订单，也不执行退款。</p>
          <label>调整原因<AppInput v-model:value="reason" :maxlength="500" placeholder="必填：赠送、补偿或调整原因" :disabled="busy" /></label>
          <NPopconfirm positive-text="确认调整" negative-text="取消" @positive-click="adjust"><template #trigger><NButton type="primary" :disabled="!canAdjust || busy">保存权益调整</NButton></template>当前 {{ tierLabel(membership.tier) }} → {{ tierLabel(tier) }}；到期时间 {{ tier === 'none' ? '清除' : expiry === null ? '未设置' : date(new Date(expiry).toISOString()) }}；房主上限 {{ ownerLimit === null ? '跟随默认 2 个' : ownerLimit }}。此操作会立即生效。</NPopconfirm>
          <NButton v-if="reviewOrderID" :disabled="busy" @click="reviewOrderID = ''">取消订单处理</NButton>
        </div>
      </details>
      <details class="membership-disclosure"><summary>付款记录 · {{ orders.length }} 笔</summary>
        <p v-if="orders.length === 0" class="membership-note">暂无付款记录。上线赠送和管理员授权不属于付费订单。</p>
        <ul class="membership-orders"><li v-for="order in orders" :key="order.id"><div><strong>{{ productLabel(order.product) }} · ¥{{ (order.amount_cents / 100).toFixed(2) }} · {{ statusLabel(order.status) }}</strong><small>{{ date(order.created_at) }} · {{ order.id }}</small><small v-if="order.service_expires_at">处理后有效至 {{ date(order.service_expires_at) }}</small><small v-if="order.status === 'review_required'">款项已确认，请勿重复付款。会员状态发生变化，需要管理员核对处理。</small></div><div class="membership-order-actions">
          <NButton v-if="order.status === 'pending' || order.status === 'expired'" size="small" :disabled="busy" @click="verify(order.id)">核实付款</NButton>
          <NPopconfirm v-if="!adminUserId && order.status === 'pending'" positive-text="取消本地订单" negative-text="返回" @positive-click="cancel(order.id)"><template #trigger><NButton size="small" :disabled="busy">取消订单</NButton></template>只取消本地待付状态，不保证关闭收银台交易。若已付款请先核实，不要重复支付。</NPopconfirm>
          <NButton v-if="adminUserId && order.status === 'review_required'" size="small" :disabled="busy" @click="reviewOrderID = order.id">处理已到账订单</NButton>
        </div></li></ul>
      </details>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { NAlert, NButton, NDatePicker, NInputNumber, NPopconfirm, NSelect } from 'naive-ui'
import { membershipAPI, type Membership, type MembershipOrder, type MembershipTier } from '../api/membership'
import AppInput from './AppInput.vue'

const props = withDefaults(defineProps<{ adminUserId?: string }>(), { adminUserId: '' })
const membership = ref<Membership | null>(null)
const orders = ref<MembershipOrder[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const message = ref('')
const method = ref<'alipay' | 'wxpay'>('alipay')
const tier = ref<MembershipTier>('none')
const expiry = ref<number | null>(null)
const ownerLimit = ref<number | null>(null)
const reason = ref('')
const reviewOrderID = ref('')
const methods = [{ label: '支付宝', value: 'alipay' }, { label: '微信支付', value: 'wxpay' }]
const tiers = [{ label: '无会员', value: 'none' }, { label: 'VIP', value: 'vip' }, { label: 'SVIP', value: 'svip' }]
const canAdjust = computed(() => !loading.value && reason.value.trim() !== '' && (tier.value === 'none' || expiry.value !== null))
const products = computed<{ id: MembershipOrder['product']; label: string }[]>(() => {
  if (!membership.value) return []
  if (membership.value.active && membership.value.tier === 'svip') return [{ id: 'svip', label: '续费 SVIP · ¥19.9' }]
  if (membership.value.active && membership.value.tier === 'vip') return [{ id: 'vip', label: '续费 VIP · ¥9.9' }, { id: 'upgrade', label: '升级 SVIP · 补 ¥10' }]
  return [{ id: 'vip', label: '开通 VIP · ¥9.9' }, { id: 'svip', label: '开通 SVIP · ¥19.9' }]
})
const tierLabel = (value: MembershipTier) => ({ none: '无会员', vip: 'VIP', svip: 'SVIP' })[value]
const sourceLabel = (value: Membership['source']) => ({ none: '未开通', rollout: '上线赠送', admin: '管理员调整', payment: '在线付款' })[value]
const productLabel = (value: MembershipOrder['product']) => ({ vip: 'VIP 30 天', svip: 'SVIP 30 天', upgrade: 'VIP 升级 SVIP' })[value]
const statusLabel = (value: MembershipOrder['status']) => ({ pending: '待付款', expired: '本地已取消或过期，已付款仍可核实', paid: '已到账并处理', review_required: '已到账，待人工处理' })[value]
const date = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
let disposed = false
let timer: ReturnType<typeof setInterval> | undefined
async function load() {
  if (loading.value) return
  loading.value = true
  try {
    const [value, records] = await Promise.all([membershipAPI.get(props.adminUserId), membershipAPI.orders(props.adminUserId)])
    if (disposed) return
    membership.value = value
    orders.value = records
    tier.value = value.tier
    expiry.value = value.expires_at === null ? null : Date.parse(value.expires_at)
    ownerLimit.value = value.owner_limit_override
    error.value = ''
  } catch (value) { if (!disposed) error.value = value instanceof Error ? value.message : String(value) }
  finally { loading.value = false }
}
async function action(run: () => Promise<void>) {
  if (busy.value || loading.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try { await run() } catch (value) { error.value = value instanceof Error ? value.message : String(value) }
  finally { busy.value = false }
}
async function checkout(product: MembershipOrder['product']) { await action(async () => { const value = await membershipAPI.checkout(product, method.value); if (!disposed) window.location.assign(value.pay_url) }) }
async function verify(id: string) { await action(async () => { const value = await membershipAPI.verify(id); await load(); if (value.status === 'pending' || value.status === 'expired') error.value = '支付平台尚未确认收款，请稍后核实，不要重复付款。' }) }
async function cancel(id: string) { await action(async () => { await membershipAPI.cancel(id); await load() }) }
async function adjust() {
  if (!membership.value || !canAdjust.value) return
  const input = { tier: tier.value, expires_at: tier.value === 'none' ? null : new Date(expiry.value!).toISOString(), owner_limit_override: ownerLimit.value, revision: membership.value.revision, reason: reason.value.trim(), review_order_id: reviewOrderID.value }
  await action(async () => { await membershipAPI.adjust(props.adminUserId, input); reason.value = ''; reviewOrderID.value = ''; await load(); message.value = '会员权益与房主名额已更新。' })
}
onMounted(() => { void load(); if (!props.adminUserId) timer = setInterval(() => { if (!document.hidden && !busy.value) void load() }, 15_000) })
onUnmounted(() => { disposed = true; if (timer) clearInterval(timer) })
</script>

<style scoped>
.membership-panel { display: grid; gap: 16px; min-width: 0; padding: 20px; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
.membership-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.membership-heading h2 { margin: 0; font-size: 18px; color: var(--ink-strong); }
.membership-heading p, .membership-note { margin: 5px 0 0; color: var(--muted); font-size: 12px; line-height: 1.75; }
.membership-status { display: flex; flex-wrap: wrap; align-items: baseline; gap: 12px; }
.membership-status strong { font-size: 20px; color: var(--primary); }
.membership-status span { font-size: 12px; color: var(--muted); }
.membership-facts { display: flex; flex-wrap: wrap; gap: 18px; margin: 0; }
.membership-facts dt { font-size: 11px; color: var(--muted); }.membership-facts dd { margin: 5px 0 0; font-size: 13px; }
.membership-products { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); border: 1px solid var(--line); border-radius: 8px; }
.membership-products article { padding: 16px; }.membership-products article + article { border-left: 1px solid var(--line); }
.membership-products h3 { margin: 0; font-size: 14px; }.membership-products p { font-size: 12px; color: var(--muted); }
.membership-products .membership-price { font-size: 26px; color: var(--ink-strong); margin: 10px 0; }.membership-price small { font-size: 12px; color: var(--muted); }
.membership-checkout, .membership-order-actions { display: flex; flex-wrap: wrap; gap: 8px; }.membership-checkout > .n-select { width: 120px; }
.membership-disclosure { border-top: 1px solid var(--line); padding-top: 14px; }.membership-disclosure summary { cursor: pointer; font-size: 13px; font-weight: 600; }
.membership-admin-form { display: grid; gap: 14px; padding-top: 16px; }.membership-admin-form label { display: grid; gap: 7px; font-size: 12px; }
.membership-orders { list-style: none; padding: 0; margin: 12px 0 0; }.membership-orders li { display: grid; gap: 10px; padding: 12px 0; border-bottom: 1px solid var(--line); }.membership-orders strong { font-size: 12px; }.membership-orders small { display: block; overflow-wrap: anywhere; font-size: 11px; color: var(--muted); margin-top: 5px; }
@media (max-width: 600px) { .membership-panel { padding: 14px; }.membership-products { grid-template-columns: 1fr; }.membership-products article + article { border-left: 0; border-top: 1px solid var(--line); } }
</style>
