import { computed, ref } from "vue"
import { useRouter } from "vue-router"

import { useGlobalStatusStore } from "@/stores/useGlobalStatusStore"
import { AUTH_STATUS } from "@/utils/auth.mjs"
import { createLoginLocation, createSetupLocation, getSafeLoginRedirect } from "@/utils/navigation.mjs"

export function useStartupStatus() {
  const router = useRouter()
  const globalStatus = useGlobalStatusStore()
  const retrying = ref(false)

  const isLoading = computed(() => globalStatus.authLoading || retrying.value)
  const hasStartupError = computed(() => Boolean(globalStatus.startupError))
  const title = computed(() => {
    if (hasStartupError.value) {
      return "页面加载失败"
    }
    if (isLoading.value) {
      return "正在恢复登录状态"
    }
    return "认证服务暂时不可用"
  })
  const description = computed(() => {
    if (hasStartupError.value) {
      return globalStatus.startupError
    }
    if (isLoading.value) {
      return "正在确认当前登录会话，请稍候。"
    }
    return globalStatus.authError || "暂时无法确认登录状态，请检查网络后重试。"
  })
  const buttonLabel = computed(() => hasStartupError.value ? "刷新页面" : "重新检查")

  const retry = async () => {
    if (isLoading.value) {
      return
    }
    if (hasStartupError.value) {
      window.location.reload()
      return
    }

    retrying.value = true
    const redirect = getSafeLoginRedirect(globalStatus.authRetryPath)
    try {
      const status = await globalStatus.refresh()
      if (status === AUTH_STATUS.AUTHENTICATED) {
        globalStatus.clearAuthRetryPath()
        await router.replace(redirect)
      } else if (status === AUTH_STATUS.UNAUTHENTICATED) {
        globalStatus.clearAuthRetryPath()
        await router.replace(createLoginLocation(redirect))
      } else if (status === AUTH_STATUS.NEEDS_SETUP) {
        globalStatus.clearAuthRetryPath()
        await router.replace(createSetupLocation(redirect))
      }
    } catch (error) {
      console.error("Authentication retry failed", error)
      globalStatus.setStartupError("页面加载失败，请刷新后重试")
    } finally {
      retrying.value = false
    }
  }

  return {
    buttonLabel,
    description,
    isLoading,
    retry,
    title
  }
}
