<template>
  <div class="settings-page-status" :role="error ? 'alert' : 'status'">
    {{ error ? '设置页面加载失败，请刷新后重试。' : '正在加载设置页面…' }}
  </div>
</template>

<script setup>
defineProps({ error: { type: Error, default: null } })
</script>

<style scoped src="./SettingsPageStatus.css"></style>
