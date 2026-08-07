<template>
  <NPopover
    trigger="click"
    placement="bottom-end"
    to="body"
    :show-arrow="false"
    :style="{ width: '292px', maxWidth: 'calc(100vw - 32px)' }"
  >
    <template #trigger>
      <NButton
        secondary
        class="support-trigger"
        title="售后客服"
        aria-label="售后客服"
      >
        <template #icon><Headphones :size="17" /></template>
        <span class="support-trigger-label">售后客服</span>
      </NButton>
    </template>

    <section class="support-panel" aria-label="售后客服联系方式">
      <header>
        <span class="support-panel-icon"><MessagesSquare :size="18" /></span>
        <div>
          <strong>售后客服</strong>
          <small>使用中遇到问题，欢迎进群沟通</small>
        </div>
      </header>
      <div class="support-contact">
        <span>QQ 群</span>
        <code>1095916809</code>
      </div>
    </section>
  </NPopover>
</template>

<script setup lang="ts">
import { NButton, NPopover } from 'naive-ui'
import { Headphones, MessagesSquare } from 'lucide-vue-next'
</script>

<style scoped>
.support-trigger.n-button {
  height: 44px;
  padding: 0 13px;
  border-radius: 7px;
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow-xs);
}

.support-trigger-label {
  font-size: 11px;
  font-weight: 700;
}

.support-panel {
  display: grid;
  gap: 17px;
  padding: 4px 2px 2px;
}

.support-panel > header {
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr);
  align-items: center;
  gap: 11px;
}

.support-panel-icon {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: var(--blue-soft);
  color: var(--blue);
}

.support-panel header > div {
  min-width: 0;
  display: grid;
  gap: 3px;
}

.support-panel header strong {
  color: var(--ink-strong);
  font-size: 12px;
}

.support-panel header small {
  color: var(--muted);
  font-size: 11px;
  line-height: 1.45;
}

.support-contact {
  min-height: 48px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 13px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-soft);
}

.support-contact span {
  color: var(--muted);
  font-size: 11px;
  font-weight: 700;
}

.support-contact code {
  color: var(--ink-strong);
  font-size: 13px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  user-select: all;
}

@media (max-width: 560px) {
  .support-trigger.n-button {
    width: 44px;
    min-width: 44px;
    padding: 0;
  }

  .support-trigger-label {
    display: none;
  }
}
</style>
