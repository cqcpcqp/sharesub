<template>
  <section class="async-view-state" role="status" aria-live="polite">
    <NSpin size="medium" />
    <div>
      <strong>正在加载工作区</strong>
      <span>正在准备页面资源，请稍候。</span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { NSpin } from 'naive-ui'
</script>

<style scoped>
.async-view-state {
  min-height: 320px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--muted);
}

.async-view-state > div {
  display: grid;
  gap: 4px;
}

.async-view-state strong {
  color: var(--ink-strong);
  font-size: 13px;
}

.async-view-state span {
  font-size: 11px;
}
</style>
