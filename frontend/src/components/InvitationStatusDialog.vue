<template>
  <NModal :show="true" :mask-closable="false" :close-on-esc="false">
    <section class="modal invitation-status-dialog" role="dialog" aria-modal="true" aria-labelledby="invitation-status-title">
      <template v-if="accepting">
        <NSpin size="medium" />
        <div><h2 id="invitation-status-title">正在加入共享 Plan</h2><p>{{ preview ? `${preview.owner_username} 邀请你加入 ${preview.plan_name}` : '正在确认邀请并建立成员关系。' }}</p></div>
      </template>
      <template v-else>
        <header><div><span class="section-kicker">INVITATION</span><h2 id="invitation-status-title">暂时无法接受邀请</h2></div></header>
        <NAlert type="error">{{ error }}</NAlert>
        <div v-if="preview" class="invitation-summary"><span><Layers3 :size="20" /></span><div><strong>{{ preview.plan_name }}</strong><small>{{ preview.owner_username }} 发出的邀请 · 链接仅可使用一次</small></div></div>
        <p class="invitation-help">链接可能已过期、被撤销或已由其他用户领取。你也可以切换账号后重试。</p>
        <footer><NButton quaternary @click="emit('discard')">放弃邀请</NButton><NButton secondary @click="emit('switchAccount')">切换账号</NButton><NButton type="primary" @click="emit('retry')"><template #icon><RefreshCw :size="17" /></template>重试</NButton></footer>
      </template>
    </section>
  </NModal>
</template>

<script setup lang="ts">
import { NAlert, NButton, NModal, NSpin } from 'naive-ui'
import { Layers3, RefreshCw } from 'lucide-vue-next'
import type { InvitePreview } from '../types'

defineProps<{ accepting: boolean; error: string; preview: InvitePreview | null }>()
const emit = defineEmits<{ retry: []; switchAccount: []; discard: [] }>()
</script>
