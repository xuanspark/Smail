<template>
  <el-dialog
    :model-value="visible"
    :title="selfManaged ? '编辑我的外部邮箱' : '编辑外部邮箱'"
    width="min(520px, calc(100vw - 32px))"
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    :show-close="!busy"
    append-to-body
    destroy-on-close
    class="user-external-email-dialog"
    @update:model-value="value => { if (!value) close() }"
  >
    <div class="external-email-account">
      <span>目标账号</span>
      <strong>{{ user?.Account || '-' }}</strong>
      <span v-if="currentEmail">当前邮箱：{{ currentEmail }}</span>
      <span v-else>当前未绑定外部邮箱</span>
    </div>

    <el-form
      v-if="step === 'details'"
      :model="form"
      label-position="top"
      class="external-email-form"
      @submit.prevent="sendVerificationCode"
    >
      <el-form-item label="新的外部邮箱地址" required>
        <el-input
          v-model="form.email"
          type="email"
          autocomplete="email"
          placeholder="请输入可接收验证码的外部邮箱"
          clearable
          :disabled="sending"
          @input="errorMessage = ''"
          @keyup.enter="sendVerificationCode"
        />
      </el-form-item>
      <el-form-item label="邮箱用途" required>
        <el-select v-model="form.type" aria-label="邮箱用途" :disabled="sending">
          <el-option label="主要邮箱" value="primary" />
          <el-option label="备用邮箱" value="backup" />
        </el-select>
      </el-form-item>
    </el-form>

    <div v-else class="external-email-verification">
      <div class="verification-target">
        <span>验证码已发送至</span>
        <strong>{{ challenge.maskedEmail }}</strong>
        <span :class="{ expired: remainingSeconds <= 0 }">{{ expirationText }}</span>
      </div>
      <el-form label-position="top" @submit.prevent="verifyAndReplace">
        <el-form-item label="邮箱验证码" required>
          <el-input
            :model-value="form.code"
            inputmode="numeric"
            autocomplete="one-time-code"
            :maxlength="verificationCodeLength"
            placeholder="请输入邮箱中的验证码"
            clearable
            :disabled="verifying"
            @update:model-value="handleCodeInput"
            @keyup.enter="verifyAndReplace"
          />
        </el-form-item>
      </el-form>
    </div>

    <p class="replacement-note">
      新邮箱验证成功后才会替换同用途旧邮箱，验证失败不会影响现有绑定。
    </p>
    <p v-if="statusMessage" class="dialog-feedback success" role="status">
      {{ statusMessage }}
    </p>
    <p v-if="errorMessage" class="dialog-feedback error" role="alert">
      {{ errorMessage }}
    </p>

    <template #footer>
      <div class="external-dialog-actions">
        <el-button :disabled="busy" @click="close">取消</el-button>
        <el-button
          v-if="step === 'verification'"
          :loading="sending"
          :disabled="verifying"
          @click="sendVerificationCode"
        >
          重新发送
        </el-button>
        <el-button
          v-if="step === 'details'"
          type="primary"
          :loading="sending"
          @click="sendVerificationCode"
        >
          发送验证码
        </el-button>
        <el-button
          v-else
          type="primary"
          :loading="verifying"
          :disabled="sending"
          @click="verifyAndReplace"
        >
          验证并保存
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import {
  userExternalEmailDialogProps,
  useUserExternalEmailDialog,
} from './UserExternalEmailDialog.js'

const props = defineProps(userExternalEmailDialogProps)
const emit = defineEmits(['update:modelValue', 'changed'])

const {
  visible,
  form,
  step,
  currentEmail,
  sending,
  verifying,
  busy,
  challenge,
  errorMessage,
  statusMessage,
  remainingSeconds,
  expirationText,
  verificationCodeLength,
  close,
  sendVerificationCode,
  verifyAndReplace,
  handleCodeInput,
} = useUserExternalEmailDialog(props, emit)
</script>

<style scoped src="./UserExternalEmailDialog.css"></style>
