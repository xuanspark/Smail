import { computed, onBeforeUnmount, onMounted, ref } from "vue"

import { ElMessage } from "element-plus"

import { useRouter } from "vue-router"

import { useGlobalStatusStore } from "@/stores/useGlobalStatusStore"
import { http } from "@/utils/axios"
import { getSafeLoginRedirect } from "@/utils/navigation.mjs"
import {
  normalizeRequestOptions,
  serializeCredential,
  webAuthnSupported
} from "@/utils/webauthn"

export function usePage() {
  const router = useRouter()
  const globalStatus = useGlobalStatusStore()
  const pending = computed(() => globalStatus.pendingLogin)
  const code = ref("")
  const loading = ref(false)
  const resending = ref(false)
  const webAuthnLoading = ref(false)
  const remainingSeconds = ref(0)
  const errorMessage = ref("")
  let timer = null

  const expiresAt = computed(() => pending.value?.expiresAt || "")
  const webAuthnAvailable = computed(() => webAuthnSupported())

  const updateRemaining = () => {
    const timestamp = Date.parse(expiresAt.value)
    remainingSeconds.value = Number.isFinite(timestamp)
      ? Math.max(0, Math.ceil((timestamp - Date.now()) / 1000))
      : 0
  }

  const startTimer = () => {
    updateRemaining()
    timer = window.setInterval(updateRemaining, 1000)
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
    if (!code.value.trim()) {
      ElMessage.warning("请输入邮箱验证码")
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
        code: code.value.trim()
      })
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
    if (!pending.value?.challengeId || resending.value) {
      return
    }
    resending.value = true
    errorMessage.value = ""
    try {
      const response = await http.post("/api/v1/security/login/resend", {
        id: pending.value.challengeId,
        account: pending.value.account
      })
      if (response.errorNo !== 0 || !response.data?.pending) {
        throw new Error(response.errorMsg || "验证码发送失败")
      }
      globalStatus.setPendingLogin({
        ...response.data,
        redirect: pending.value.redirect
      })
      code.value = ""
      ElMessage.success("验证码已重新发送")
      stopTimer()
      startTimer()
    } catch (error) {
      errorMessage.value = error?.message || "验证码发送失败，请稍后重试"
      ElMessage.error(errorMessage.value)
    } finally {
      resending.value = false
    }
  }

  const verifyWithWebAuthn = async () => {
    if (!webAuthnAvailable.value || webAuthnLoading.value) {
      return
    }
    webAuthnLoading.value = true
    errorMessage.value = ""
    try {
      const begin = await http.post("/api/v1/security/webauthn/login/begin", {
        challenge_id: pending.value.challengeId,
        account: pending.value.account
      })
      if (begin.errorNo !== 0) {
        throw new Error(begin.errorMsg || "安全密钥登录不可用")
      }
      const credential = await navigator.credentials.get({
        publicKey: normalizeRequestOptions(begin.data.options)
      })
      const finish = await http.post("/api/v1/security/webauthn/login/finish", {
        challenge_id: pending.value.challengeId,
        account: pending.value.account,
        session_id: begin.data.session_id,
        credential: serializeCredential(credential)
      })
      if (finish.errorNo !== 0) {
        throw new Error(finish.errorMsg || "安全密钥验证失败")
      }
      const redirect = getSafeLoginRedirect(pending.value?.redirect)
      globalStatus.clearPendingLogin()
      globalStatus.setUser(finish.data, true)
      await router.replace(redirect)
    } catch (error) {
      if (error?.name !== "NotAllowedError") {
        errorMessage.value = error?.message || "安全密钥验证失败，请重试"
        ElMessage.error(errorMessage.value)
      }
    } finally {
      webAuthnLoading.value = false
    }
  }

  onMounted(() => {
    if (!pending.value?.challengeId) {
      router.replace({ name: "login" })
      return
    }
    startTimer()
  })

  onBeforeUnmount(stopTimer)

  return {
    pending,
    code,
    loading,
    resending,
    webAuthnLoading,
    remainingSeconds,
    errorMessage,
    webAuthnAvailable,
    verifyCode,
    resendCode,
    verifyWithWebAuthn,
    goBackToLogin
  }
}
