<template>
  <AuthLayout
    brand-caption="安全、简洁的邮件管理"
    intro-title="完成安全验证"
    intro-description="账号密码已通过验证。选择邮箱验证码或手机动态码，完成其中一种验证即可登录。"
  >
    <section class="verification-content">
      <div class="verification-card">
        <div class="verification-heading">
          <span class="verification-eyebrow">登录保护</span>
          <h2>选择验证方式</h2>
          <p>当前账号：<strong>{{ pending?.account }}</strong></p>
        </div>

        <div class="verification-methods">
          <el-tooltip
            :disabled="methods.includes('email') && !loading"
            :content="loading ? '正在验证，请稍候。' : '尚未绑定可用的安全邮箱，请使用已启用的手机动态码登录。'"
            placement="top"
            :show-after="150"
          >
            <span class="verification-method" :tabindex="!methods.includes('email') || loading ? 0 : undefined">
              <el-button
                :type="method === 'email' ? 'primary' : 'default'"
                :disabled="!methods.includes('email') || loading"
                :aria-pressed="method === 'email'"
                @click="chooseMethod('email')"
              >邮箱验证码</el-button>
            </span>
          </el-tooltip>
          <el-tooltip
            :disabled="methods.includes('totp') && !loading"
            :content="loading ? '正在验证，请稍候。' : '尚未启用手机动态码。请先使用邮箱验证码登录，再前往「安全中心 → 手机动态码」开启。'"
            placement="top"
            :show-after="150"
            popper-class="verification-method-tooltip"
          >
            <span class="verification-method" :tabindex="!methods.includes('totp') || loading ? 0 : undefined">
              <el-button
                :type="method === 'totp' ? 'primary' : 'default'"
                :disabled="!methods.includes('totp') || loading"
                :aria-pressed="method === 'totp'"
                @click="chooseMethod('totp')"
              >手机动态码（TOTP）</el-button>
            </span>
          </el-tooltip>
        </div>
        <p v-if="!method" class="verification-hint verification-placeholder">
          请选择一种验证方式，完成验证后即可登录。
        </p>

        <el-form v-if="method" class="verification-form" label-position="top" @submit.prevent="verifyCode">
          <div v-if="method === 'email'" class="verification-delivery">
            <div class="verification-delivery-row">
              <div class="verification-email">
                <span>绑定邮箱</span>
                <strong>{{ pending?.email }}</strong>
              </div>
              <el-button
                class="verification-send"
                :loading="resending"
                :disabled="loading || sending || retrySeconds > 0 || remainingSeconds <= 0"
                @click="resendCode"
              >{{ sending ? '正在发送' : retrySeconds > 0 ? `${retrySeconds} 秒后可重发` : '发送验证码' }}</el-button>
            </div>
            <p class="verification-delivery-status" aria-live="polite">{{ deliveryMessage }}</p>
          </div>
          <p v-else class="verification-hint">
            打开已绑定的手机验证器，输入当前六位动态码。已使用的动态码请等待更新。
          </p>

          <el-form-item :label="method === 'totp' ? '六位手机动态码' : '八位邮箱验证码'">
            <el-input
              v-model="code"
              :maxlength="method === 'totp' ? 6 : 8"
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

          <el-button
            type="primary"
            size="large"
            class="verification-submit"
            :loading="loading"
            :disabled="remainingSeconds <= 0 || (method === 'email' && pending?.deliveryStatus !== 'sent')"
            @click="verifyCode"
          >
            验证并登录
          </el-button>
          <p class="verification-expiry">
            {{ remainingSeconds > 0
              ? `请在 ${remainingSeconds} 秒内完成登录验证`
              : "登录验证已过期，请重新登录" }}
          </p>
        </el-form>
        <div class="verification-actions">
          <el-button link :disabled="loading" @click="goBackToLogin">返回登录</el-button>
        </div>
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
  method,
  methods,
  sending,
  retrySeconds,
  deliveryMessage,
  chooseMethod,
  remainingSeconds,
  errorMessage,
  verifyCode,
  resendCode,
  goBackToLogin
} = usePage()
</script>

<style scoped src="./LoginVerification.css"></style>
