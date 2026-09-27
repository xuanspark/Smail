import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { http } from '@/utils/axios'
import {
  confirmRecentAuthentication,
  isRecentAuthenticationCancelled,
  requestWithRecentAuthentication,
} from '@/utils/recentAuthentication'

export const recoveryCodeDialogProps = {
  modelValue: {
    type: Boolean,
    default: false,
  },
  user: {
    type: Object,
    default: null,
  },
}

function assertSuccess(response, fallback) {
  if (!response || response.errorNo !== 0) {
    const error = new Error(response?.errorMsg || fallback)
    if (typeof response?.data?.code === 'string') {
      error.apiErrorCode = response.data.code
    }
    throw error
  }
  return response
}

export function useRecoveryCodeDialog(props, emit) {
  const configured = ref(false)
  const codes = ref([])
  const loading = ref(false)
  const revealing = ref(false)
  const revealed = ref(false)
  const generating = ref(false)
  const revoking = ref(false)
  const errorMessage = ref('')
  let hideTimer = null
  let viewVersion = 0
  let active = true

  const clearCodes = () => {
    codes.value = []
    revealed.value = false
    clearTimeout(hideTimer)
    hideTimer = null
  }

  const scheduleHide = () => {
    clearTimeout(hideTimer)
    hideTimer = setTimeout(clearCodes, 60000)
  }

  const isCurrentView = (version, userID) => (
    active && props.modelValue && version === viewVersion && props.user?.ID === userID
  )

  const visible = computed({
    get: () => props.modelValue,
    set: value => emit('update:modelValue', value),
  })
  const busy = computed(() => loading.value || revealing.value || generating.value || revoking.value)

  const loadStatus = async () => {
    if (!props.user?.ID) return false
    const userID = props.user.ID
    const version = viewVersion
    loading.value = true
    errorMessage.value = ''
    try {
      const response = await requestWithRecentAuthentication(async () => {
        if (!isCurrentView(version, userID)) return null
        const result = await http.get('/api/v1/user/security/recovery-codes', {
          params: { id: userID },
        })
        return assertSuccess(result, '读取安全码状态失败')
      })
      if (!isCurrentView(version, userID)) return false
      configured.value = Boolean(response.data?.configured)
      return true
    } catch (error) {
      if (!isCurrentView(version, userID)) return false
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '读取安全码状态失败'
      }
      return false
    } finally {
      if (version === viewVersion) loading.value = false
    }
  }

  const generateCodes = async () => {
    if (busy.value || !props.user?.ID) return false
    const userID = props.user.ID
    const version = viewVersion
    clearCodes()
    generating.value = true
    errorMessage.value = ''
    try {
      const response = await requestWithRecentAuthentication(async () => {
        if (!isCurrentView(version, userID)) return null
        const result = await http.post('/api/v1/user/security/recovery-codes/generate', {
          user_id: userID,
          count: 10,
        })
        return assertSuccess(result, '安全码生成失败')
      })
      if (!isCurrentView(version, userID)) return false
      codes.value = Array.isArray(response.data?.codes) ? response.data.codes : []
      if (!codes.value.length) {
        throw new Error('服务器未返回安全码，请稍后重试')
      }
      configured.value = true
      scheduleHide()
      emit('changed')
      return true
    } catch (error) {
      if (!isCurrentView(version, userID)) return false
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '安全码生成失败'
      }
      return false
    } finally {
      if (version === viewVersion) generating.value = false
    }
  }

  const revokeCodes = async () => {
    if (busy.value || !props.user?.ID) return false
    const userID = props.user.ID
    const version = viewVersion
    clearCodes()
    revoking.value = true
    errorMessage.value = ''
    try {
      await requestWithRecentAuthentication(async () => {
        if (!isCurrentView(version, userID)) return null
        const result = await http.post('/api/v1/user/security/recovery-codes/revoke', {
          user_id: userID,
        })
        return assertSuccess(result, '安全码撤销失败')
      })
      if (!isCurrentView(version, userID)) return false
      configured.value = false
      codes.value = []
      emit('changed')
      return true
    } catch (error) {
      if (!isCurrentView(version, userID)) return false
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '安全码撤销失败'
      }
      return false
    } finally {
      if (version === viewVersion) revoking.value = false
    }
  }

  const revealCodes = async () => {
    if (busy.value || !configured.value || !props.user?.ID) return false
    const userID = props.user.ID
    const version = viewVersion
    clearCodes()
    revealing.value = true
    errorMessage.value = ''
    try {
      await confirmRecentAuthentication()
      if (!isCurrentView(version, userID)) return false
      const response = await requestWithRecentAuthentication(async () => {
        if (!isCurrentView(version, userID)) return null
        const result = await http.post('/api/v1/user/security/recovery-codes/reveal', {
          user_id: userID,
        })
        return assertSuccess(result, '读取安全码失败')
      })
      if (!isCurrentView(version, userID)) return false
      if (!Array.isArray(response.data?.codes) || !response.data.codes.length) {
        throw new Error('当前没有可查看的有效安全码')
      }
      codes.value = response.data.codes
      revealed.value = true
      scheduleHide()
      return true
    } catch (error) {
      if (isCurrentView(version, userID) && !isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '读取安全码失败'
      }
      return false
    } finally {
      if (version === viewVersion) revealing.value = false
    }
  }

  const copyCodes = async () => {
    if (!codes.value.length) return
    try {
      await navigator.clipboard.writeText(codes.value.join('\n'))
      ElMessage.success('安全码已复制')
    } catch {
      errorMessage.value = '复制失败，请手动选择安全码复制'
    }
  }

  const reset = () => {
    viewVersion += 1
    clearCodes()
    loading.value = false
    revealing.value = false
    generating.value = false
    revoking.value = false
    configured.value = Boolean(props.user?.RecoveryCodeConfigured)
    errorMessage.value = ''
  }

  const close = () => {
    if (busy.value) return
    visible.value = false
    reset()
  }

  watch(
    [() => props.modelValue, () => props.user?.ID],
    ([value]) => {
      reset()
      if (value) {
        void loadStatus()
      }
    },
    { immediate: true, flush: 'sync' },
  )

  const handleVisibilityChange = () => {
    if (document.hidden) reset()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', handleVisibilityChange)
  })

  onUnmounted(() => {
    active = false
    viewVersion += 1
    clearCodes()
    document.removeEventListener('visibilitychange', handleVisibilityChange)
  })

  return {
    visible,
    configured,
    codes,
    loading,
    revealing,
    revealed,
    clearCodes,
    generating,
    revoking,
    busy,
    errorMessage,
    close,
    generateCodes,
    revealCodes,
    revokeCodes,
    copyCodes,
  }
}
