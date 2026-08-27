<template>
  <AuthLayout
    brand-caption="超级管理员密码重置"
    intro-title="重置超级管理员密码"
    intro-description="此页面仅用于系统中唯一的超级管理员，不适用于管理员或普通用户。"
  >
    <template #intro-extra>
      <el-alert
        title="正式重置必须由部署端 smail-admin 本机安全通道授权。"
        type="warning"
        :closable="false"
        show-icon
        class="reset-boundary"
      />
    </template>

    <section class="recovery-content">
      <div class="security-notice">
        <strong>安全密钥不需要输入</strong>
        <span>
          点击验证按钮后，按照浏览器提示触碰硬件安全密钥，或使用系统安全凭据完成验证。
        </span>
      </div>

      <div class="method-switcher">
        <div class="method-copy">
          <span class="method-label">当前验证方式</span>
          <strong>
            {{ recovery.method === 'webauthn' ? 'WebAuthn 安全密钥' : '外部邮箱验证码' }}
          </strong>
        </div>
        <el-button
          plain
          type="primary"
          class="switch-button secondary-action-btn"
          :disabled="loading"
          @click="switchRecoveryMethod"
        >
          切换验证方式
        </el-button>
      </div>

      <div
        v-if="recovery.method === 'webauthn'"
        class="verification-panel"
      >
        <h2>使用安全密钥验证</h2>
        <p>支持 USB、NFC、蓝牙安全密钥以及当前设备的系统凭据。</p>
        <el-button
          type="primary"
          class="recovery-action-btn"
          :loading="loading"
          :disabled="!securityKeySupported"
          @click="verifySecurityKey"
        >
          {{ securityKeySupported ? '验证安全密钥' : '当前环境不支持安全密钥' }}
        </el-button>
        <span v-if="verificationState" class="verification-state">
          {{ verificationState }}
        </span>
      </div>

      <div v-else class="verification-panel">
        <h2>使用外部邮箱验证码</h2>
        <p>验证码只发送到已经验证的超级管理员安全邮箱。</p>
        <el-form label-position="top">
          <el-form-item label="外部安全邮箱">
            <el-input
              v-model="recovery.email"
              type="email"
              autocomplete="email"
              placeholder="请输入外部安全邮箱"
            />
          </el-form-item>
          <div class="email-verification-row">
            <el-input
              v-model="recovery.code"
              inputmode="numeric"
              maxlength="12"
              autocomplete="one-time-code"
              placeholder="请输入邮箱验证码"
            />
            <el-button
              type="primary"
              class="recovery-action-btn"
              :loading="loading"
              @click="requestRecoveryCode"
            >
              发送验证码
            </el-button>
          </div>
          <span v-if="verificationState" class="verification-state">
            {{ verificationState }}
          </span>
        </el-form>
      </div>

      <el-form
        class="password-form"
        label-position="top"
        @submit.prevent="completeRecovery"
      >
        <el-form-item label="新密码">
          <el-input
            v-model="recovery.newPassword"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input
            v-model="recovery.confirmPassword"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <div class="actions">
          <el-button
            type="primary"
            class="recovery-action-btn"
            :loading="loading"
            :disabled="!verified"
            native-type="submit"
          >
            验证并重置密码
          </el-button>
        </div>
      </el-form>

      <el-button
        link
        type="primary"
        class="back-button"
        @click="goLogin"
      >
        返回登录
      </el-button>
    </section>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from '@/views/components/AuthLayout/AuthLayout.vue'
import { usePage } from './Recovery.js'

const {
  recovery,
  switchRecoveryMethod,
  loading,
  securityKeySupported,
  verifySecurityKey,
  verificationState,
  requestRecoveryCode,
  completeRecovery,
  verified,
  goLogin
} = usePage()
</script>

<style scoped src="./Recovery.css"></style>
