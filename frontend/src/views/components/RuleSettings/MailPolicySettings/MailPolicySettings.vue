<template>
  <section class="mail-policy-settings" aria-label="邮件策略">
    <ConfigEditor />
  </section>
</template>

<script setup>
import { provide } from 'vue'
import { usePage } from '../../ConfigSettings/ConfigSettings.js'
import ConfigEditor from '../../ConfigSettings/ConfigEditor/ConfigEditor.vue'
import { mailPolicySection } from './MailPolicySettings.js'

const page = usePage(mailPolicySection)
provide('smail-config-page', page)

defineExpose({ beforeLeave: page.beforeLeave })
</script>

<style scoped src="./MailPolicySettings.css"></style>
