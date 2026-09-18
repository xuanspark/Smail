import { computed, onBeforeUnmount, onMounted, ref } from "vue"

import { ElMessage } from "element-plus"

import { useRouter } from "vue-router"

import { useGlobalStatusStore } from "@/stores/useGlobalStatusStore"
import { http } from "@/utils/axios"
import { getSafeLoginRedirect } from "@/utils/navigation.mjs"

export function usePage() {
  const router = useRouter()
  const globalStatus = useGlobalStatusStore()
  const pending = computed(() => globalStatus.pendingLogin)
  const code = ref("")
  const loading = ref(false)
  const resending = ref(false)
  const method = ref("")
  const retrySeconds = ref(0)
  const remainingSeconds = ref(0)
  const errorMessage = ref("")
  let timer = null
  let polling = false
  let disposed = false

  const expiresAt = computed(() => pending.value?.expiresAt || "")
  const methods = computed(() => pending.value?.methods || [])
  const sending = computed(() => pending.value?.deliveryStatus === "sending")
  const deliveryMessage = computed(() => {
    switch (pending.value?.deliveryStatus) {
      case "sending": return "正在发送邮件，您可以等待，也可以切换到手机动态码。"
      case "sent": return "收件服务器已接收邮件，请检查收件箱和垃圾邮件。"
      case "failed": return pending.value?.deliveryError || "邮件发送失败，请稍后重试。"
      default: return "点击发送验证码后，我们会向绑定邮箱发送邮件。"
    }
  })

  const savePending = (data, id) => {
    if (disposed || pending.value?.challengeId !== id) return
    globalStatus.setPendingLogin({ ...data, redirect: pending.value.redirect })
    updateRemaining()
  }

  const pollStatus = async () => {
    if (polling || !sending.value || remainingSeconds.value <= 0) return
    polling = true
    const id = pending.value.challengeId
    try {
      const result = await http.post("/api/v1/security/login/status", {
        id, account: pending.value.account
      }, { timeout: 4500 })
      if (result.errorNo !== 0) throw new Error(result.errorMsg || "无法查询发送状态")
      savePending(result.data, id)
      errorMessage.value = ""
    } catch (error) {
      if (!disposed) errorMessage.value = "发送状态暂时无法更新，请检查网络；无需重复发送。"
    } finally {
      polling = false
    }
  }

  const chooseMethod = value => {
    if (!methods.value.includes(value) || loading.value) return
    method.value = value
    code.value = ""
    errorMessage.value = ""
  }

  const updateRemaining = () => {
    const timestamp = Date.parse(expiresAt.value)
    remainingSeconds.value = Number.isFinite(timestamp)
      ? Math.max(0, Math.ceil((timestamp - Date.now()) / 1000))
      : 0
    retrySeconds.value = Math.max(0, Math.ceil((Date.parse(pending.value?.retryAt) - Date.now()) / 1000)) || 0
  }

  const startTimer = () => {
    updateRemaining()
    timer = window.setInterval(() => {
      updateRemaining()
      pollStatus()
    }, 1000)
  }

  const stopTimer = () => {
    if (timer !== null) {
      window.clearInterval(timer)
      timer = null
    }
  }

  const goBackToLogin = async () => {
    globalStatus.clearPendingLogin()
    await router.replace({ name: "login" })
  }

  const verifyCode = async () => {
    if (!pending.value?.challengeId) {
      await goBackToLogin()
      return
    }
    if (loading.value || !method.value) return
    const pattern = method.value === "totp" ? /^\d{6}$/ : /^\d{8}$/
    if (!pattern.test(code.value.trim())) {
      ElMessage.warning(method.value === "totp" ? "请输入六位手机动态码" : "请输入八位邮箱验证码")
      return
    }
    if (remainingSeconds.value <= 0) {
      errorMessage.value = "验证码已过期，请重新登录"
      ElMessage.warning(errorMessage.value)
      return
    }

    loading.value = true
    errorMessage.value = ""
    try {
      const response = await http.post("/api/v1/security/login/verify", {
        id: pending.value.challengeId,
        account: pending.value.account,
        code: code.value.trim(),
        method: method.value
      }, { timeout: 4500 })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || "验证码验证失败")
      }
      const redirect = getSafeLoginRedirect(pending.value.redirect)
      globalStatus.clearPendingLogin()
      globalStatus.setUser(response.data, true)
      await router.replace(redirect)
    } catch (error) {
      errorMessage.value = error?.message || "验证码验证失败，请重试"
      ElMessage.error(errorMessage.value)
    } finally {
      loading.value = false
    }
  }

  const resendCode = async () => {
    if (!pending.value?.challengeId || resending.value || sending.value || retrySeconds.value > 0) {
      return
    }
    resending.value = true
    errorMessage.value = ""
    try {
      const id = pending.value.challengeId
      const response = await http.post("/api/v1/security/login/resend", {
        id,
        account: pending.value.account
      }, { timeout: 4500 })
      if (response.errorNo !== 0 || !response.data?.pending) {
        throw new Error(response.errorMsg || "验证码发送失败")
      }
      savePending(response.data, id)
      code.value = ""
    } catch (error) {
      if (error?.code === 'ECONNABORTED' || error?.code === 'ERR_NETWORK') {
        errorMessage.value = "发送请求未确认，正在查询状态，请勿重复点击。"
        if (pending.value) pending.value.deliveryStatus = "sending"
      } else {
        errorMessage.value = error?.message || "验证码发送失败，请稍后重试"
      }
      ElMessage.error(errorMessage.value)
    } finally {
      resending.value = false
    }
  }

  onMounted(() => {
    if (!pending.value?.challengeId) {
      router.replace({ name: "login" })
      return
    }
    startTimer()
  })

  onBeforeUnmount(() => {
    disposed = true
    stopTimer()
  })

  return {
    pending,
    code,
    loading,
    resending,
    method,
    methods,
    sending,
    retrySeconds,
    deliveryMessage,
    chooseMethod,
    remainingSeconds,
    errorMessage,
    verifyCode,
    resendCode,
    goBackToLogin
  }
}
