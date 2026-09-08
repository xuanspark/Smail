<template>
  <section class="telegram-settings" aria-label="Telegram 推送设置">
    <header class="telegram-header">
      <div>
        <h4>Telegram 推送设置</h4>
        <p>只配置当前账户的邮件通知，聊天 ID 留空可停用推送。</p>
      </div>
      <a v-if="botLink" :href="botLink" target="_blank" rel="noopener noreferrer">联系机器人</a>
    </header>
    <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
    <el-form :model="settings" label-position="top" :disabled="!ready || !!working">
      <el-form-item label="Telegram 聊天 ID">
        <el-input v-model="settings.chatId" placeholder="填写聊天 ID，留空停用" maxlength="20" />
      </el-form-item>
      <el-form-item label="显示邮件内容">
        <el-switch v-model="settings.showContent" />
      </el-form-item>
      <el-form-item label="隐藏正文，点击后显示">
        <el-switch v-model="settings.spoilerContent" :disabled="!settings.showContent" />
      </el-form-item>
      <el-form-item label="发送附件">
        <el-switch v-model="settings.sendAttachments" />
      </el-form-item>
      <el-form-item label="禁用链接预览">
        <el-switch v-model="settings.disableLinkPreview" />
      </el-form-item>
    </el-form>
    <p class="telegram-hint">启用正文或附件后，相应内容会发送到 Telegram，请确认聊天对象和隐私需求。</p>
    <footer class="telegram-actions">
      <el-button :disabled="!!working" @click="refresh">重新加载</el-button>
      <el-button :disabled="!ready || !!working || !settings.chatId.trim()" @click="submit('test')">发送测试消息</el-button>
      <el-button type="primary" :disabled="!ready || !!working" @click="submit('save')">保存设置</el-button>
    </footer>
  </section>
</template>

<script setup>
import { useTelegramSettings } from './TelegramSettings.js'

const { settings, ready, working, errorMessage, botLink, refresh, submit, beforeLeave } = useTelegramSettings()

defineExpose({ beforeLeave })
</script>

<style scoped src="./TelegramSettings.css"></style>
