import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'

import { http } from '@/utils/axios'
import {
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
  const generating = ref(false)
  const revoking = ref(false)
  const errorMessage = ref('')

  const visible = computed({
    get: () => props.modelValue,
    set: value => emit('update:modelValue', value),
  })
  const busy = computed(() => generating.value || revoking.value)

  const loadStatus = async () => {
    if (!props.user?.ID) return false
    errorMessage.value = ''
    try {
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.get('/api/v1/user/security/recovery-codes', {
          params: { id: props.user.ID },
        })
        return assertSuccess(result, '读取安全码状态失败')
      })
      configured.value = Boolean(response.data?.configured)
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '读取安全码状态失败'
      }
      return false
    }
  }

  const generateCodes = async () => {
    if (busy.value || !props.user?.ID) return false
    generating.value = true
    errorMessage.value = ''
    try {
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.post('/api/v1/user/security/recovery-codes/generate', {
          user_id: props.user.ID,
          count: 10,
        })
        return assertSuccess(result, '安全码生成失败')
      })
      codes.value = Array.isArray(response.data?.codes) ? response.data.codes : []
      if (!codes.value.length) {
        throw new Error('服务器未返回安全码，请稍后重试')
      }
      configured.value = true
      emit('changed')
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '安全码生成失败'
      }
      return false
    } finally {
      generating.value = false
    }
  }

  const revokeCodes = async () => {
    if (busy.value || !props.user?.ID) return false
    revoking.value = true
    errorMessage.value = ''
    try {
      const response = await requestWithRecentAuthentication(async () => {
        const result = await http.post('/api/v1/user/security/recovery-codes/revoke', {
          user_id: props.user.ID,
        })
        return assertSuccess(result, '安全码撤销失败')
      })
      configured.value = false
      codes.value = []
      emit('changed')
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '安全码撤销失败'
      }
      return false
    } finally {
      revoking.value = false
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
    configured.value = Boolean(props.user?.RecoveryCodeConfigured)
    codes.value = []
    errorMessage.value = ''
  }

  const close = () => {
    if (busy.value) return
    visible.value = false
    reset()
  }

  watch(
    () => props.modelValue,
    value => {
      if (value) {
        reset()
        void loadStatus()
      }
    },
  )

  return {
    visible,
    configured,
    codes,
    generating,
    revoking,
    busy,
    errorMessage,
    close,
    generateCodes,
    revokeCodes,
    copyCodes,
  }
}
