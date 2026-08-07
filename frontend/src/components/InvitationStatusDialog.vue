<template>
  <NModal :show="true" :mask-closable="false" :close-on-esc="false">
    <section class="modal invitation-status-dialog" role="dialog" aria-modal="true" aria-labelledby="invitation-status-title">
      <template v-if="loading">
        <NSpin size="medium" />
        <div><h2 id="invitation-status-title">正在读取邀请</h2><p>确认链接状态和加入条件。</p></div>
      </template>
      <template v-else-if="accepting">
        <NSpin size="medium" />
        <div><h2 id="invitation-status-title">正在加入共享 Plan</h2><p>{{ preview ? `${preview.owner_username} 邀请你加入 ${preview.plan_name}` : '正在建立成员关系。' }}</p></div>
      </template>
      <template v-else-if="error">
        <header><div><h2 id="invitation-status-title">暂时无法接受邀请</h2></div></header>
        <NAlert type="error">{{ error }}</NAlert>
        <InvitationSummary v-if="preview" :preview="preview" />
        <p class="invitation-help">链接可能已过期、被撤销或已由其他用户领取。你也可以切换账号后重试。</p>
        <footer><NButton quaternary @click="emit('discard')">放弃邀请</NButton><NButton secondary @click="emit('switchAccount')">切换账号</NButton><NButton type="primary" @click="emit('retry')"><template #icon><RefreshCw :size="17" /></template>重试</NButton></footer>
      </template>
      <template v-else-if="preview">
        <header><div><h2 id="invitation-status-title">确认加入 Plan</h2><p>这条链接只能领取一次，请确认加入条件和当前账号。</p></div></header>
        <InvitationSummary :preview="preview" />
        <section class="invitation-account" aria-label="当前加入账号">
          <UserAvatar :size="38" :username="user.username" :src="user.avatar_url" />
          <div><small>将使用此账号加入</small><strong>{{ user.username }}</strong><span>{{ user.email }}</span></div>
          <NButton text type="primary" @click="emit('switchAccount')">切换账号</NButton>
        </section>
        <footer><NButton quaternary @click="emit('discard')">放弃邀请</NButton><NButton type="primary" @click="emit('accept')">确认加入</NButton></footer>
      </template>
    </section>
  </NModal>
</template>

<script setup lang="ts">
import { NAlert, NButton, NModal, NSpin } from 'naive-ui'
import { RefreshCw } from 'lucide-vue-next'
import InvitationSummary from './InvitationSummary.vue'
import UserAvatar from './UserAvatar.vue'
import type { InvitePreview, User } from '../types'

defineProps<{ loading: boolean; accepting: boolean; error: string; preview: InvitePreview | null; user: User }>()
const emit = defineEmits<{ accept: []; retry: []; switchAccount: []; discard: [] }>()
</script>
