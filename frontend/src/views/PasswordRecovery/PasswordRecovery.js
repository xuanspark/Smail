import { computed, reactive, ref } from 'vue'

import { ElMessage } from 'element-plus'

import { http } from '@/utils/axios'

import { isValidPasswordLength } from '@/utils/password'

export function usePage() {
  const loading = ref(false)
  
  const sendingCode = ref(false)
  
  const verificationTicket = ref('')
  
  const verificationState = ref('')
  
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
  
  const currentMethodLabel = computed(() => recovery.method === 'email'
    ? '第三方安全邮箱验证码'
    : '密保问题 + 离线恢复码')
  
  const switchButtonText = computed(() => recovery.method === 'email'
    ? '切换为离线恢复'
    : '切换为邮箱验证')
  
  const clearMethodState = () => {
    recovery.email = ''
    recovery.code = ''
    recovery.challengeId = ''
    recovery.question = ''
    recovery.answer = ''
    recovery.recoveryCode = ''
    verificationTicket.value = ''
    verificationState.value = ''
  }
  
  const switchRecoveryMethod = () => {
    recovery.method = recovery.method === 'email' ? 'offline' : 'email'
    clearMethodState()
  }
  
  const requestRecoveryCode = async () => {
    if (!recovery.identifier || !recovery.email) {
      ElMessage.warning('请先填写用户名或完整邮箱以及外部安全邮箱')
      return
    }
    recovery.code = ''
    recovery.challengeId = ''
    verificationTicket.value = ''
    verificationState.value = ''
    sendingCode.value = true
    try {
      const result = await http.post('/api/v1/security/recovery/begin', {
        identifier: recovery.identifier,
        email: recovery.email
      })
      if (result.errorNo !== 0 || !result.data?.id) throw new Error('请求已受理')
      recovery.challengeId = result.data.id
      verificationState.value = '如果账号和外部安全邮箱匹配，验证码将发送到该邮箱'
    } catch (_) {
      verificationState.value = '请求已受理，请稍后检查外部安全邮箱'
    } finally {
      sendingCode.value = false
    }
  }
  
  const validatePassword = () => {
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
  
  const validateRecoveryMethod = () => {
    if (!recovery.identifier) {
      ElMessage.warning('请填写用户名或完整邮箱')
      return false
    }
    if (recovery.method === 'email') {
      if (!recovery.email || !recovery.challengeId || !recovery.code) {
        ElMessage.warning('请先发送并填写邮箱验证码')
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
  
  const verifyRecovery = async () => {
    const payload = recovery.method === 'email'
      ? {
          method: 'email',
          id: recovery.challengeId,
          identifier: recovery.identifier,
          email: recovery.email,
          code: recovery.code,
          recovery_code: recovery.recoveryCode
        }
      : {
          method: 'offline',
          identifier: recovery.identifier,
          question: recovery.question,
          answer: recovery.answer,
          recovery_code: recovery.recoveryCode
        }
    const result = await http.post('/api/v1/security/recovery/verify', payload)
    if (result.errorNo !== 0 || !result.data?.ticket) throw new Error('验证失败')
    verificationTicket.value = result.data.ticket
    return result.data.ticket
  }
  
  const completeRecovery = async () => {
    if (!validateRecoveryMethod() || !validatePassword()) return
    loading.value = true
    verificationTicket.value = ''
    verificationState.value = ''
    try {
      const ticket = await verifyRecovery()
      const result = await http.post('/api/v1/security/recovery/complete', {
        ticket,
        new_password: recovery.newPassword
      })
      if (result.errorNo !== 0 || !result.data?.reset) throw new Error('重置失败')
      clearMethodState()
      recovery.identifier = ''
      recovery.newPassword = ''
      recovery.confirmPassword = ''
      ElMessage.success('密码已重置，请使用新密码登录')
      goLogin()
    } catch (_) {
      verificationTicket.value = ''
      verificationState.value = '验证或重置失败，请检查输入后重试'
      ElMessage.error('验证或重置失败')
    } finally {
      loading.value = false
    }
  }
  
  const goLogin = () => window.location.assign('/#/login')

  return {
    computed,
    reactive,
    ref,
    ElMessage,
    http,
    loading,
    sendingCode,
    verificationTicket,
    verificationState,
    recovery,
    currentMethodLabel,
    switchButtonText,
    clearMethodState,
    switchRecoveryMethod,
    requestRecoveryCode,
    validatePassword,
    validateRecoveryMethod,
    verifyRecovery,
    completeRecovery,
    goLogin
  }
}

