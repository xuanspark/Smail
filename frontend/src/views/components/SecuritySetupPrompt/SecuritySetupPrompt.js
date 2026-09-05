import { computed, ref } from 'vue'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'

export function usePage() {
  const globalStatus = useGlobalStatusStore()
  const saving = ref(false)
  const error = ref('')
  const visible = computed(() => globalStatus.isLogin
    && globalStatus.userInfos.showSecuritySetupPrompt === true
    && !globalStatus.userInfos.must_change_password)
  const isSuperAdmin = computed(() => globalStatus.role === 'super_admin')
  const options = computed(() => [
    { key: 'question', title: '密保问题', description: '设置只有自己知道的答案，避免使用容易猜到的个人信息。' },
    { key: 'external-email', title: '外部安全邮箱', description: '绑定可正常接收验证码的外部邮箱，并完成验证。' },
    {
      key: 'webauthn', title: '安全密钥',
      description: globalStatus.userInfos.webAuthnEnabled === true
        ? '使用支持 WebAuthn 的设备或浏览器注册安全密钥。'
        : '服务器尚未启用，需由部署管理员配置 WebAuthn 后才能注册。'
    }
  ])

  const acknowledge = async section => {
    if (saving.value) return
    saving.value = true
    error.value = ''
    const account = globalStatus.userInfos.account
    try {
      const result = await http.post('/api/v1/security/setup/acknowledge', {})
      if (result.errorNo !== 0) throw new Error(result.errorMsg || '保存引导状态失败，请重试')
      if (globalStatus.userInfos.account !== account) return
      globalStatus.userInfos.showSecuritySetupPrompt = false
      globalStatus.dismissRecoveryPrompt()
      if (section) {
        globalStatus.securityActiveSection = section
        globalStatus.settingsActiveTab = 'security'
        globalStatus.settingsDrawerVisible = true
      }
    } catch (failure) {
      error.value = failure?.message || '保存引导状态失败，请重试'
    } finally {
      saving.value = false
    }
  }

  return { visible, isSuperAdmin, options, saving, error, acknowledge }
}
