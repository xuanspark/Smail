import { onBeforeUnmount, onMounted, ref } from 'vue'
import QRCode from 'qrcode'
import { ElMessage } from 'element-plus'
import { http } from '@/utils/axios'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'

export function useTOTPSettings() {
  const enabled = ref(false)
  const ready = ref(false)
  const busy = ref(false)
  const enrollment = ref(null)
  const qrCode = ref('')
  const code = ref('')
  const errorMessage = ref('')
  let disposed = false

  const clearEnrollment = () => {
    enrollment.value = null
    qrCode.value = ''
    code.value = ''
  }

  const load = async () => {
    const result = await http.get('/api/v1/security/totp', { timeout: 4500 })
    if (result.errorNo !== 0) throw new Error(result.errorMsg || '无法读取动态码状态')
    if (disposed) return
    enabled.value = result.data.enabled
    ready.value = true
  }

  const perform = async action => {
    if (busy.value) return
    if (['confirm', 'disable'].includes(action) && !/^\d{6}$/.test(code.value)) {
      ElMessage.warning('请输入手机验证器生成的六位动态码')
      return
    }
    busy.value = true
    errorMessage.value = ''
    try {
      const result = await requestWithRecentAuthentication(() => http.post('/api/v1/security/totp', {
        action, id: enrollment.value?.id, code: code.value
      }, { timeout: 4500 }))
      if (result.errorNo !== 0) throw new Error(result.errorMsg || '动态码设置失败')
      if (disposed) return
      if (action === 'begin') {
        enrollment.value = result.data
        // 二维码在浏览器本地生成，密钥不会发送给第三方服务。
        qrCode.value = await QRCode.toDataURL(result.data.uri, { width: 240, margin: 2 })
      } else {
        clearEnrollment()
        await load()
        if (action !== 'cancel') ElMessage.success(action === 'confirm' ? '手机动态码已启用' : '手机动态码已关闭')
      }
    } catch (error) {
      if (!disposed && !isRecentAuthenticationCancelled(error)) {
        errorMessage.value = error?.message || '操作失败，请刷新状态后重试'
      }
    } finally {
      busy.value = false
    }
  }

  const refresh = async () => {
    busy.value = true
    errorMessage.value = ''
    try { await load() } catch (error) { errorMessage.value = error.message } finally { busy.value = false }
  }

  onMounted(refresh)
  onBeforeUnmount(() => {
    disposed = true
    clearEnrollment()
  })
  return { enabled, ready, busy, enrollment, qrCode, code, errorMessage, perform, refresh }
}
