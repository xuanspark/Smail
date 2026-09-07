<template>
  <ConfigEditor />
</template>

<script setup>
import { provide } from 'vue'
import { usePage } from '../ConfigSettings.js'
import ConfigEditor from '../ConfigEditor/ConfigEditor.vue'

const page = usePage('mail')
provide('smail-config-page', page)
defineExpose({ beforeLeave: page.beforeLeave })
</script>
