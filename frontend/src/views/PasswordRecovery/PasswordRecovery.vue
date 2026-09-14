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

    <div class="password-recovery-container">
      <!-- 三段式进度向导导航条 -->
      <div class="recovery-steps-bar">
        <div
          class="step-item"
          :class="{ active: currentStep >= 1, current: currentStep === 1 }"
        >
          <div class="step-number">1</div>
          <div class="step-label">身份验证</div>
        </div>
        <div class="step-divider" :class="{ active: currentStep >= 2 }"></div>
        <div
          class="step-item"
          :class="{ active: currentStep >= 2, current: currentStep === 2 }"
        >
          <div class="step-number">2</div>
          <div class="step-label">设置新密码</div>
        </div>
        <div class="step-divider" :class="{ active: currentStep >= 3 }"></div>
        <div
          class="step-item"
          :class="{ active: currentStep >= 3, current: currentStep === 3 }"
        >
          <div class="step-number">3</div>
          <div class="step-label">完成重置</div>
        </div>
      </div>

      <!-- 步骤 1：身份验证 -->
      <div v-if="currentStep === 1" class="step-content">
        <div class="method-switcher">
          <div class="method-copy">
            <span class="method-label">当前验证方式</span>
            <strong>{{ currentMethodLabel }}</strong>
          </div>
          <el-button
            plain
            type="primary"
            class="switch-button secondary-action-btn"
            :disabled="isActionDisabled"
            @click="switchRecoveryMethod"
          >
            {{ switchButtonText }}
          </el-button>
        </div>

        <el-form
          class="recovery-form"
          label-position="top"
          @submit.prevent="handleStepOneNext"
        >
          <!-- 邮箱验证方式：用户名与外部安全邮箱紧密相邻 -->
          <section
            v-if="recovery.method === 'email'"
            class="verification-panel"
          >
            <div class="panel-header">
              <h2>使用第三方安全邮箱</h2>
              <p>验证码只会发送到该账号已绑定并验证的外部安全邮箱。</p>
            </div>

            <div class="compact-field-group">
              <el-form-item label="用户名或完整邮箱" required>
                <el-input
                  v-model="recovery.identifier"
                  autocomplete="username"
                  placeholder="请输入用户名或完整邮箱"
                  @input="onIdentifierChange"
                />
              </el-form-item>

              <el-form-item label="外部安全邮箱" required>
                <el-input
                  v-model="recovery.email"
                  type="email"
                  autocomplete="email"
                  placeholder="name@example.com"
                />
              </el-form-item>
            </div>

            <el-form-item label="邮箱验证码" required>
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
                  class="recovery-action-btn code-btn"
                  :loading="sendingCode"
                  :disabled="isCodeButtonDisabled"
                  @click="requestRecoveryCode"
                >
                  {{ countdown > 0 ? (countdown + ' 秒后可重发') : '发送验证码' }}
                </el-button>
              </div>
            </el-form-item>

            <el-form-item label="管理员离线恢复码（普通用户留空）">
              <el-input
                v-model="recovery.recoveryCode"
                autocomplete="one-time-code"
                placeholder="管理员账号需额外输入离线恢复码"
              />
            </el-form-item>
          </section>

          <!-- 离线恢复方式 -->
          <section v-else class="verification-panel">
            <div class="panel-header">
              <h2>使用离线恢复方式</h2>
              <p>密保问题、密保答案和离线恢复码必须属于同一账号。</p>
            </div>

            <el-form-item label="用户名或完整邮箱" required>
              <el-input
                v-model="recovery.identifier"
                autocomplete="username"
                placeholder="请输入用户名或完整邮箱"
                @input="onIdentifierChange"
              />
            </el-form-item>

            <el-form-item label="密保问题" required>
              <el-input
                v-model="recovery.question"
                autocomplete="off"
                placeholder="请输入您访置的密保问题"
              />
            </el-form-item>

            <div class="offline-grid">
              <el-form-item label="密保答案" required>
                <el-input
                  v-model="recovery.answer"
                  type="password"
                  show-password
                  autocomplete="off"
                  placeholder="密保答案不区分大小写"
                />
              </el-form-item>
              <el-form-item label="离线恢复码" required>
                <el-input
                  v-model="recovery.recoveryCode"
                  autocomplete="one-time-code"
                  placeholder="未使用的离线恢复码"
                />
              </el-form-item>
            </div>
          </section>

          <div v-if="verificationState" class="verification-state">
            {{ verificationState }}
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
                plain
                class="secondary-action-btn"
                :disabled="isActionDisabled"
                @click="resetStepOne"
              >
                重置
              </el-button>
              <el-button
                type="primary"
                class="recovery-action-btn"
                native-type="submit"
                :loading="loading"
                :disabled="sendingCode"
              >
                下一步
              </el-button>
            </div>
          </div>
        </el-form>
      </div>

      <!-- 步骤 2：设置新密码 -->
      <div v-else-if="currentStep === 2" class="step-content">
        <div class="step-two-intro">
          <h2>设置新密码</h2>
          <p>身份核验通过，请为账号 <strong>{{ recovery.identifier }}</strong> 设置新的安全登录密码。</p>
        </div>

        <el-form
          class="recovery-form"
          label-position="top"
          @submit.prevent="handleStepTwoNext"
        >
          <div class="password-grid-vertical">
            <el-form-item label="新密码" required>
              <el-input
                v-model="recovery.newPassword"
                type="password"
                show-password
                autocomplete="new-password"
                placeholder="请输入 8 到 12 位新密码"
                @input="validatePasswordMatch"
              />
            </el-form-item>

            <el-form-item label="确认新密码" required>
              <el-input
                v-model="recovery.confirmPassword"
                type="password"
                show-password
                autocomplete="new-password"
                placeholder="请再次输入新密码"
                @input="validatePasswordMatch"
              />
            </el-form-item>
          </div>

          <div v-if="passwordMismatchError" class="password-error-msg">
            {{ passwordMismatchError }}
          </div>

          <div class="recovery-footer">
            <el-button
              link
              type="primary"
              class="back-button"
              :disabled="loading"
              @click="goPreviousStep"
            >
              上一步
            </el-button>
            <div class="actions">
              <el-button
                plain
                class="secondary-action-btn"
                :disabled="loading"
                @click="resetStepTwo"
              >
                重置
              </el-button>
              <el-button
                type="primary"
                class="recovery-action-btn"
                native-type="submit"
                :loading="loading"
              >
                下一步
              </el-button>
            </div>
          </div>
        </el-form>
      </div>


      <!-- 步骤 3：完成重置 -->
      <div v-else-if="currentStep === 3" class="step-content step-success">
        <div class="success-icon-wrap">
          <span class="success-emoji">-^0^-</span>
        </div>
        <h2 class="success-title">新登录密码重置成功！</h2>
        <p class="success-description">请使用新设置的密码重新登录系统。</p>
        <div class="success-action">
          <el-button
            type="primary"
            class="recovery-action-btn"
            @click="goLogin"
          >
            重新登录
          </el-button>
        </div>
      </div>
    </div>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from '@/views/components/AuthLayout/AuthLayout.vue'
import { usePage } from './PasswordRecovery.js'

const {
  currentStep,
  loading,
  sendingCode,
  countdown,
  verificationState,
  recovery,
  currentMethodLabel,
  switchButtonText,
  isActionDisabled,
  isCodeButtonDisabled,
  passwordMismatchError,
  switchRecoveryMethod,
  requestRecoveryCode,
  handleStepOneNext,
  handleStepTwoNext,
  goPreviousStep,
  resetStepOne,
  resetStepTwo,
  validatePasswordMatch,
  onIdentifierChange,
  goLogin
} = usePage()
</script>

<style scoped src="./PasswordRecovery.css"></style>
