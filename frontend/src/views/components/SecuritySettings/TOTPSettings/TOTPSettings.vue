<template>
  <section class="totp-settings" v-loading="busy">
    <div class="totp-heading">
      <h3>手机动态码（TOTP）</h3>
      <el-tag v-if="ready" :type="enrollment ? 'warning' : enabled ? 'success' : 'info'">
        {{ enrollment ? '待确认' : enabled ? '已启用' : '未启用' }}
      </el-tag>
    </div>
    <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
    <el-button v-if="!ready" :disabled="busy" @click="refresh">重新读取状态</el-button>
    <template v-else>
      <div v-if="enrollment" class="totp-enrollment">
        <img v-if="qrCode" :src="qrCode" alt="手机验证器绑定二维码" width="240" height="240" />
        <details class="totp-manual">
          <summary>手动绑定</summary>
          <code>{{ enrollment.secret }}</code>
          <p>SHA256 · 6 位 · 30 秒</p>
        </details>
        <el-form label-position="top" @submit.prevent="perform('confirm')">
          <el-form-item label="手机动态码">
            <el-input v-model="code" maxlength="6" inputmode="numeric" autocomplete="one-time-code" placeholder="六位动态码" />
          </el-form-item>
          <div class="totp-actions">
            <el-button type="primary" :disabled="busy" @click="perform('confirm')">确认启用</el-button>
            <el-button :disabled="busy" @click="perform('cancel')">取消绑定</el-button>
          </div>
        </el-form>
      </div>
      <el-form v-else-if="enabled" class="totp-disable" label-position="top" @submit.prevent="perform('disable')">
        <el-form-item label="手机动态码">
          <el-input v-model="code" maxlength="6" inputmode="numeric" autocomplete="one-time-code" placeholder="六位动态码" />
        </el-form-item>
        <el-button type="danger" plain :disabled="busy" @click="perform('disable')">关闭手机动态码</el-button>
      </el-form>
      <div v-else class="totp-actions">
        <el-button type="primary" :disabled="busy" @click="perform('begin')">绑定手机验证器</el-button>
      </div>
    </template>
  </section>
</template>

<script setup>
import { useTOTPSettings } from './TOTPSettings.js'

const { enabled, ready, busy, enrollment, qrCode, code, errorMessage, perform, refresh } = useTOTPSettings()
</script>

<style scoped src="./TOTPSettings.css"></style>
