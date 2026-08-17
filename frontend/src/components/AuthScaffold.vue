<template>
  <main class="auth-shell">
    <section class="auth-art" aria-hidden="true">
      <div class="auth-art-brand"><BrandMark :size="42" inverse /><strong>ShareSub</strong></div>
      <div class="auth-art-copy"><span>SHARE · ROUTE · CREATE</span><p>一起使用，<br />也各自清楚。</p></div>
      <div class="auth-art-blocks"><i /><i /><i /><i /></div>
    </section>
    <section class="auth-panel"><div class="auth-form-wrap"><slot /></div></section>
  </main>
</template>

<script setup lang="ts">
import BrandMark from './BrandMark.vue'
</script>
