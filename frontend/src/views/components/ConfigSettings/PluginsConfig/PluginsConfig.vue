<template>
  <ConfigEditor />
</template>

<script setup>
import { provide } from 'vue'
import { usePage } from '../ConfigSettings.js'
import ConfigEditor from '../ConfigEditor/ConfigEditor.vue'

const props = defineProps({ pageKey: { type: String, required: true } })
const page = usePage(props.pageKey)
provide('smail-config-page', page)
defineExpose({ beforeLeave: page.beforeLeave })
</script>
