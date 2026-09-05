<template>
  <AuthLayout
    brand-caption="安全、简洁的邮件管理"
    intro-title="欢迎使用 Smail"
    intro-description="统一管理邮件、账户和安全设置，使用已配置的验证方式安全登录。"
  >
    <section class="login-content">
      <div class="login-form-box">
        <h2>{{ lang.login }}</h2>
        <p class="subtitle">{{ lang.login_subtitle }}</p>

        <el-form
          :model="form"
          class="auth-form"
          label-position="top"
          @keyup.enter="onSubmit"
        >
          <el-form-item :label="lang.account">
            <el-input
              v-model="form.account"
              :placeholder="lang.account"
              size="large"
              class="premium-input"
            />
          </el-form-item>

          <el-form-item :label="lang.password">
            <el-input
              v-model="form.password"
              :placeholder="lang.password"
              type="password"
              size="large"
              class="premium-input"
              show-password
            />
          </el-form-item>

          <div class="submit-action">
            <el-button
              type="primary"
              size="large"
              class="login-btn"
              :loading="loading"
              @click="onSubmit"
            >
              {{ lang.login }}
            </el-button>
          </div>

          <div class="forgot-password-action">
            <el-button
              link
              type="primary"
              class="forgot-password-btn"
              @click="goForgotPassword"
            >
              忘记密码？
            </el-button>
          </div>
        </el-form>
      </div>
    </section>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from '@/views/components/AuthLayout/AuthLayout.vue'
import { usePage } from './Login.js'

const {
  lang,
  form,
  onSubmit,
  loading,
  goForgotPassword
} = usePage()
</script>

<style scoped src="./Login.css"></style>
