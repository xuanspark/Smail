<template>
  <AuthLayout
    brand-caption="账号密码恢复"
    intro-title="恢复账号访问"
    intro-description="管理员和普通用户可使用已配置的安全验证方式重置密码。"
  >
    <template #intro-extra>
      <el-alert
        title="超级管理员不能通过此页恢复密码，必须由部署人员启动本机安全重置通道。"
        type="warning"
        :closable="false"
        show-icon
        class="reset-boundary"
      />
    </template>

    <el-form
      class="recovery-form"
      label-position="top"
      @submit.prevent="completeRecovery"
    >
      <el-form-item label="用户名或完整邮箱">
        <el-input
          v-model="recovery.identifier"
          autocomplete="username"
          placeholder="请输入用户名或完整邮箱"
        />
      </el-form-item>

      <div class="method-switcher">
        <div class="method-copy">
          <span class="method-label">当前验证方式</span>
          <strong>{{ currentMethodLabel }}</strong>
        </div>
        <el-button
          plain
          type="primary"
          class="switch-button secondary-action-btn"
          :disabled="loading"
          @click="switchRecoveryMethod"
        >
          {{ switchButtonText }}
        </el-button>
      </div>

      <section
        v-if="recovery.method === 'email'"
        class="verification-panel"
      >
        <h2>使用第三方安全邮箱</h2>
        <p>验证码只会发送到该账号已绑定并验证的外部安全邮箱。</p>
        <el-form-item label="外部安全邮箱">
          <el-input
            v-model="recovery.email"
            type="email"
            autocomplete="email"
            placeholder="name@example.com"
          />
        </el-form-item>
        <el-form-item label="邮箱验证码">
          <div class="verification-code-row">
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
              :loading="sendingCode"
              :disabled="loading"
              @click="requestRecoveryCode"
            >
              发送验证码
            </el-button>
          </div>
        </el-form-item>
        <el-form-item label="管理员离线恢复码（普通用户可留空）">
          <el-input
            v-model="recovery.recoveryCode"
            autocomplete="one-time-code"
            placeholder="管理员账号需要额外输入离线恢复码"
          />
        </el-form-item>
      </section>

      <section v-else class="verification-panel">
        <h2>使用离线恢复方式</h2>
        <p>密保问题、密保答案和离线恢复码必须属于同一账号。</p>
        <el-form-item label="密保问题">
          <el-input
            v-model="recovery.question"
            autocomplete="off"
            placeholder="请输入您设置的密保问题"
          />
        </el-form-item>
        <div class="offline-grid">
          <el-form-item label="密保答案">
            <el-input
              v-model="recovery.answer"
              type="password"
              show-password
              autocomplete="off"
              placeholder="密保答案不区分大小写"
            />
          </el-form-item>
          <el-form-item label="离线恢复码">
            <el-input
              v-model="recovery.recoveryCode"
              autocomplete="one-time-code"
              placeholder="请输入一个未使用的离线恢复码"
            />
          </el-form-item>
        </div>
      </section>

      <span v-if="verificationState" class="verification-state">
        {{ verificationState }}
      </span>

      <div class="password-grid">
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
      </div>

      <div class="recovery-footer">
        <el-button
          link
          type="primary"
          class="back-button"
          @click="goLogin"
        >
          返回登录
        </el-button>
        <div class="actions">
          <el-button
            type="primary"
            class="recovery-action-btn"
            native-type="submit"
            :loading="loading"
            :disabled="sendingCode"
          >
            验证并重置密码
          </el-button>
        </div>
      </div>
    </el-form>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from '@/views/components/AuthLayout/AuthLayout.vue'
import { usePage } from './PasswordRecovery.js'

const {
  completeRecovery,
  recovery,
  currentMethodLabel,
  loading,
  switchRecoveryMethod,
  switchButtonText,
  sendingCode,
  requestRecoveryCode,
  verificationState,
  goLogin
} = usePage()
</script>

<style scoped src="./PasswordRecovery.css"></style>
