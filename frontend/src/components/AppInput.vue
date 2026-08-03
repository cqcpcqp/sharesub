<template>
  <NInput :value="innerValue" v-bind="$attrs" @update:value="handleUpdate">
    <template v-if="$slots.prefix" #prefix><slot name="prefix" /></template>
    <template v-if="$slots.suffix" #suffix><slot name="suffix" /></template>
  </NInput>
</template>

<script setup lang="ts">
import { NInput } from 'naive-ui'
import { ref, watch } from 'vue'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{ value?: string }>(), { value: '' })
const emit = defineEmits<{ 'update:value': [value: string] }>()
const innerValue = ref(props.value)

watch(() => props.value, value => {
  if (value !== innerValue.value) innerValue.value = value
})

function handleUpdate(value: string) {
  innerValue.value = value
  emit('update:value', value)
}
</script>
