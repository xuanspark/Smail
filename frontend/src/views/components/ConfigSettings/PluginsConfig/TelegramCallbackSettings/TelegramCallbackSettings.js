import { computed, inject, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'

const endpoint = '/api/v1/plugin/settings/telegram_push/callback'

export function useTelegramCallbackSettings(emit) {
  const page = inject('smail-config-page')
  const store = useGlobalStatusStore()
  const working = ref('')
  const ready = ref(false)
  const checked = ref(false)
  const message = ref('')
  const errorMessage = ref('')
  let disposed = false
  let generation = 0
  const unavailable = computed(() => store.role !== 'super_admin' || page.loading.value ||
    page.sectionLoading.value || page.busy.value || page.hasChanges.value ||
    !!page.pendingCandidate.value || !!page.sectionError.value || !!page.availabilityError.value ||
    !page.canWrite.value || !page.pluginSecretStatus.value?.configured ||
    page.formConfig.value.plugins?.telegram_push?.enabled !== true)

  // 配置或令牌发生变化后，旧的回调检查结果不能继续显示为已就绪。
  watch(() => [page.status.activeGeneration, page.pluginSecretStatus.value?.version, unavailable.value], () => {
    generation++
    checked.value = false
    ready.value = false
    message.value = ''
  })
  const operate = async action => {
    if (disposed || unavailable.value || working.value) return
    working.value = action
    emit('working', true)
    errorMessage.value = ''
    const current = generation
    try {
      if (action === 'configure') {
        try {
          await ElMessageBox.confirm('将使用已保存的系统机器人配置接收绑定请求，不会自动绑定任何账号，也不会覆盖其他应用的回调。', '启用授权回调', {
            type: 'warning', confirmButtonText: '启用或更新', cancelButtonText: '取消', closeOnClickModal: false
          })
        } catch { return }
      }
      const request = () => {
        if (disposed || unavailable.value || current !== generation) throw new Error('系统配置已变更，请刷新后重试')
        return action === 'configure' ? http.post(endpoint) : http.get(endpoint)
      }
      const response = action === 'configure' ? await requestWithRecentAuthentication(request) : await request()
      if (disposed || current !== generation) return
      if (response?.errorNo !== 0 || typeof response.data?.ready !== 'boolean') {
        throw new Error(response?.errorMsg || '读取授权回调状态失败')
      }
      checked.value = true
      ready.value = response.data.ready
      message.value = ready.value ? '回调已配置，各账号可前往“账号通知”绑定本人 Telegram。' : response.data.message || '尚未启用授权回调'
    } catch (error) {
      if (!disposed && !isRecentAuthenticationCancelled(error)) {
        checked.value = false
        ready.value = false
        errorMessage.value = error.message || '授权回调操作失败'
      }
    } finally {
      working.value = ''
      emit('working', false)
    }
  }
  const preventUnload = event => {
    if (!working.value) return
    event.preventDefault()
    event.returnValue = ''
  }
  window.addEventListener('beforeunload', preventUnload)
  onBeforeUnmount(() => {
    disposed = true
    generation++
    window.removeEventListener('beforeunload', preventUnload)
  })
  return { working, ready, checked, message, errorMessage, unavailable, operate }
}
