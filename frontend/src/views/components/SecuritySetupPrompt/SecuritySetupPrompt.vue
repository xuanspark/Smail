<template>
  <el-dialog
    :model-value="visible"
    title="完善账户安全"
    width="min(560px, calc(100vw - 32px))"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    :show-close="false"
    class="security-setup-dialog"
  >
    <p class="setup-description">欢迎使用 Smail！首次登录请了解并设置以下安全选项，确认后不再自动弹出此引导。</p>
    <div class="setup-options">
      <section v-for="item in options" :key="item.key" class="setup-option">
        <div>
          <h3>{{ item.title }}</h3>
          <p>{{ item.description }}</p>
        </div>
        <el-button :disabled="saving" @click="acknowledge(item.key)">前往设置</el-button>
      </section>
    </div>
    <el-alert
      v-if="isSuperAdmin"
      title="超级管理员密码仍只能通过部署端受保护的本机通道重置，上述选项不会开放网页密码恢复。"
      type="info"
      :closable="false"
      show-icon
    />
    <el-alert
      v-else
      title="普通账号还可在安全设置中生成离线恢复码。恢复码只显示一次，请妥善保存。"
      type="info"
      :closable="false"
      show-icon
    />
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <template #footer>
      <el-button :loading="saving" @click="acknowledge()">我已了解，稍后设置</el-button>
      <el-button type="primary" :loading="saving" @click="acknowledge('question')">开始设置</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { usePage } from './SecuritySetupPrompt.js'

const { visible, isSuperAdmin, options, saving, error, acknowledge } = usePage()
</script>

<style scoped src="./SecuritySetupPrompt.css"></style>
