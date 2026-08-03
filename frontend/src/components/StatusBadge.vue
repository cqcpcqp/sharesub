<template><NTag class="status-badge" round size="small" :bordered="false" :type="tagType"><span class="status-dot" />{{ labels[value] }}</NTag></template>

<script setup lang="ts">
import { NTag } from 'naive-ui'
import { computed } from 'vue'

const props = defineProps<{ value: string }>()
const labels: Record<string, string> = {
  active: '正常', approved: '已批准', accepted: '已接受', pending: '待处理',
  rejected: '已拒绝', revoked: '已撤销', disabled: '已停用', refresh_required: '需重新授权',
  archived: '已归档', expired: '已过期', removed: '已移除', public: '公开', private: '私密',
}
const tagTypes: Record<string, 'success' | 'warning' | 'error' | 'info'> = {
  active: 'success', approved: 'success', accepted: 'success', public: 'success',
  pending: 'warning', refresh_required: 'warning', expired: 'warning', archived: 'info',
  rejected: 'error', revoked: 'error', disabled: 'error', removed: 'error',
  private: 'info',
}
const tagType = computed(() => tagTypes[props.value])
</script>
