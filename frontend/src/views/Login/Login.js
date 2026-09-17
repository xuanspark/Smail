import { reactive, ref } from "vue"

import { ElMessage } from "element-plus"

import { useRouter } from "vue-router"

import lang from "../../i18n/i18n"

import { http } from "@/utils/axios"

import { useGlobalStatusStore } from "@/stores/useGlobalStatusStore"
import { getSafeLoginRedirect } from "@/utils/navigation.mjs"

export function usePage() {
  const router = useRouter()
  const globalStatus = useGlobalStatusStore()
  const loading = ref(false)
  const form = reactive({
    account: "",
    password: "",
  })

  const onSubmit = async () => {
    if (!form.account || !form.password) {
      ElMessage.warning(lang.login_fill_required)
      return
    }
    loading.value = true
    try {
      const res = await http.post("/api/v1/login", form)
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
        return
      }
      const redirect = getSafeLoginRedirect(router.currentRoute.value.query.redirect)
      if (res.data?.pending) {
        globalStatus.setPendingLogin({ ...res.data, redirect })
        await router.replace({ name: "login-verification" })
        return
      }
      globalStatus.clearPendingLogin()
      globalStatus.setUser(res.data, true)
      await router.replace(redirect)
    } catch (error) {
      ElMessage.error(error.response?.status === 503
        ? "认证服务暂时繁忙，请稍后重试"
        : "登录请求失败，请稍后重试")
    } finally {
      loading.value = false
    }
  }

  const goForgotPassword = () => {
    window.location.assign('/public-password-recovery')
  }

  return {
    lang,
    form,
    onSubmit,
    loading,
    goForgotPassword
  }
}
