import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'

import { ElMessageBox, ElNotification } from 'element-plus'

import lang from '../../../i18n/i18n'
import { http } from '@/utils/axios'
import {
  confirmRecentAuthentication,
  isRecentAuthenticationCancelled,
  requestWithRecentAuthentication,
} from '@/utils/recentAuthentication'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { normalizeCreationOptions, serializeCredential, webAuthnSupported } from '@/utils/webauthn'
import { downloadRecoveryCodes } from '@/utils/recoveryCodeDownload'
import { RECOVERY_CODE_DISPLAY_MS } from '@/utils/recoveryCodeDisplay'

export function usePage() {
  const globalStatus = useGlobalStatusStore()
  const isSuperAdmin = computed(() => globalStatus.role === 'super_admin')
  const supported = webAuthnSupported()
  const webAuthnEnabled = computed(() => globalStatus.userInfos.webAuthnEnabled === true)
  const canReadGlobalPolicy = computed(() => globalStatus.hasCapability('config.read'))
  const sectionOptions = computed(() => {
    const options = []
    options.push({ value: 'profile', label: '个人资料' })
    if (canReadGlobalPolicy.value) {
      options.push({ value: 'security-policy', label: '安全策略' })
    }
    options.push({ value: 'password', label: lang.modify_pwd })
    options.push({ value: 'question', label: '密保问题' })
    options.push({ value: 'recovery', label: '安全码' })
    options.push({ value: 'external-email', label: '外部安全邮箱' })
    options.push({ value: 'totp', label: '手机动态码' })
    options.push({ value: 'webauthn', label: '安全密钥' })
    return globalStatus.userInfos.securitySetupRequired
      ? options.filter(item => ['recovery', 'external-email', 'totp'].includes(item.value))
      : options
  })
  const selectedSection = computed({
    get: () => globalStatus.securityActiveSection,
    set: value => { globalStatus.securityActiveSection = value }
  })

  const globalSecurityPolicyPage = ref(null)
  const selectSection = async value => {
    if (!sectionOptions.value.some(item => item.value === value) || value === selectedSection.value) return
    if (selectedSection.value === 'security-policy' && !await (globalSecurityPolicyPage.value?.beforeLeave?.() ?? true)) return
    selectedSection.value = value
  }

  watch(sectionOptions, options => {
    if (!options.some(item => item.value === selectedSection.value)) {
      selectedSection.value = options[0]?.value || ''
    }
  }, { immediate: true })

  const registering = ref(false)
  const credentialsLoading = ref(false)
  const credentials = ref([])
  const securityLoading = ref(false)
  const securityQuestion = reactive({ configured: false, question: '' })
  const questionForm = reactive({ question: '', answer: '' })
  const recoveryConfigured = ref(false)
  const generatedCodes = ref([])
  const recoveryLoading = ref(false)
  let recoveryCodeHideTimer = null
  let recoveryViewVersion = 0
  let pageActive = true

  const clearRecoveryCodes = () => {
    recoveryViewVersion += 1
    generatedCodes.value = []
    clearTimeout(recoveryCodeHideTimer)
    recoveryCodeHideTimer = null
  }

  const isRecoveryViewCurrent = version => (
    pageActive && selectedSection.value === 'recovery' && version === recoveryViewVersion
  )

  const handleVisibilityChange = () => {
    if (document.hidden) clearRecoveryCodes()
  }

  const requestRecoveryAction = (action, data, version) => (
    requestWithRecentAuthentication(() => {
      if (!isRecoveryViewCurrent(version)) return null
      return http.post(`/api/v1/security/credentials/recovery-codes/${action}`, data)
    })
  )

  const externalEmails = ref([])
  const externalListLoading = ref(false)
  const externalDialogVisible = ref(false)
  const passwordEmail = computed(() => {
    const verified = externalEmails.value.filter(item => item.status === 'verified')
    return verified.find(item => item.type === 'primary') || verified.find(item => item.type === 'backup')
  })
  const passwordSending = ref(false)
  const passwordSubmitting = ref(false)
  const passwordChallenge = ref('')
  const passwordCountdown = ref(0)
  let passwordTimer = null
  let lastNotificationKey = ''
  let lastNotificationAt = 0

  const ruleForm = reactive({ current_pwd: '', new_pwd: '', new_pwd2: '', code: '' })
  const rules = reactive({
    current_pwd: [{ required: true, message: lang.err_required_pwd, trigger: 'blur' }],
    new_pwd: [{ required: true, message: lang.err_required_pwd, trigger: 'blur' }],
    new_pwd2: [{ required: true, message: lang.err_required_pwd, trigger: 'blur' }],
  })

  const notify = (message, type = 'error') => {
    const notificationKey = `${type}:${String(message || '').trim()}`
    const now = Date.now()
    if (notificationKey === lastNotificationKey && now - lastNotificationAt < 1500) return
    lastNotificationKey = notificationKey
    lastNotificationAt = now
    ElNotification({ title: type === 'success' ? '操作成功' : '操作失败', message, type })
  }

  const loadCredentials = async () => {
    if (globalStatus.userInfos.securitySetupRequired) return
    if (!supported || !webAuthnEnabled.value) return
    credentialsLoading.value = true
    try {
      const res = await http.get('/api/v1/security/webauthn/credentials')
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '加载安全密钥失败')
      credentials.value = Array.isArray(res.data) ? res.data : []
    } catch (error) {
      notify(error?.message || '加载安全密钥失败')
    } finally {
      credentialsLoading.value = false
    }
  }

  const loadSecurityData = async () => {
    securityLoading.value = true
    try {
      const requests = [
        http.get('/api/v1/security/credentials/question'),
        http.get('/api/v1/security/external-email/list'),
      ]
      requests.push(http.get('/api/v1/security/credentials/recovery-codes'))
      const [question, external, recovery] = await Promise.all(requests)
      if (question.errorNo === 0 && question.data) {
        securityQuestion.configured = Boolean(question.data.configured)
        securityQuestion.question = question.data.question || ''
        questionForm.question = question.data.question || ''
      }
      if (recovery && recovery.errorNo === 0) recoveryConfigured.value = Boolean(recovery.data?.configured)
      if (external.errorNo === 0) externalEmails.value = Array.isArray(external.data) ? external.data : []
    } catch (error) {
      notify(error?.message || '读取账户安全设置失败')
    } finally {
      securityLoading.value = false
    }
  }

  const loadExternalEmails = async () => {
    externalListLoading.value = true
    try {
      const response = await http.get('/api/v1/security/external-email/list')
      if (response.errorNo !== 0) throw new Error(response.errorMsg || '读取外部邮箱失败')
      externalEmails.value = Array.isArray(response.data) ? response.data : []
      await globalStatus.refresh()
      return true
    } catch (error) {
      notify(error?.message || '读取外部邮箱失败')
      return false
    } finally {
      externalListLoading.value = false
    }
  }

  const registerCredential = async () => {
    if (!supported || !webAuthnEnabled.value || registering.value) return
    registering.value = true
    try {
      const prompt = await ElMessageBox.prompt('请为新安全密钥设置一个名称', '注册安全密钥', {
        confirmButtonText: '确定', cancelButtonText: '取消', inputPlaceholder: '例如：工作电脑',
        inputValidator: value => String(value || '').trim().length > 0 || '请输入密钥名称',
      })
      const begin = await http.post('/api/v1/security/webauthn/registration/begin', {})
      if (begin.errorNo !== 0) throw new Error(begin.errorMsg || '无法开始安全密钥注册')
      const credential = await navigator.credentials.create({ publicKey: normalizeCreationOptions(begin.data.options) })
      const finish = await http.post('/api/v1/security/webauthn/registration/finish', {
        session_id: begin.data.session_id,
        display_name: String(prompt.value).trim(),
        credential: serializeCredential(credential),
      })
      if (finish.errorNo !== 0) throw new Error(finish.errorMsg || '安全密钥注册失败')
      notify('安全密钥注册成功', 'success')
      await loadCredentials()
    } catch (error) {
      if (error !== 'cancel' && error !== 'close' && error?.name !== 'NotAllowedError') notify(error?.message || '安全密钥注册失败')
    } finally {
      registering.value = false
    }
  }

  const renameCredential = async item => {
    try {
      const prompt = await ElMessageBox.prompt('请输入新的密钥名称', '重命名', {
        confirmButtonText: '确定', cancelButtonText: '取消', inputValue: item.display_name,
        inputValidator: value => String(value || '').trim().length > 0 || '请输入密钥名称',
      })
      const res = await http.post('/api/v1/security/webauthn/credentials/rename', { id: item.id, display_name: String(prompt.value).trim() })
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '密钥重命名失败')
      notify('密钥重命名成功', 'success')
      await loadCredentials()
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '密钥重命名失败')
    }
  }

  const revokeCredential = async item => {
    try {
      await ElMessageBox.confirm(`确定要撤销密钥“${item.display_name}”吗？撤销后将不能用于登录`, '撤销安全密钥', {
        confirmButtonText: '确定撤销', cancelButtonText: '取消', type: 'warning',
      })
      const res = await http.post('/api/v1/security/webauthn/credentials/revoke', { id: item.id })
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '撤销安全密钥失败')
      notify('安全密钥已撤销', 'success')
      await loadCredentials()
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '撤销安全密钥失败')
    }
  }

  const saveSecurityQuestion = async () => {
    if (!questionForm.question.trim() || !questionForm.answer.trim()) {
      notify('请填写密保问题和答案')
      return
    }
    const res = await http.post('/api/v1/security/credentials/question', questionForm)
    if (res.errorNo !== 0) {
      notify(res.errorMsg || '密保问题设置失败')
      return
    }
    securityQuestion.configured = true
    securityQuestion.question = questionForm.question.trim()
    questionForm.answer = ''
    notify('密保问题已保存', 'success')
  }

  const generateRecoveryCodes = async () => {
    if (recoveryLoading.value) return
    clearRecoveryCodes()
    const version = recoveryViewVersion
    recoveryLoading.value = true
    try {
      await ElMessageBox.confirm('生成新恢复码会撤销当前仍有效的恢复码，请确认已准备好安全保存。', '生成离线恢复码', { type: 'warning' })
      if (!isRecoveryViewCurrent(version)) return
      const res = await requestRecoveryAction('generate', { count: 12 }, version)
      if (!isRecoveryViewCurrent(version)) return
      if (res.errorNo !== 0 || !Array.isArray(res.data?.codes)) throw new Error(res.errorMsg || '离线恢复码生成失败')
      generatedCodes.value = res.data.codes
      scheduleRecoveryCodeHide()
      recoveryConfigured.value = true
      await globalStatus.refresh()
      notify('恢复码已生成，请妥善保存；验证本人密码后可再次查看有效恢复码', 'success')
    } catch (error) {
      if (isRecentAuthenticationCancelled(error) || !isRecoveryViewCurrent(version)) return
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '离线恢复码生成失败')
    } finally {
      recoveryLoading.value = false
    }
  }

  const scheduleRecoveryCodeHide = () => {
    clearTimeout(recoveryCodeHideTimer)
    recoveryCodeHideTimer = setTimeout(clearRecoveryCodes, RECOVERY_CODE_DISPLAY_MS)
  }

  const revealRecoveryCodes = async () => {
    if (recoveryLoading.value) return
    clearRecoveryCodes()
    const version = recoveryViewVersion
    recoveryLoading.value = true
    try {
      await confirmRecentAuthentication()
      if (!isRecoveryViewCurrent(version)) return
      const res = await requestRecoveryAction('reveal', {}, version)
      if (!isRecoveryViewCurrent(version)) return
      if (res.errorNo !== 0 || !Array.isArray(res.data?.codes) || !res.data.codes.length) {
        throw new Error(res.errorMsg || '读取安全码失败')
      }
      generatedCodes.value = res.data.codes
      scheduleRecoveryCodeHide()
      notify('安全码已显示，60 秒后自动隐藏', 'success')
    } catch (error) {
      if (isRecentAuthenticationCancelled(error) || !isRecoveryViewCurrent(version)) return
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '读取安全码失败')
    } finally {
      recoveryLoading.value = false
    }
  }

  const saveRecoveryCodes = () => {
    if (!generatedCodes.value.length) return
    downloadRecoveryCodes(generatedCodes.value)
    notify('安全码已保存到本地文件，文件不会上传服务器', 'success')
  }

  const revokeRecoveryCodes = async () => {
    if (recoveryLoading.value) return
    clearRecoveryCodes()
    const version = recoveryViewVersion
    recoveryLoading.value = true
    try {
      await ElMessageBox.confirm('撤销后现有离线恢复码将立即失效，是否继续？', '撤销离线恢复码', { type: 'warning' })
      if (!isRecoveryViewCurrent(version)) return
      const res = await requestRecoveryAction('revoke', {}, version)
      if (!isRecoveryViewCurrent(version)) return
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '撤销恢复码失败')
      recoveryConfigured.value = false
      generatedCodes.value = []
      clearTimeout(recoveryCodeHideTimer)
      await globalStatus.refresh()
      notify('离线恢复码已撤销', 'success')
    } catch (error) {
      if (isRecentAuthenticationCancelled(error) || !isRecoveryViewCurrent(version)) return
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '撤销恢复码失败')
    } finally {
      recoveryLoading.value = false
    }
  }

  const openExternalEmailDialog = () => {
    externalDialogVisible.value = true
  }

  const handleExternalEmailBound = async () => {
    await loadExternalEmails()
    notify('外部安全邮箱绑定成功', 'success')
  }

  const disableExternalEmail = async item => {
    try {
      await ElMessageBox.confirm(`确定停用 ${item.email} 吗？停用后不能用于登录验证和恢复。`, '停用外部邮箱', { type: 'warning' })
      const res = await http.post('/api/v1/security/external-email/disable', { id: item.id })
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '外部邮箱停用失败')
      await loadSecurityData()
      await globalStatus.refresh()
      notify('外部邮箱已停用', 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '外部邮箱停用失败')
    }
  }

  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

  const submit = async () => {
    if (passwordSubmitting.value || passwordSending.value) return
    if (!ruleForm.new_pwd || (!isSuperAdmin.value && !ruleForm.current_pwd)) {
      notify('请填写完整的密码信息')
      return
    }
    const passwordBytes = new TextEncoder().encode(ruleForm.new_pwd).length
    if (passwordBytes < 8 || passwordBytes > 12) {
      notify('密码长度必须为 8-12 字节')
      return
    }
    if (ruleForm.new_pwd !== ruleForm.new_pwd2) {
      notify(lang.err_pwd_diff)
      return
    }
    if (isSuperAdmin.value && (!passwordEmail.value || !passwordChallenge.value || !/^\d{8}$/.test(ruleForm.code.trim()))) {
      notify('请获取并输入安全邮箱中的 8 位验证码')
      return
    }
    passwordSubmitting.value = true
    try {
      const payload = isSuperAdmin.value
        ? { password: ruleForm.new_pwd, challenge_id: passwordChallenge.value, code: ruleForm.code.trim() }
        : { current_password: ruleForm.current_pwd, password: ruleForm.new_pwd }
      const res = await http.post('/api/v1/settings/modify_password', payload)
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '密码修改失败')
      Object.assign(ruleForm, { current_pwd: '', new_pwd: '', new_pwd2: '', code: '' })
      passwordChallenge.value = ''
      notify('密码修改成功，请重新登录；原安全凭据已失效', 'success')
      globalStatus.clear()
      location.assign('/#/login')
    } catch (error) {
      notify(error?.message || '密码修改失败，请重新获取验证码后重试')
    } finally {
      passwordSubmitting.value = false
    }
  }

  const sendPasswordCode = async () => {
    if (passwordSending.value || passwordSubmitting.value || passwordCountdown.value > 0 || !passwordEmail.value) return
    passwordSending.value = true
    passwordChallenge.value = ''
    ruleForm.code = ''
    try {
      const res = await http.post('/api/v1/security/verification/request', {
        purpose: 'password_change', account: globalStatus.userInfos.account,
      })
      if (res.errorNo !== 0 || !res.data?.id) throw new Error(res.errorMsg || '验证码发送失败')
      passwordChallenge.value = res.data.id
      passwordCountdown.value = 60
      const deadline = Date.now() + 60000
      clearInterval(passwordTimer)
      passwordTimer = setInterval(() => {
        passwordCountdown.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
        if (!passwordCountdown.value) clearInterval(passwordTimer)
      }, 1000)
      notify(`验证码已发送至 ${res.data.email || passwordEmail.value.email}`, 'success')
    } catch (error) {
      notify(error?.message || '验证码发送失败')
    } finally {
      passwordSending.value = false
    }
  }

  watch(() => passwordEmail.value?.id, () => {
    passwordChallenge.value = ''
    ruleForm.code = ''
  })
  onUnmounted(() => {
    pageActive = false
    clearRecoveryCodes()
    document.removeEventListener('visibilitychange', handleVisibilityChange)
    clearInterval(passwordTimer)
  })

  watch(selectedSection, section => {
    if (section !== 'recovery') {
      clearRecoveryCodes()
    }
  })

  onMounted(async () => {
    document.addEventListener('visibilitychange', handleVisibilityChange)
    await loadSecurityData()
  })

  watch([selectedSection, webAuthnEnabled], ([section]) => {
    if (section === 'webauthn') loadCredentials()
  }, { immediate: true })

  return {
    lang, isSuperAdmin, supported, webAuthnEnabled, sectionOptions, selectedSection, selectSection,
    globalSecurityPolicyPage,
    passwordEmail, passwordSending, passwordSubmitting, passwordCountdown, sendPasswordCode,
    registering, credentialsLoading, credentials, securityLoading, recoveryLoading,
    securityQuestion, questionForm, recoveryConfigured, generatedCodes, externalEmails, externalListLoading,
    externalDialogVisible, ruleForm, rules, submit, registerCredential, renameCredential, revokeCredential,
    saveSecurityQuestion, generateRecoveryCodes, revealRecoveryCodes, saveRecoveryCodes, revokeRecoveryCodes, openExternalEmailDialog,
    handleExternalEmailBound,
    disableExternalEmail, formatTime,
  }
}
