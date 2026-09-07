import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'

import { http } from '@/utils/axios'
import {
  isRecentAuthenticationCancelled,
  requestWithRecentAuthentication,
} from '@/utils/recentAuthentication'

export const externalEmailBindingDialogProps = {
  modelValue: {
    type: Boolean,
    default: false,
  },
}

const verificationCodeLength = 8

function responseError(response, fallback) {
  const error = new Error(response?.errorMsg || fallback)
  if (typeof response?.data?.code === 'string') {
    error.apiErrorCode = response.data.code
  }
  return error
}

function assertSuccess(response, fallback) {
  if (!response || response.errorNo !== 0) {
    throw responseError(response, fallback)
  }
  return response
}

export function useExternalEmailBindingDialog(props, emit) {
  const form = reactive({
    email: '',
    type: 'backup',
    code: '',
  })
  const challenge = reactive({
    id: '',
    bindingId: 0,
    maskedEmail: '',
    expiresAt: '',
  })
  const step = ref('details')
  const sending = ref(false)
  const verifying = ref(false)
  const errorMessage = ref('')
  const statusMessage = ref('')
  const currentTime = ref(Date.now())
  let timer = null

  const visible = computed({
    get: () => props.modelValue,
    set: value => emit('update:modelValue', value),
  })
  const busy = computed(() => sending.value || verifying.value)
  const hasChallenge = computed(() => Boolean(challenge.id && challenge.bindingId))
  const remainingSeconds = computed(() => {
    if (!challenge.expiresAt) return 0
    const expiresAt = new Date(challenge.expiresAt).getTime()
    if (!Number.isFinite(expiresAt)) return 0
    return Math.max(0, Math.ceil((expiresAt - currentTime.value) / 1000))
  })
  const expirationText = computed(() => {
    if (!hasChallenge.value) return ''
    if (remainingSeconds.value <= 0) return '验证码已过期，请重新发送'
    const minutes = Math.floor(remainingSeconds.value / 60)
    const seconds = remainingSeconds.value % 60
    return `${minutes}:${String(seconds).padStart(2, '0')} 后过期`
  })

  const startTimer = () => {
    if (timer) return
    currentTime.value = Date.now()
    timer = window.setInterval(() => {
      currentTime.value = Date.now()
    }, 1000)
  }

  const stopTimer = () => {
    if (!timer) return
    window.clearInterval(timer)
    timer = null
  }

  const clearMessages = () => {
    errorMessage.value = ''
    statusMessage.value = ''
  }

  const resetChallenge = () => {
    Object.assign(challenge, {
      id: '',
      bindingId: 0,
      maskedEmail: '',
      expiresAt: '',
    })
    form.code = ''
    step.value = 'details'
  }

  const resetAll = () => {
    Object.assign(form, { email: '', type: 'backup', code: '' })
    resetChallenge()
    clearMessages()
  }

  const close = () => {
    if (busy.value) return
    clearMessages()
    visible.value = false
  }

  const sendVerificationCode = async () => {
    if (busy.value) return false
    const email = form.email.trim().toLowerCase()
    if (!email) {
      errorMessage.value = '请输入外部邮箱地址'
      return false
    }

    sending.value = true
    clearMessages()
    try {
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.post('/api/v1/security/external-email/bind', {
          email,
          type: form.type,
        })
        return assertSuccess(result, '验证码发送失败')
      })
      if (!response.data?.challenge_id || !response.data?.id) {
        throw new Error('服务器返回的验证信息不完整，请稍后重试')
      }
      form.email = email
      form.code = ''
      Object.assign(challenge, {
        id: response.data.challenge_id,
        bindingId: response.data.id,
        maskedEmail: response.data.email || email,
        expiresAt: response.data.expires_at || '',
      })
      currentTime.value = Date.now()
      step.value = 'verification'
      statusMessage.value = `验证码已发送到 ${challenge.maskedEmail}`
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '验证码发送失败，请稍后重试'
      }
      return false
    } finally {
      sending.value = false
    }
  }

  const verifyAndBind = async () => {
    if (busy.value || !hasChallenge.value) return false
    const code = form.code.trim()
    if (!code) {
      errorMessage.value = '请输入邮箱验证码'
      return false
    }
    if (remainingSeconds.value <= 0) {
      errorMessage.value = '验证码已过期，请重新发送'
      return false
    }

    verifying.value = true
    clearMessages()
    try {
      const response = await http.post('/api/v1/security/external-email/verify', {
        id: challenge.id,
        binding_id: challenge.bindingId,
        email: form.email,
        code,
      })
      assertSuccess(response, '外部邮箱验证失败')
      emit('bound')
      visible.value = false
      resetAll()
      return true
    } catch (error) {
      errorMessage.value = error?.message || '外部邮箱验证失败，请检查验证码后重试'
      form.code = ''
      return false
    } finally {
      verifying.value = false
    }
  }

  const handleCodeInput = value => {
    form.code = String(value || '').replace(/\s+/g, '').slice(0, verificationCodeLength)
    errorMessage.value = ''
  }

  watch(() => props.modelValue, value => {
    if (value) {
      startTimer()
      if (hasChallenge.value) step.value = 'verification'
    } else {
      stopTimer()
    }
  }, { immediate: true })

  onBeforeUnmount(stopTimer)

  return {
    form,
    step,
    sending,
    verifying,
    busy,
    challenge,
    hasChallenge,
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
  }
}
