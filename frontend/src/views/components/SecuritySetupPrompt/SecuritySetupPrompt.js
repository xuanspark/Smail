import { computed, ref } from 'vue'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'

export function usePage() {
  const globalStatus = useGlobalStatusStore()
  const saving = ref(false)
  const error = ref('')
  const required = computed(() => globalStatus.userInfos.securitySetupRequired === true)
  const visible = computed(() => globalStatus.isLogin
    && globalStatus.userInfos.showSecuritySetupPrompt === true
    && !(required.value && globalStatus.settingsDrawerVisible)
    && !globalStatus.userInfos.must_change_password)
  const isSuperAdmin = computed(() => globalStatus.role === 'super_admin')
  const options = computed(() => {
    const items = []
    if (!globalStatus.userInfos.recoveryCodesConfigured) {
      items.push({ key: 'recovery', title: '安全码', description: '生成安全码并妥善保存。' })
    }
    if (!globalStatus.userInfos.externalEmailConfigured) {
      items.push({ key: 'external-email', title: '外部邮箱', description: '绑定邮箱并完成验证。' })
    }
    if (isSuperAdmin.value && !globalStatus.userInfos.totpConfigured) {
      items.push({ key: 'totp', title: '手机动态码', description: '使用手机验证器完成绑定。' })
    }
    return items
  })

  const acknowledge = async section => {
    if (saving.value) return
    if (required.value) {
      if (!section) return
      globalStatus.securityActiveSection = section
      globalStatus.settingsActiveTab = 'security'
      globalStatus.settingsDrawerVisible = true
      return
    }
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

  const logout = async () => {
    if (saving.value) return
    saving.value = true
    error.value = ''
    try {
      const result = await http.post('/api/v1/logout', {})
      if (result.errorNo !== 0) throw new Error(result.errorMsg || '注销失败，请重试')
      location.reload()
    } catch (failure) {
      error.value = failure?.message || '注销失败，请重试'
      saving.value = false
    }
  }

  return { visible, isSuperAdmin, required, options, saving, error, acknowledge, logout }
}
