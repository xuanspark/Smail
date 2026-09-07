import { ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'

const recentAuthenticationRequired = 'RECENT_AUTH_REQUIRED'
const cancelledCode = 'RECENT_AUTH_CANCELLED'

export function isRecentAuthenticationCancelled(error) {
  return error?.code === cancelledCode
}

export async function requestWithRecentAuthentication(request) {
  try {
    return await request()
  } catch (error) {
    if (error?.apiErrorCode !== recentAuthenticationRequired) throw error
  }

  let password = ''
  try {
    const result = await ElMessageBox.prompt(
      '为保护敏感配置，请确认当前登录密码。确认后将自动完成保存。',
      '确认当前密码',
      {
        inputType: 'password',
        inputPlaceholder: '请输入当前密码',
        confirmButtonText: '确认并保存',
        cancelButtonText: '取消',
        closeOnClickModal: false,
        inputValidator: value => String(value || '').length > 0 || '请输入当前密码'
      }
    )
    password = String(result.value || '')
  } catch (error) {
    const cancelled = new Error('已取消身份确认')
    cancelled.code = cancelledCode
    throw cancelled
  }

  try {
    const response = await http.post('/api/v1/security/reauth', { password })
    if (!response || response.errorNo !== 0) {
      throw new Error(response?.errorMsg || '身份确认失败')
    }
  } finally {
    password = ''
  }

  return request()
}
