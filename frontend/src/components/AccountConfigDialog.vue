<template>
  <ModalShell
    v-if="account"
    title="编辑 OpenAI 账号"
    :subtitle="subtitle || account.email"
    :wide="true"
    @close="emit('close')"
  >
    <AccountConfigFields v-model="config" :show-status="true" :policy-user-options="policyUserOptions" />
    <template #footer>
      <div class="account-dialog-footer">
        <NButton secondary @click="emit('reauthorize', account)">
          <template #icon><RotateCw :size="17" /></template>
          重新授权
        </NButton>
        <div>
          <NButton @click="emit('close')">取消</NButton>
          <NButton type="primary" :loading="saving" :disabled="!config.name.trim()" @click="emit('save', { ...config })">
            <template #icon><Save :size="17" /></template>
            保存配置
          </NButton>
        </div>
      </div>
    </template>
  </ModalShell>
</template>

<script setup lang="ts">
import { NButton } from 'naive-ui'
import { RotateCw, Save } from 'lucide-vue-next'
import { ref, watch } from 'vue'
import type { Account, AccountConfigInput } from '../types'
import AccountConfigFields from './AccountConfigFields.vue'
import ModalShell from './ModalShell.vue'

const props = withDefaults(defineProps<{
  account: Account | null
  subtitle?: string
  policyUserOptions?: Array<{ label: string; value: string }>
  saving?: boolean
}>(), {
  subtitle: '',
  policyUserOptions: () => [],
  saving: false,
})
const emit = defineEmits<{
  close: []
  save: [config: AccountConfigInput]
  reauthorize: [account: Account]
}>()
const config = ref<AccountConfigInput>(emptyConfig())

function emptyConfig(): AccountConfigInput {
  return { name: '', notes: '', proxy_url: '', max_concurrency: 0, rpm_limit: 0, fast_policy: [], codex_fingerprint_mode: 'session', status: 'active' }
}

watch(() => props.account, (account) => {
  if (!account) {
    config.value = emptyConfig()
    return
  }
  config.value = {
    name: account.name,
    notes: account.notes,
    proxy_url: account.proxy_url,
    max_concurrency: account.max_concurrency,
    rpm_limit: account.rpm_limit,
    fast_policy: account.fast_policy.map(rule => ({ ...rule, user_ids: [...rule.user_ids], model_whitelist: [...rule.model_whitelist] })),
    codex_fingerprint_mode: account.codex_fingerprint_mode,
    status: account.status,
  }
}, { immediate: true })
</script>

<style scoped>
.account-dialog-footer { width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.account-dialog-footer > div { display: flex; gap: 8px; }
@media (max-width: 560px) {
  .account-dialog-footer { align-items: stretch; flex-direction: column-reverse; }
  .account-dialog-footer > div { display: grid; grid-template-columns: 1fr 1fr; }
  .account-dialog-footer > .n-button { width: 100%; }
}
</style>
