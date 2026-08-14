<template>
  <div class="codex-fingerprint-guide">
    <p>{{ currentMode.description }}</p>
    <details>
      <summary><CircleHelp :size="14" /><span>查看四种模式区别</span></summary>
      <div class="codex-fingerprint-guide-content">
        <p>“收敛”表示统一上游看到的 Codex 标识，不会改变账号、模型或额度。</p>
        <dl>
          <div v-for="mode in codexFingerprintModes" :key="mode.value" :class="{ 'is-current': mode.value === modelValue }">
            <dt>
              <span>{{ mode.label }}</span>
              <small v-if="mode.value === modelValue">当前选择</small>
            </dt>
            <dd>{{ mode.description }}</dd>
          </div>
        </dl>
      </div>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CircleHelp } from 'lucide-vue-next'
import { codexFingerprintMode, codexFingerprintModes, type CodexFingerprintMode } from '../codexFingerprint'

const props = defineProps<{ modelValue: CodexFingerprintMode }>()
const currentMode = computed(() => codexFingerprintMode(props.modelValue))
</script>
