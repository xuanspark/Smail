<template>
  <main class="public-recovery-page">
    <section class="public-recovery-card" aria-labelledby="public-recovery-title">
      <header class="public-recovery-header">
        <img class="public-recovery-logo" src="/smail-logo.png" alt="Smail" />
        <div>
          <p class="public-recovery-eyebrow">Smail 安全恢复</p>
          <h1 id="public-recovery-title">{{ page === 'request' ? '找回登录密码' : '重设登录密码' }}</h1>
        </div>
      </header>

      <form v-if="page === 'request'" class="public-recovery-form" @submit.prevent="requestReset">
        <p class="public-recovery-copy">输入账号或完整邮箱。若该账号允许外部邮箱恢复，系统会向已验证的安全邮箱发送一次性恢复链接。</p>
        <label for="public-recovery-identifier">账号或完整邮箱</label>
        <input
          id="public-recovery-identifier"
          v-model="form.identifier"
          autocomplete="username"
          maxlength="320"
          placeholder="例如：name@example.com"
          :disabled="loading"
          required
        />
        <p v-if="submitted" class="public-recovery-status success" role="status">请求已受理。若信息匹配，请检查已验证的外部安全邮箱。</p>
        <div class="public-recovery-actions">
          <button type="button" class="text-button" :disabled="loading" @click="goLogin">返回登录</button>
          <button type="submit" :disabled="loading || !form.identifier.trim()">{{ loading ? '正在提交…' : '发送恢复链接' }}</button>
        </div>
      </form>

      <section v-else-if="page === 'reset'" class="public-recovery-form" aria-live="polite">
        <p class="public-recovery-copy">请设置新的登录密码。为保护账号安全，此链接仅可使用一次。</p>
        <p v-if="loading" class="public-recovery-status">正在验证恢复链接…</p>
        <template v-else-if="available">
          <label for="public-recovery-password">新密码</label>
          <input
            id="public-recovery-password"
            v-model="form.newPassword"
            type="password"
            autocomplete="new-password"
            minlength="8"
            maxlength="12"
            placeholder="8 至 12 位密码"
          />
          <label for="public-recovery-confirm-password">确认新密码</label>
          <input
            id="public-recovery-confirm-password"
            v-model="form.confirmPassword"
            type="password"
            autocomplete="new-password"
            minlength="8"
            maxlength="12"
            placeholder="再次输入新密码"
          />
          <p v-if="passwordMismatch" class="public-recovery-status error">两次输入的新密码不一致。</p>
          <template v-if="requiresRecoveryCode">
            <label for="public-recovery-code">离线恢复码</label>
            <input
              id="public-recovery-code"
              v-model="form.recoveryCode"
              type="password"
              autocomplete="one-time-code"
              maxlength="64"
              placeholder="输入未使用的离线恢复码"
            />
          </template>
          <p v-if="errorMessage" class="public-recovery-status error">{{ errorMessage }}</p>
          <div class="public-recovery-actions">
            <button type="button" class="text-button" :disabled="loading" @click="goRequest">重新申请</button>
            <button type="button" :disabled="loading || !canSubmitReset" @click="completeReset">{{ loading ? '正在重置…' : '确认重置' }}</button>
          </div>
        </template>
        <template v-else>
          <p class="public-recovery-status error">{{ errorMessage || '恢复链接无效或已失效，请重新申请。' }}</p>
          <div class="public-recovery-actions single-action">
            <button type="button" @click="goRequest">重新申请恢复链接</button>
          </div>
        </template>
      </section>

      <section v-else class="public-recovery-complete" aria-live="polite">
        <div class="public-recovery-success-icon" aria-hidden="true">✓</div>
        <h2>密码已重置</h2>
        <p>请使用新密码重新登录系统。</p>
        <button type="button" @click="goLogin">前往登录</button>
      </section>
    </section>
  </main>
</template>

<script setup>
import { usePage } from './PublicPasswordReset.js'

const {
  page,
  loading,
  submitted,
  available,
  requiresRecoveryCode,
  errorMessage,
  form,
  passwordMismatch,
  canSubmitReset,
  requestReset,
  completeReset,
  goLogin,
  goRequest
} = usePage()
</script>

<style scoped src="./PublicPasswordReset.css"></style>
