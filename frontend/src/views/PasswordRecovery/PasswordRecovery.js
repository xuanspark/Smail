import { computed, reactive, ref, watch, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { http } from '@/utils/axios'
import { isValidPasswordLength } from '@/utils/password'

export function usePage() {
  const currentStep = ref(1)
  const loading = ref(false)
  const sendingCode = ref(false)
  const countdown = ref(0)
  let timer = null
  let continuationTimer = null
  let requestVersion = 0
  let continuationAttempts = 0
  const adminRecoveryVisible = ref(false)
  const adminRecoveryError = ref('')
  const emailContinuation = ref('')

  const verificationTicket = ref('')
  const verificationState = ref('')
  const passwordMismatchError = ref('')

  const recovery = reactive({
    method: 'email',
    identifier: '',
    email: '',
    code: '',
    challengeId: '',
    question: '',
    answer: '',
    recoveryCode: '',
    newPassword: '',
    confirmPassword: ''
  })

  onBeforeUnmount(() => {
    requestVersion++
    clearAdminVerification()
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  })

  const currentMethodLabel = computed(() => recovery.method === 'email'
    ? '第三方安全邮箱验证码'
    : '密保问题 + 离线恢复码')

  const switchButtonText = computed(() => recovery.method === 'email'
    ? '切换为离线恢复'
    : '切换为邮箱验证')

  const isActionDisabled = computed(() => loading.value || sendingCode.value)

  const isCodeButtonDisabled = computed(() =>
    isActionDisabled.value ||
    countdown.value > 0 ||
    !recovery.identifier ||
    !recovery.email
  )

  const clearAdminVerification = () => {
    adminRecoveryVisible.value = false
    adminRecoveryError.value = ''
    emailContinuation.value = ''
    recovery.recoveryCode = ''
    continuationAttempts = 0
    if (continuationTimer) {
      clearTimeout(continuationTimer)
      continuationTimer = null
    }
  }

  const onIdentifierChange = () => {
    requestVersion++
    clearAdminVerification()
    recovery.challengeId = ''
    recovery.code = ''
    recovery.newPassword = ''
    recovery.confirmPassword = ''
    verificationTicket.value = ''
    verificationState.value = ''
  }

  // 账号、邮箱或方式变化后，旧请求和旧凭证均不能推进当前流程。
  watch(() => [recovery.identifier, recovery.email, recovery.method], onIdentifierChange, { flush: 'sync' })

  const clearMethodState = () => {
    onIdentifierChange()
    recovery.email = ''
    recovery.code = ''
    recovery.challengeId = ''
    recovery.question = ''
    recovery.answer = ''
    recovery.recoveryCode = ''
    verificationTicket.value = ''
    verificationState.value = ''
    if (timer) {
      clearInterval(timer)
      timer = null
    }
    countdown.value = 0
  }

  const switchRecoveryMethod = () => {
    if (isActionDisabled.value) return
    recovery.method = recovery.method === 'email' ? 'offline' : 'email'
    clearMethodState()
  }

  const startCountdown = () => {
    countdown.value = 60
    if (timer) clearInterval(timer)
    timer = setInterval(() => {
      if (countdown.value > 1) {
        countdown.value--
      } else {
        countdown.value = 0
        clearInterval(timer)
        timer = null
      }
    }, 1000)
  }

  const requestRecoveryCode = async () => {
    if (isCodeButtonDisabled.value) return
    if (!recovery.identifier || !recovery.email) {
      ElMessage.warning('请填写用户名或完整邮箱以及外部安全邮箱')
      return
    }
    onIdentifierChange()
    const version = requestVersion
    sendingCode.value = true
    try {
      const result = await http.post('/api/v1/security/recovery/begin', {
        identifier: recovery.identifier,
        email: recovery.email
      })
      if (version !== requestVersion) return
      if (result.errorNo !== 0 || !result.data?.id) {
        throw new Error(result.errorMsg || '请求已受理')
      }
      recovery.challengeId = result.data.id
      verificationState.value = '如果账号和外部安全邮箱匹配，验证码将发送到该邮箱'
      startCountdown()
      ElMessage.info('请求已受理，请检查外部安全邮箱')
    } catch (err) {
      if (version !== requestVersion) return
      verificationState.value = '请求已受理，请稍后检查外部安全邮箱'
      ElMessage.info('请求已受理，请稍后检查外部安全邮箱')
      startCountdown()
    } finally {
      sendingCode.value = false
    }
  }

  const validatePasswordMatch = () => {
    if (!recovery.newPassword || !recovery.confirmPassword) {
      passwordMismatchError.value = ''
      return
    }
    if (recovery.newPassword !== recovery.confirmPassword) {
      passwordMismatchError.value = '两次输入的新密码不一致'
    } else {
      passwordMismatchError.value = ''
    }
  }

  const validateStepOne = () => {
    if (!recovery.identifier) {
      ElMessage.warning('请填写用户名或完整邮箱')
      return false
    }
    if (recovery.method === 'email') {
      if (!recovery.email) {
        ElMessage.warning('请填写外部安全邮箱')
        return false
      }
      if (!recovery.challengeId || !recovery.code) {
        ElMessage.warning('请先获取并填写邮箱验证码')
        return false
      }
      return true
    }
    if (!recovery.question || !recovery.answer || !recovery.recoveryCode) {
      ElMessage.warning('请完整填写密保问题、密保答案和离线恢复码')
      return false
    }
    return true
  }

  const handleStepOneNext = async () => {
    if (isActionDisabled.value || adminRecoveryVisible.value) return
    if (!validateStepOne()) return
    const version = requestVersion
    loading.value = true
    verificationState.value = ''
    try {
      const payload = recovery.method === 'email'
        ? {
            method: 'email',
            id: recovery.challengeId,
            identifier: recovery.identifier,
            email: recovery.email,
            code: recovery.code
          }
        : {
            method: 'offline',
            identifier: recovery.identifier,
            question: recovery.question,
            answer: recovery.answer,
            recovery_code: recovery.recoveryCode
          }
      const result = await http.post('/api/v1/security/recovery/verify', payload)
      if (version !== requestVersion) return
      if (result.errorNo === 0 && result.data?.requires_recovery_code && result.data?.continuation) {
        clearAdminVerification()
        emailContinuation.value = result.data.continuation
        recovery.code = ''
        recovery.challengeId = ''
        adminRecoveryVisible.value = true
        const expiresIn = Math.min(Number(result.data.expires_in) || 300, 300)
        continuationTimer = setTimeout(() => {
          onIdentifierChange()
          verificationState.value = '附加验证已过期，请重新获取邮箱验证码'
          ElMessage.warning(verificationState.value)
        }, Math.max(1, expiresIn) * 1000)
        return
      }
      if (result.errorNo !== 0 || !result.data?.ticket) {
        throw new Error(result.errorMsg || '身份验证失败')
      }
      acceptVerificationTicket(result.data.ticket)
    } catch (err) {
      if (version !== requestVersion) return
      verificationTicket.value = ''
      verificationState.value = err.message || '身份验证失败，请核对信息后重试'
      ElMessage.error(err.message || '身份验证失败，请核对信息后重试')
    } finally {
      loading.value = false
    }
  }

  const acceptVerificationTicket = (ticket) => {
    clearAdminVerification()
    recovery.code = ''
    recovery.challengeId = ''
    recovery.answer = ''
    verificationTicket.value = ticket
    currentStep.value = 2
    ElMessage.success('身份核验通过，请设置新密码')
  }

  const cancelAdminVerification = () => {
    if (loading.value) return
    onIdentifierChange()
    verificationState.value = '已取消附加验证，继续恢复时请重新获取邮箱验证码'
  }

  const confirmAdminVerification = async () => {
    if (isActionDisabled.value || !adminRecoveryVisible.value || !emailContinuation.value) return
    if (!recovery.recoveryCode.trim()) {
      adminRecoveryError.value = '请输入未使用的离线恢复码'
      return
    }
    const version = requestVersion
    loading.value = true
    adminRecoveryError.value = ''
    try {
      const result = await http.post('/api/v1/security/recovery/verify', {
        method: 'email_recovery_code',
        continuation: emailContinuation.value,
        identifier: recovery.identifier,
        email: recovery.email,
        recovery_code: recovery.recoveryCode
      })
      if (version !== requestVersion) return
      if (result.errorNo !== 0 || !result.data?.ticket) {
        throw new Error('恢复码无效或验证已过期，请核对后重试')
      }
      acceptVerificationTicket(result.data.ticket)
    } catch (err) {
      if (version !== requestVersion) return
      // 临时不可用不代表恢复码错误，保留输入及附加验证凭证。
      if ([408, 429, 500, 502, 503, 504].includes(err.response?.status)
        || (err.isAxiosError && !err.response)) {
        adminRecoveryError.value = '验证服务暂时不可用，请稍后重试'
        return
      }
      recovery.recoveryCode = ''
      continuationAttempts++
      if (continuationAttempts >= 5) {
        onIdentifierChange()
        verificationState.value = '附加验证未通过，请重新获取邮箱验证码'
        ElMessage.error(verificationState.value)
      } else {
        adminRecoveryError.value = err.message || '验证失败，请重试'
      }
    } finally {
      loading.value = false
    }
  }

  const resetStepOne = () => {
    if (isActionDisabled.value) return
    recovery.identifier = ''
    clearMethodState()
  }

  const validateStepTwo = () => {
    if (!recovery.newPassword || !recovery.confirmPassword) {
      ElMessage.warning('请填写两次新密码')
      return false
    }
    if (recovery.newPassword !== recovery.confirmPassword) {
      ElMessage.warning('两次输入的新密码不一致')
      return false
    }
    if (!isValidPasswordLength(recovery.newPassword)) {
      ElMessage.warning('新密码长度必须为 8 到 12 个字节')
      return false
    }
    return true
  }

  const handleStepTwoNext = async () => {
    if (isActionDisabled.value) return
    if (!validateStepTwo()) return
    if (!verificationTicket.value) {
      ElMessage.error('验证凭证已失效，请重新进行身份验证')
      currentStep.value = 1
      return
    }
    loading.value = true
    try {
      const result = await http.post('/api/v1/security/recovery/complete', {
        ticket: verificationTicket.value,
        new_password: recovery.newPassword
      })
      if (result.errorNo !== 0 || !result.data?.reset) {
        throw new Error(result.errorMsg || '密码重置失败')
      }
      currentStep.value = 3
      verificationTicket.value = ''
      resetStepTwo()
      ElMessage.success('新登录密码重置成功！')
    } catch (err) {
      ElMessage.error(err.message || '密码重置失败，请重试')
    } finally {
      loading.value = false
    }
  }

  const resetStepTwo = () => {
    recovery.newPassword = ''
    recovery.confirmPassword = ''
    passwordMismatchError.value = ''
  }

  const goPreviousStep = () => {
    if (isActionDisabled.value) return
    if (currentStep.value === 2) {
      onIdentifierChange()
      currentStep.value = 1
      verificationState.value = '请重新进行身份验证'
    }
  }

  const goLogin = () => window.location.assign('/#/login')

  return {
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
    adminRecoveryVisible,
    adminRecoveryError,
    cancelAdminVerification,
    confirmAdminVerification,
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
  }
}
