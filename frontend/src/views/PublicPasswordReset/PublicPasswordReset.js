import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { isValidPasswordLength } from '@/utils/password'
import { postPublicRecovery } from '@/utils/publicRecoveryHttp'

const requestHash = '#/forgot-password'
const resetHashPrefix = '#/reset-password/'

function currentResetToken() {
  const hash = window.location.hash
  if (!hash.startsWith(resetHashPrefix)) {
    return ''
  }

  const encodedToken = hash.slice(resetHashPrefix.length)
  if (!encodedToken || encodedToken.includes('/')) {
    return ''
  }

  try {
    const token = decodeURIComponent(encodedToken)
    return token.length <= 512 ? token : ''
  } catch {
    return ''
  }
}

function loginURL() {
  const value = document.querySelector('#app')?.dataset.loginUrl
  if (!value) {
    return '/#/login'
  }
  return value
}

export function usePage() {
  const page = ref('request')
  const loading = ref(false)
  const submitted = ref(false)
  const available = ref(false)
  const requiresRecoveryCode = ref(false)
  const errorMessage = ref('')
  const resetToken = ref('')
  const form = reactive({
    identifier: '',
    newPassword: '',
    confirmPassword: '',
    recoveryCode: ''
  })

  const passwordMismatch = computed(() => form.confirmPassword !== '' && form.newPassword !== form.confirmPassword)
  const canSubmitReset = computed(() => (
    form.newPassword !== '' &&
    form.confirmPassword !== '' &&
    !passwordMismatch.value &&
    isValidPasswordLength(form.newPassword) &&
    (!requiresRecoveryCode.value || form.recoveryCode.trim() !== '')
  ))

  const resetRequestForm = () => {
    page.value = 'request'
    submitted.value = false
    available.value = false
    requiresRecoveryCode.value = false
    errorMessage.value = ''
    resetToken.value = ''
    form.newPassword = ''
    form.confirmPassword = ''
    form.recoveryCode = ''
  }

  const loadRoute = async () => {
    const token = currentResetToken()
    if (!token) {
      if (window.location.hash !== requestHash) {
        window.history.replaceState({}, document.title, `${window.location.pathname}${window.location.search}${requestHash}`)
      }
      resetRequestForm()
      return
    }

    page.value = 'reset'
    submitted.value = false
    available.value = false
    requiresRecoveryCode.value = false
    errorMessage.value = ''
    resetToken.value = token
    form.newPassword = ''
    form.confirmPassword = ''
    form.recoveryCode = ''

    loading.value = true
    try {
      const result = await postPublicRecovery('requirements', { token })
      if (resetToken.value !== token || page.value !== 'reset') {
        return
      }
      if (!result.ok || !result.data) {
        errorMessage.value = '恢复链接无效或已失效，请重新申请。'
        return
      }
      requiresRecoveryCode.value = result.data.requires_recovery_code === true
      available.value = true
    } catch {
      if (resetToken.value === token && page.value === 'reset') {
        errorMessage.value = '暂时无法验证恢复链接，请稍后重试。'
      }
    } finally {
      if (resetToken.value === token) {
        loading.value = false
      }
    }
  }

  const requestReset = async () => {
    if (loading.value || !form.identifier.trim()) {
      return
    }
    loading.value = true
    errorMessage.value = ''
    try {
      await postPublicRecovery('begin', { identifier: form.identifier.trim() })
      submitted.value = true
    } catch {
      submitted.value = true
    } finally {
      loading.value = false
    }
  }

  const completeReset = async () => {
    if (loading.value || !canSubmitReset.value || !resetToken.value) {
      return
    }
    loading.value = true
    errorMessage.value = ''
    try {
      const result = await postPublicRecovery('complete', {
        token: resetToken.value,
        new_password: form.newPassword,
        recovery_code: requiresRecoveryCode.value ? form.recoveryCode.trim() : ''
      })
      if (!result.ok || result.data?.reset !== true) {
        errorMessage.value = '重置失败。恢复链接可能已失效，请重新申请。'
        return
      }
      resetToken.value = ''
      form.newPassword = ''
      form.confirmPassword = ''
      form.recoveryCode = ''
      page.value = 'success'
    } catch {
      errorMessage.value = '重置失败。恢复链接可能已失效，请重新申请。'
    } finally {
      loading.value = false
    }
  }

  const goLogin = () => window.location.assign(loginURL())
  const goRequest = () => window.location.assign(`${window.location.pathname}${window.location.search}${requestHash}`)

  onMounted(() => {
    void loadRoute()
    window.addEventListener('hashchange', loadRoute)
  })

  onBeforeUnmount(() => {
    window.removeEventListener('hashchange', loadRoute)
    resetToken.value = ''
  })

  return {
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
  }
}
