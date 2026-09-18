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
        <p v-if="errorMessage" class="public-recovery-status error" role="alert">{{ errorMessage }}</p>
        <div class="public-recovery-actions">
          <button type="button" class="text-button" :disabled="loading" @click="goLogin">返回登录</button>
          <button type="submit" :disabled="loading || !form.identifier.trim()">{{ loading ? '正在查询' : '下一步' }}</button>
        </div>
      </form>

      <section v-else-if="page === 'blocked'" class="public-recovery-form" aria-live="polite">
        <strong>{{ roleLabel }}</strong>
        <p class="public-recovery-status">超级管理员无法通过登录页通道重置密码。已登录时可在安全中心重置；无法登录时请使用部署端本机重置。</p>
        <div class="public-recovery-actions">
          <button type="button" class="text-button" @click="goRequest">更换账号</button>
          <button type="button" @click="goLogin">返回登录</button>
        </div>
      </section>

      <form v-else-if="page === 'verify'" class="public-recovery-form" @submit.prevent="verifyIdentity">
        <strong>{{ form.identifier }} · {{ roleLabel }}</strong>
        <p class="public-recovery-copy">绑定邮箱：{{ identity.email || '未绑定已验证的安全邮箱' }}</p>
        <template v-if="identity.methods.length">
          <fieldset v-if="identity.methods.length > 1" class="recovery-methods" :disabled="loading">
            <legend>验证方式</legend>
            <label><input v-model="form.method" type="radio" value="email" />邮箱验证码</label>
            <label><input v-model="form.method" type="radio" value="security_code" />安全码</label>
          </fieldset>
          <label for="public-recovery-code">{{ form.method === 'email' ? '邮箱验证码' : '安全码' }}</label>
          <div class="recovery-code-row">
            <input
              id="public-recovery-code"
              v-model="form.code"
              :type="form.method === 'email' ? 'text' : 'password'"
              :inputmode="form.method === 'email' ? 'numeric' : 'text'"
              :maxlength="form.method === 'email' ? 8 : 64"
              :placeholder="form.method === 'email' ? '输入 8 位邮件验证码' : '输入未使用的安全码'"
              autocomplete="one-time-code"
              :disabled="loading"
              required
            />
            <button v-if="form.method === 'email'" type="button" :disabled="loading || countdown > 0" @click="sendCode">
              {{ countdown ? `${countdown} 秒后重发` : '获取验证码' }}
            </button>
          </div>
        </template>
        <p v-else class="public-recovery-status">此账号尚未配置可用的重置方式，请登录安全中心或联系管理员处理。</p>
        <p v-if="notice" class="public-recovery-status success" role="status">{{ notice }}</p>
        <p v-if="errorMessage" class="public-recovery-status error" role="alert">{{ errorMessage }}</p>
        <div class="public-recovery-actions">
          <button type="button" class="text-button" :disabled="loading" @click="goRequest">更换账号</button>
          <button v-if="identity.methods.length" type="submit" :disabled="loading || !canVerify">{{ loading ? '正在验证' : '验证并继续' }}</button>
        </div>
      </form>

      <form v-else-if="page === 'reset'" class="public-recovery-form" @submit.prevent="completeReset">
        <p class="public-recovery-status success">身份验证已通过</p>
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
          <p v-if="errorMessage" class="public-recovery-status error">{{ errorMessage }}</p>
          <div class="public-recovery-actions">
            <button type="button" class="text-button" :disabled="loading" @click="goRequest">重新申请</button>
            <button type="submit" :disabled="loading || !canSubmitReset">{{ loading ? '正在重置' : '确认重置' }}</button>
          </div>
      </form>

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
import { usePage } from './useRecoveryFlow.js'

const {
  page,
  loading,
  identity,
  roleLabel,
  notice,
  countdown,
  canVerify,
  sendCode,
  verifyIdentity,
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
