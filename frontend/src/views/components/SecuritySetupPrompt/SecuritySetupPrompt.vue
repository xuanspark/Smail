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
    <p class="setup-description">{{ isSuperAdmin ? '请绑定外部邮箱。未完成前，每次登录都会提醒。' : '请设置安全码并绑定外部邮箱。未完成前，每次登录都会提醒。' }}</p>
    <div class="setup-options">
      <section v-for="item in options" :key="item.key" class="setup-option">
        <div>
          <h3>{{ item.title }}</h3>
          <p>{{ item.description }}</p>
        </div>
        <el-button :disabled="saving" @click="acknowledge(item.key)">前往设置</el-button>
      </section>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <template #footer>
      <el-button :loading="saving" @click="acknowledge()">我知道了</el-button>
      <el-button type="primary" :loading="saving" @click="acknowledge(options[0]?.key)">去设置</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { usePage } from './SecuritySetupPrompt.js'

const { visible, isSuperAdmin, options, saving, error, acknowledge } = usePage()
</script>

<style scoped src="./SecuritySetupPrompt.css"></style>
