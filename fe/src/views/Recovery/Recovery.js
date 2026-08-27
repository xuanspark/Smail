import { computed, reactive, ref } from 'vue'

import { ElMessage } from 'element-plus'

import { http } from '@/utils/axios'

import { normalizeRequestOptions, serializeCredential, webAuthnSupported } from '@/utils/webauthn'

export function usePage() {
  const loading = ref(false)
  
  const verificationTicket = ref('')
  
  const verificationState = ref('')
  
  const securityKeySupported = webAuthnSupported()
  
  const recovery = reactive({
    method: 'webauthn',
    email: '',
    code: '',
    challengeId: '',
    newPassword: '',
    confirmPassword: ''
  })
  
  const verified = computed(() => verificationTicket.value !== '')
  
  const clearVerification = () => {
    recovery.code = ''
    recovery.challengeId = ''
    verificationTicket.value = ''
    verificationState.value = ''
  }
  
  const switchRecoveryMethod = () => {
    recovery.method = recovery.method === 'webauthn' ? 'email' : 'webauthn'
    recovery.email = ''
    clearVerification()
  }
  
  const verifySecurityKey = async () => {
    if (!securityKeySupported) {
      ElMessage.warning('当前浏览器或访问环境不支持安全密钥')
      return
    }
    clearVerification()
    loading.value = true
    try {
      const begin = await http.post('/api/security/recovery/webauthn/begin', { identifier: 'super_admin' })
      if (begin.errorNo !== 0 || !begin.data?.options) throw new Error('安全密钥验证暂不可用')
      const credential = await navigator.credentials.get({ publicKey: normalizeRequestOptions(begin.data.options) })
      const result = await http.post('/api/security/recovery/webauthn/verify', {
        identifier: 'super_admin',
        session_id: begin.data.session_id,
        credential: serializeCredential(credential)
      })
      if (result.errorNo !== 0 || !result.data?.ticket) throw new Error('安全密钥验证失败')
      verificationTicket.value = result.data.ticket
      verificationState.value = '安全密钥验证成功'
    } catch (error) {
      if (error?.name !== 'NotAllowedError') {
        verificationState.value = '安全密钥验证未完成'
        ElMessage.error(error?.message || '安全密钥验证失败')
      }
    } finally {
      loading.value = false
    }
  }
  
  const requestRecoveryCode = async () => {
    if (!recovery.email) {
      ElMessage.warning('请填写外部安全邮箱')
      return
    }
    clearVerification()
    loading.value = true
    try {
      const result = await http.post('/api/security/recovery/begin', { identifier: 'super_admin', email: recovery.email })
      if (result.errorNo === 0 && result.data?.id) recovery.challengeId = result.data.id
      verificationState.value = '如果邮箱有效，验证码将发送到该邮箱'
    } catch (_) {
      verificationState.value = '如果邮箱有效，验证码将发送到该邮箱'
    } finally {
      loading.value = false
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
    if (new TextEncoder().encode(recovery.newPassword).length < 8) {
      ElMessage.warning('新密码至少需要 8 个字节')
      return false
    }
    return true
  }
  
  const completeRecovery = () => {
    if (!verified.value) {
      ElMessage.warning('请先完成当前验证方式')
      return
    }
    if (!validatePassword()) return
    ElMessage.warning('请通过部署端 smail-admin 本机安全通道完成正式重置')
  }
  
  const goLogin = () => window.location.assign('/#/login')

  return {
    computed,
    reactive,
    ref,
    ElMessage,
    http,
    normalizeRequestOptions,
    serializeCredential,
    webAuthnSupported,
    loading,
    verificationTicket,
    verificationState,
    securityKeySupported,
    recovery,
    verified,
    clearVerification,
    switchRecoveryMethod,
    verifySecurityKey,
    requestRecoveryCode,
    validatePassword,
    completeRecovery,
    goLogin
  }
}
