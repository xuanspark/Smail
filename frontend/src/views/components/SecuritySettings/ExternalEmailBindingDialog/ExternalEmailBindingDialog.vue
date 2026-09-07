<template>
  <el-dialog
    :model-value="visible"
    title="绑定外部安全邮箱"
    width="min(520px, calc(100vw - 32px))"
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    :show-close="!busy"
    class="external-email-binding-dialog"
    @update:model-value="value => { if (!value) close() }"
  >
    <el-form
      v-if="step === 'details'"
      :model="form"
      label-position="top"
      class="external-email-form"
      @submit.prevent="sendVerificationCode"
    >
      <el-form-item label="外部邮箱地址" required>
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
          <el-option label="备用邮箱" value="backup" />
          <el-option label="主要邮箱" value="primary" />
        </el-select>
      </el-form-item>
    </el-form>

    <div v-else class="external-email-verification">
      <div class="verification-target">
        <span>验证码已发送至</span>
        <strong>{{ challenge.maskedEmail }}</strong>
        <span :class="{ expired: remainingSeconds <= 0 }">{{ expirationText }}</span>
      </div>
      <el-form label-position="top" @submit.prevent="verifyAndBind">
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
            @keyup.enter="verifyAndBind"
          />
        </el-form-item>
      </el-form>
    </div>

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
          @click="verifyAndBind"
        >
          验证并绑定
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import {
  externalEmailBindingDialogProps,
  useExternalEmailBindingDialog,
} from './ExternalEmailBindingDialog.js'

const props = defineProps(externalEmailBindingDialogProps)
const emit = defineEmits(['update:modelValue', 'bound'])

const {
  form,
  step,
  sending,
  verifying,
  busy,
  challenge,
  errorMessage,
  statusMessage,
  expirationText,
  remainingSeconds,
  verificationCodeLength,
  visible,
  close,
  sendVerificationCode,
  verifyAndBind,
  handleCodeInput,
} = useExternalEmailBindingDialog(props, emit)
</script>

<style scoped src="./ExternalEmailBindingDialog.css"></style>
