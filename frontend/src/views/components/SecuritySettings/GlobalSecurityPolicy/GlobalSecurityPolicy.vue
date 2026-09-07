<template>
  <div class="global-security-policy">
    <ConfigEditor />
  </div>
</template>

<script setup>
import { provide } from 'vue'
import { usePage } from '../../ConfigSettings/ConfigSettings.js'
import ConfigEditor from '../../ConfigSettings/ConfigEditor/ConfigEditor.vue'

const page = usePage('security_policy')
provide('smail-config-page', page)
defineExpose({ beforeLeave: page.beforeLeave })
</script>

<style scoped src="./GlobalSecurityPolicy.css"></style>
