import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'

import { http } from '@/utils/axios'
import {
  isRecentAuthenticationCancelled,
  requestWithRecentAuthentication,
} from '@/utils/recentAuthentication'

export const userExternalEmailDialogProps = {
  modelValue: {
    type: Boolean,
    default: false,
  },
  user: {
    type: Object,
    default: null,
  },
}

const verificationCodeLength = 8

function assertSuccess(response, fallback) {
  if (!response || response.errorNo !== 0) {
    const error = new Error(response?.errorMsg || fallback)
    if (typeof response?.data?.code === 'string') {
      error.apiErrorCode = response.data.code
    }
    throw error
  }
  return response
}

export function useUserExternalEmailDialog(props, emit) {
  const form = reactive({ email: '', type: 'primary', code: '' })
  const challenge = reactive({ id: '', bindingId: 0, maskedEmail: '', expiresAt: '' })
  const currentEmail = ref('')
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

  const reset = () => {
    Object.assign(form, { email: '', type: 'primary', code: '' })
    Object.assign(challenge, { id: '', bindingId: 0, maskedEmail: '', expiresAt: '' })
    currentEmail.value = ''
    step.value = 'details'
    clearMessages()
  }

  const loadCurrentEmail = async () => {
    if (!props.user?.ID) return false
    try {
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.get('/api/v1/user/security/external-email/list', {
          params: { id: props.user.ID },
        })
        return assertSuccess(result, '读取外部邮箱失败')
      })
      const items = Array.isArray(response.data?.items) ? response.data.items : []
      const primary = items.find(item => item.type === 'primary') || items[0]
      currentEmail.value = primary?.email || ''
      if (primary?.type) form.type = primary.type
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '读取外部邮箱失败'
      }
      return false
    }
  }

  const sendVerificationCode = async () => {
    if (busy.value || !props.user?.ID) return false
    const email = form.email.trim().toLowerCase()
    if (!email) {
      errorMessage.value = '请输入外部邮箱地址'
      return false
    }
    sending.value = true
    clearMessages()
    try {
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.post('/api/v1/user/security/external-email/change', {
          user_id: props.user.ID,
          email,
          type: form.type,
        })
        return assertSuccess(result, '验证码发送失败')
      })
      if (!response.data?.challenge_id || !response.data?.binding_id) {
        throw new Error('服务器返回的验证信息不完整，请稍后重试')
      }
      form.email = email
      form.code = ''
      Object.assign(challenge, {
        id: response.data.challenge_id,
        bindingId: response.data.binding_id,
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

  const verifyAndReplace = async () => {
    if (busy.value || !hasChallenge.value || !props.user?.ID) return false
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
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.post('/api/v1/user/security/external-email/verify', {
          user_id: props.user.ID,
          challenge_id: challenge.id,
          binding_id: challenge.bindingId,
          email: form.email,
          code,
        })
        return assertSuccess(result, '外部邮箱验证失败')
      })
      emit('changed')
      visible.value = false
      reset()
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '外部邮箱验证失败，请检查验证码后重试'
      }
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

  const close = () => {
    if (busy.value) return
    visible.value = false
    reset()
  }

  watch(
    () => props.modelValue,
    value => {
      if (value) {
        reset()
        startTimer()
        void loadCurrentEmail()
      } else {
        stopTimer()
      }
    },
    { immediate: true },
  )

  onBeforeUnmount(stopTimer)

  return {
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
  }
}
