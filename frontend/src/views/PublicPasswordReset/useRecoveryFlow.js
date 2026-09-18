import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { isValidPasswordLength } from '@/utils/password'
import { postPublicRecovery } from '@/utils/publicRecoveryHttp'

export function usePage() {
  const page = ref('request')
  const loading = ref(false)
  const errorMessage = ref('')
  const notice = ref('')
  const identity = ref(null)
  const token = ref('')
  const countdown = ref(0)
  let timer
  let version = 0
  const form = reactive({ identifier: '', method: 'email', code: '', newPassword: '', confirmPassword: '' })
  const roleLabel = computed(() => ({ super_admin: '超级管理员', admin: '管理员', user: '普通用户' })[identity.value?.role] || '')
  const passwordMismatch = computed(() => !!form.confirmPassword && form.newPassword !== form.confirmPassword)
  const canSubmitReset = computed(() => isValidPasswordLength(form.newPassword) && form.newPassword === form.confirmPassword)
  const canVerify = computed(() => form.method === 'email' ? /^\d{8}$/.test(form.code.trim()) : !!form.code.trim())
  watch(() => form.method, () => { form.code = ''; errorMessage.value = '' })

  const request = async (endpoint, payload) => {
    const result = await postPublicRecovery(endpoint, payload)
    if (!result.ok) throw new Error(result.data?.message || (result.status >= 500 ? '服务暂时不可用，请稍后重试' : '操作失败，请重试'))
    return result.data
  }

  const run = async action => {
    if (loading.value) return
    loading.value = true
    errorMessage.value = ''
    const current = version
    try { await action(() => current === version) } catch (error) {
      if (current === version) errorMessage.value = error?.message || '网络异常，请稍后重试'
    } finally { if (current === version) loading.value = false }
  }

  const requestReset = () => {
    if (!form.identifier.trim()) return
    return run(async active => {
      const data = await request('begin', { identifier: form.identifier.trim() })
      if (!active()) return
      identity.value = data
      token.value = data.token || ''
      form.method = data.methods?.[0] || 'email'
      page.value = data.role === 'super_admin' ? 'blocked' : 'verify'
    })
  }

  const sendCode = () => {
    if (countdown.value || !token.value) return
    return run(async active => {
      const data = await request('send-code', { token: token.value })
      if (!active() || !data.sent) return
      notice.value = '验证码已发送，请查看绑定邮箱'
      form.code = ''
      const deadline = Date.now() + 60000
      countdown.value = 60
      clearInterval(timer)
      timer = setInterval(() => {
        countdown.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
        if (!countdown.value) clearInterval(timer)
      }, 1000)
    })
  }

  const verifyIdentity = () => {
    if (!canVerify.value) return
    return run(async active => {
      const data = await request('verify', { token: token.value, method: form.method, code: form.code.trim() })
      if (!active()) return
      if (!data.token) throw new Error('未取得重置凭证，请重新验证')
      token.value = data.token
      form.code = ''
      notice.value = ''
      page.value = 'reset'
    })
  }

  const completeReset = () => {
    if (!canSubmitReset.value || page.value !== 'reset') return
    return run(async active => {
      const data = await request('reset', { token: token.value, new_password: form.newPassword })
      if (!active()) return
      if (!data.reset) throw new Error('无法确认重置结果，请重试')
      token.value = ''
      form.newPassword = ''
      form.confirmPassword = ''
      page.value = 'success'
    })
  }

  const goRequest = () => {
    version++
    clearInterval(timer)
    countdown.value = 0
    loading.value = false
    token.value = ''
    identity.value = null
    errorMessage.value = ''
    notice.value = ''
    form.code = ''
    form.newPassword = ''
    form.confirmPassword = ''
    page.value = 'request'
  }
  const goLogin = () => window.location.assign(document.querySelector('#app')?.dataset.loginUrl || '/#/login')
  onBeforeUnmount(goRequest)
  return { page, loading, errorMessage, notice, identity, countdown, form, roleLabel, passwordMismatch, canSubmitReset, canVerify, requestReset, sendCode, verifyIdentity, completeReset, goRequest, goLogin }
}
