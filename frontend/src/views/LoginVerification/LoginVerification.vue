<template>
  <AuthLayout
    brand-caption="安全、简洁的邮件管理"
    intro-title="完成安全验证"
    intro-description="密码验证通过后，还需要确认已绑定的外部安全邮箱或安全密钥。"
  >
    <section class="verification-content">
      <div class="verification-card">
        <div class="verification-heading">
          <span class="verification-eyebrow">登录保护</span>
          <h2>输入邮箱验证码</h2>
          <p>
            验证码已发送至
            <strong>{{ pending?.email || "已绑定邮箱" }}</strong>
          </p>
        </div>

        <el-form class="verification-form" @submit.prevent="verifyCode">
          <el-form-item label="邮箱验证码">
            <el-input
              v-model="code"
              maxlength="8"
              inputmode="numeric"
              autocomplete="one-time-code"
              placeholder="请输入验证码"
              size="large"
              class="verification-input"
            />
          </el-form-item>

          <el-alert
            v-if="errorMessage"
            :title="errorMessage"
            type="error"
            :closable="false"
            show-icon
          />

          <p class="verification-expiry">
            {{ remainingSeconds > 0
              ? `验证码将在 ${remainingSeconds} 秒后过期`
              : "验证码已过期，请重新登录" }}
          </p>

          <el-button
            type="primary"
            size="large"
            class="verification-submit"
            :loading="loading"
            :disabled="remainingSeconds <= 0"
            @click="verifyCode"
          >
            验证并登录
          </el-button>

          <div class="verification-actions">
            <el-button
              link
              type="primary"
              :loading="resending"
              :disabled="remainingSeconds > 540"
              @click="resendCode"
            >
              重新发送验证码
            </el-button>
            <el-button link @click="goBackToLogin">返回登录</el-button>
          </div>
        </el-form>

        <el-divider>或</el-divider>

        <el-button
          v-if="webAuthnAvailable"
          plain
          size="large"
          class="webauthn-login-button"
          :loading="webAuthnLoading"
          @click="verifyWithWebAuthn"
        >
          使用安全密钥验证
        </el-button>
        <p v-else class="webauthn-unavailable">
          当前浏览器不支持安全密钥验证，请使用邮箱验证码。
        </p>
      </div>
    </section>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from "@/views/components/AuthLayout/AuthLayout.vue"
import { usePage } from "./LoginVerification.js"

const {
  pending,
  code,
  loading,
  resending,
  webAuthnLoading,
  remainingSeconds,
  errorMessage,
  webAuthnAvailable,
  verifyCode,
  resendCode,
  verifyWithWebAuthn,
  goBackToLogin
} = usePage()
</script>

<style scoped src="./LoginVerification.css"></style>