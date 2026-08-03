<template>
  <ModalShell v-if="show" title="使用 API 密钥" :subtitle="`${apiKey.name} · Codex CLI 与 OpenCode 配置`" wide @close="emit('close')">
    <div class="use-key-guide">
      <section class="connection-block credential-block">
        <header>
          <div><span>API KEY</span><strong>访问密钥</strong></div>
          <NButton quaternary class="icon-button" title="复制 API Key" aria-label="复制 API Key" @click="copy(apiKey.key)"><template #icon><Copy :size="17" /></template></NButton>
        </header>
        <code class="secret-value">{{ apiKey.key }}</code>
      </section>

      <NTabs v-model:value="client" type="line" animated class="usage-client-tabs">
        <NTabPane name="codex">
          <template #tab><span class="usage-tab-label"><SquareTerminal :size="16" />Codex CLI</span></template>
          <NTabs v-model:value="platform" type="segment" class="platform-tabs">
            <NTabPane name="unix" tab="macOS / Linux" />
            <NTabPane name="windows" tab="Windows" />
          </NTabs>
          <div class="config-file-list">
            <section v-for="file in codexFiles" :key="file.path" class="config-file">
              <header><code>{{ file.path }}</code><NButton quaternary class="icon-button" :title="`复制 ${file.path}`" :aria-label="`复制 ${file.path}`" @click="copy(file.content)"><template #icon><Copy :size="16" /></template></NButton></header>
              <pre><code>{{ file.content }}</code></pre>
            </section>
          </div>
        </NTabPane>

        <NTabPane name="opencode">
          <template #tab><span class="usage-tab-label"><Code2 :size="16" />OpenCode</span></template>
          <div class="config-file-list single">
            <section class="config-file">
              <header><code>{{ openCodeFile.path }}</code><NButton quaternary class="icon-button" title="复制 opencode.json" aria-label="复制 opencode.json" @click="copy(openCodeFile.content)"><template #icon><Copy :size="16" /></template></NButton></header>
              <pre><code>{{ openCodeFile.content }}</code></pre>
            </section>
          </div>
        </NTabPane>
      </NTabs>
    </div>

    <template #footer><NButton secondary @click="importToCCS"><template #icon><Upload :size="17" /></template>导入到 CCS</NButton><NButton @click="emit('close')">关闭</NButton></template>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NTabPane, NTabs } from 'naive-ui'
import { Code2, Copy, SquareTerminal, Upload } from 'lucide-vue-next'
import type { APIKey } from '../types'
import { buildCCSwitchImportDeepLink, codexConfigFiles, gatewayBaseURL, openCCSwitchImport, openCodeConfig } from '../keyUsage'
import ModalShell from './ModalShell.vue'

const props = defineProps<{ show: boolean; apiKey: APIKey }>()
const emit = defineEmits<{ close: []; message: [type: 'success' | 'error', text: string] }>()
const client = ref<'codex' | 'opencode'>('codex')
const platform = ref<'unix' | 'windows'>('unix')
const homepage = computed(() => window.location.origin.replace(/\/+$/, ''))
const baseURL = computed(() => gatewayBaseURL(homepage.value))
const codexFiles = computed(() => codexConfigFiles(baseURL.value, props.apiKey.key, platform.value))
const openCodeFile = computed(() => openCodeConfig(baseURL.value, props.apiKey.key))

function importToCCS() {
  const deepLink = buildCCSwitchImportDeepLink({ homepage: homepage.value, endpoint: baseURL.value, apiKey: props.apiKey.key, providerName: 'ShareSub' })
  if (!openCCSwitchImport(deepLink)) emit('message', 'error', '无法唤起 CC Switch，请确认已安装并允许浏览器打开外部应用')
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    emit('message', 'success', '已复制到剪贴板')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  }
}
</script>

<style scoped>
.use-key-guide {
  min-width: 0;
  display: grid;
  gap: 18px;
}

.credential-block {
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-soft);
}

.credential-block .secret-value {
  max-height: 84px;
  overflow: auto;
  background: var(--surface);
}

.usage-client-tabs :deep(.n-tabs-nav) {
  padding: 0 2px;
}

.usage-tab-label {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

.platform-tabs {
  margin-top: 14px;
}

.platform-tabs :deep(.n-tabs-rail) {
  width: min(360px, 100%);
}

.config-file-list {
  display: grid;
  gap: 12px;
  margin-top: 14px;
}

.config-file {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-soft);
}

.config-file > header {
  min-height: 42px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 4px 6px 4px 14px;
  border-bottom: 1px solid var(--line);
  background: var(--surface-hover);
}

.config-file > header code {
  color: var(--muted);
}

.config-file pre {
  max-height: 300px;
  overflow: auto;
  margin: 0;
  padding: 16px;
  color: var(--ink);
  font-size: 11px;
  line-height: 1.65;
}

.config-file pre code {
  white-space: pre;
}
</style>
