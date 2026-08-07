<template>
  <section class="invitation-summary-card" aria-label="邀请详情">
    <header>
      <span><Layers3 :size="19" /></span>
      <div><strong>{{ preview.plan_name }}</strong><small>{{ preview.owner_username }} 邀请你加入</small></div>
    </header>
    <dl>
      <div><dt>额度方式</dt><dd>{{ allocationModeLabel(preview.allocation_mode) }}</dd></div>
      <div><dt>你的额度</dt><dd>{{ memberAccessLabel }}</dd></div>
      <div><dt>有效期至</dt><dd>{{ expiresAt }}</dd></div>
    </dl>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Layers3 } from 'lucide-vue-next'
import { allocationModeLabel, formatShareBasisPoints } from '../planAllocation'
import type { InvitePreview } from '../types'

const props = defineProps<{ preview: InvitePreview }>()
const expiresAt = computed(() => new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
}).format(new Date(props.preview.expires_at)))
const memberAccessLabel = computed(() => props.preview.allocation_mode === 'shared'
  ? '共享账号总额度'
  : props.preview.share_basis_points === 0
    ? '仅查看，不能发起请求'
    : formatShareBasisPoints(props.preview.share_basis_points))
</script>

<style scoped>
.invitation-summary-card { min-width: 0; display: grid; gap: 13px; padding: 14px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-soft); }
.invitation-summary-card > header { min-width: 0; display: grid; grid-template-columns: 36px minmax(0, 1fr); align-items: center; gap: 10px; }
.invitation-summary-card > header > span { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 7px; background: var(--teal-soft); color: var(--teal); }
.invitation-summary-card > header > div { min-width: 0; display: grid; gap: 3px; }
.invitation-summary-card strong, .invitation-summary-card small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.invitation-summary-card strong { color: var(--ink-strong); font-size: 12px; }
.invitation-summary-card small { color: var(--muted); font-size: 11px; }
.invitation-summary-card dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; border-top: 1px solid var(--line-soft); }
.invitation-summary-card dl > div { min-width: 0; display: grid; align-content: start; gap: 4px; padding: 11px 10px 0; border-left: 1px solid var(--line-soft); }
.invitation-summary-card dl > div:first-child { padding-left: 0; border-left: 0; }
.invitation-summary-card dl > div:last-child { padding-right: 0; }
.invitation-summary-card dt { color: var(--muted-light); font-size: 10px; font-weight: 750; }
.invitation-summary-card dd { min-width: 0; margin: 0; color: var(--ink); font-size: 11px; line-height: 1.45; overflow-wrap: anywhere; }
@media (max-width: 520px) {
  .invitation-summary-card dl { grid-template-columns: 1fr; }
  .invitation-summary-card dl > div, .invitation-summary-card dl > div:first-child, .invitation-summary-card dl > div:last-child { grid-template-columns: 78px minmax(0, 1fr); padding: 9px 0; border-left: 0; border-bottom: 1px solid var(--line-soft); }
  .invitation-summary-card dl > div:last-child { padding-bottom: 0; border-bottom: 0; }
}
</style>
