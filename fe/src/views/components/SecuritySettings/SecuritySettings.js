import { computed, onMounted, reactive, ref, watch } from 'vue'

import { ElMessageBox, ElNotification } from 'element-plus'

import lang from '../../../i18n/i18n'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { normalizeCreationOptions, serializeCredential, webAuthnSupported } from '@/utils/webauthn'

export function usePage() {
  const globalStatus = useGlobalStatusStore()
  const isSuperAdmin = computed(() => globalStatus.role === 'super_admin')
  const supported = webAuthnSupported()
  const sectionOptions = computed(() => {
    const options = []
    if (!isSuperAdmin.value) {
      options.push({ value: 'password', label: lang.modify_pwd })
    }
    options.push({ value: 'question', label: '密保问题' })
    if (!isSuperAdmin.value) {
      options.push({ value: 'recovery', label: '离线恢复码' })
    }
    options.push({ value: 'external-email', label: '外部安全邮箱' })
    options.push({ value: 'webauthn', label: '安全密钥' })
    options.push({ value: 'logout', label: lang.logout })
    return options
  })
  const selectedSection = ref('question')

  const selectSection = value => {
    if (sectionOptions.value.some(item => item.value === value)) {
      selectedSection.value = value
    }
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
  const externalEmails = ref([])
  const externalLoading = ref(false)
  let lastNotificationKey = ''
  let lastNotificationAt = 0
  const bindForm = reactive({ email: '', type: 'backup', challengeId: '', bindingId: 0, code: '' })

  const ruleForm = reactive({ current_pwd: '', new_pwd: '', new_pwd2: '' })
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
    if (!supported) return
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
      if (!isSuperAdmin.value) {
        requests.push(http.get('/api/v1/security/credentials/recovery-codes'))
      }
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

  const registerCredential = async () => {
    if (!supported || registering.value) return
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
    try {
      await ElMessageBox.confirm('生成新恢复码会撤销当前仍有效的恢复码，请确认已准备好安全保存。', '生成离线恢复码', { type: 'warning' })
      const res = await http.post('/api/v1/security/credentials/recovery-codes/generate', { count: 10 })
      if (res.errorNo !== 0 || !Array.isArray(res.data?.codes)) throw new Error(res.errorMsg || '离线恢复码生成失败')
      generatedCodes.value = res.data.codes
      recoveryConfigured.value = true
      notify('恢复码已生成，请仅保存这一次显示的内容', 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '离线恢复码生成失败')
    }
  }

  const revokeRecoveryCodes = async () => {
    try {
      await ElMessageBox.confirm('撤销后现有离线恢复码将立即失效，是否继续？', '撤销离线恢复码', { type: 'warning' })
      const res = await http.post('/api/v1/security/credentials/recovery-codes/revoke', {})
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '撤销恢复码失败')
      recoveryConfigured.value = false
      generatedCodes.value = []
      notify('离线恢复码已撤销', 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '撤销恢复码失败')
    }
  }

  const bindExternalEmail = async () => {
    if (!bindForm.email.trim()) {
      notify('请输入外部安全邮箱')
      return
    }
    externalLoading.value = true
    try {
      const res = await http.post('/api/v1/security/external-email/bind', { email: bindForm.email.trim(), type: bindForm.type })
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '外部邮箱绑定失败')
      bindForm.challengeId = res.data.challenge_id
      bindForm.bindingId = res.data.id
      const prompt = await ElMessageBox.prompt(`验证码已发送到 ${res.data.email}，请输入验证码`, '验证外部邮箱', {
        confirmButtonText: '验证', cancelButtonText: '取消', inputPlaceholder: '请输入验证码',
      })
      bindForm.code = String(prompt.value).trim()
      const verified = await http.post('/api/v1/security/external-email/verify', {
        id: bindForm.challengeId, binding_id: bindForm.bindingId, email: bindForm.email.trim(), code: bindForm.code,
      })
      if (verified.errorNo !== 0) throw new Error(verified.errorMsg || '外部邮箱验证失败')
      bindForm.email = ''
      bindForm.challengeId = ''
      bindForm.bindingId = 0
      await loadSecurityData()
      notify('外部安全邮箱绑定成功', 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '外部邮箱绑定失败')
    } finally {
      externalLoading.value = false
    }
  }

  const disableExternalEmail = async item => {
    try {
      await ElMessageBox.confirm(`确定停用 ${item.email} 吗？停用后不能用于登录验证和恢复。`, '停用外部邮箱', { type: 'warning' })
      const res = await http.post('/api/v1/security/external-email/disable', { id: item.id })
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '外部邮箱停用失败')
      await loadSecurityData()
      notify('外部邮箱已停用', 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '外部邮箱停用失败')
    }
  }

  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
  const logout = () => http.post('/api/v1/logout', {}).then(() => location.reload())

  const submit = async () => {
    if (!ruleForm.current_pwd || !ruleForm.new_pwd) return
    if (ruleForm.new_pwd !== ruleForm.new_pwd2) {
      notify(lang.err_pwd_diff)
      return
    }
    const res = await http.post('/api/v1/settings/modify_password', { current_password: ruleForm.current_pwd, password: ruleForm.new_pwd })
    notify(res.errorNo === 0 ? '密码修改成功' : res.errorMsg, res.errorNo === 0 ? 'success' : 'error')
    if (res.errorNo === 0) Object.assign(ruleForm, { current_pwd: '', new_pwd: '', new_pwd2: '' })
  }

  onMounted(async () => {
    await Promise.all([loadSecurityData(), loadCredentials()])
  })

  return {
    lang, isSuperAdmin, supported, sectionOptions, selectedSection, selectSection,
    registering, credentialsLoading, credentials, securityLoading,
    securityQuestion, questionForm, recoveryConfigured, generatedCodes, externalEmails, externalLoading,
    bindForm, ruleForm, rules, submit, registerCredential, renameCredential, revokeCredential,
    saveSecurityQuestion, generateRecoveryCodes, revokeRecoveryCodes, bindExternalEmail,
    disableExternalEmail, formatTime, logout,
  }
}
