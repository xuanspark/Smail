<template>
  <section class="telegram-callback" aria-label="Telegram 授权回调">
    <header>
      <h4>个人绑定接入</h4>
      <el-tag :type="ready ? 'success' : 'info'">{{ checked ? ready ? '已配置' : '未就绪' : '待检查' }}</el-tag>
    </header>
    <p>先保存并启用系统机器人，再配置授权回调。超级管理员也需在“账号通知”中单独绑定。</p>
    <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
    <el-alert v-if="message" :title="message" :type="ready ? 'success' : 'warning'" :closable="false" show-icon />
    <div class="telegram-callback-actions">
      <el-button :disabled="unavailable || !!working" :loading="working === 'check'" @click="operate('check')">
        检查回调
      </el-button>
      <el-button type="primary" :disabled="unavailable || !!working" :loading="working === 'configure'" @click="operate('configure')">
        启用或更新回调
      </el-button>
    </div>
  </section>
</template>

<script setup>
import { useTelegramCallbackSettings } from './TelegramCallbackSettings.js'

const emit = defineEmits(['working'])
const { working, ready, checked, message, errorMessage, unavailable, operate } = useTelegramCallbackSettings(emit)
</script>

<style scoped src="./TelegramCallbackSettings.css"></style>
