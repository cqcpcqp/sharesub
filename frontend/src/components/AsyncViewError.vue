<template>
  <section class="async-view-error" role="alert">
    <span class="async-view-error-icon" aria-hidden="true"><TriangleAlert :size="20" /></span>
    <div>
      <strong>页面资源加载失败</strong>
      <p>请检查网络连接后刷新页面，重新加载当前工作区。</p>
      <NButton type="primary" secondary @click="reloadPage">
        <template #icon><RefreshCw :size="15" /></template>
        刷新页面
      </NButton>
    </div>
  </section>
</template>

<script setup lang="ts">
import { NButton } from 'naive-ui'
import { RefreshCw, TriangleAlert } from 'lucide-vue-next'

function reloadPage() {
  window.location.reload()
}
</script>

<style scoped>
.async-view-error {
  min-height: 320px;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  gap: 14px;
  padding-top: 96px;
}

.async-view-error-icon {
  width: 42px;
  height: 42px;
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid var(--danger-border);
  border-radius: 8px;
  background: var(--red-soft);
  color: var(--red);
}

.async-view-error > div {
  max-width: 380px;
}

.async-view-error strong {
  color: var(--ink-strong);
  font-size: 15px;
}

.async-view-error p {
  margin: 7px 0 16px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
}
</style>
