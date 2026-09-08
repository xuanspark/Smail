import { computed, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'

const endpoint = '/api/v1/plugin/settings/telegram_push'
const defaults = {
  chatId: '',
  showContent: false,
  spoilerContent: false,
  sendAttachments: false,
  disableLinkPreview: true
}

export function useTelegramSettings() {
  const settings = reactive({ ...defaults })
  const version = ref(0)
  const ready = ref(false)
  const working = ref('')
  const errorMessage = ref('')
  const botUsername = ref('')
  const savedSettings = ref('')
  const hasChanges = computed(() => ready.value && JSON.stringify(settings) !== savedSettings.value)
  let disposed = false
  const botLink = computed(() => /^[a-zA-Z0-9_]{5,32}$/.test(botUsername.value)
    ? `https://t.me/${botUsername.value}` : '')

  const checkResponse = (response, fallback) => {
    if (!response || response.errorNo !== 0) throw new Error(response?.errorMsg || fallback)
    return response.data
  }
  const acceptSettings = data => {
    if (!data?.settings || !Number.isSafeInteger(data.version) || data.version < 0) {
      throw new Error('插件设置响应不完整，请重新加载')
    }
    for (const key of Object.keys(defaults)) {
      settings[key] = key === 'chatId' ? String(data.settings[key] || '') : data.settings[key] === true
    }
    version.value = data.version
    savedSettings.value = JSON.stringify(settings)
    ready.value = true
  }
  const beforeLeave = async () => {
    if (working.value) return false
    if (!hasChanges.value) return true
    try {
      await ElMessageBox.confirm('当前通知设置尚未保存，确定离开吗？', '未保存的更改', {
        type: 'warning', confirmButtonText: '离开', cancelButtonText: '继续编辑'
      })
      return true
    } catch {
      return false
    }
  }
  const load = async () => {
    if (working.value || disposed) return
    working.value = 'load'
    ready.value = false
    errorMessage.value = ''
    try {
      const response = await http.get(`${endpoint}/user`)
      if (disposed) return
      acceptSettings(checkResponse(response, '读取 Telegram 设置失败'))
      try {
        const info = checkResponse(await http.get(`${endpoint}/bot`), '读取机器人信息失败')
        if (!disposed) botUsername.value = info?.username || ''
      } catch {
        // 机器人不可用不应阻止用户停用通知或调整本人设置。
        botUsername.value = ''
      }
    } catch (error) {
      if (!disposed) errorMessage.value = error.message || '读取 Telegram 设置失败'
    } finally {
      working.value = ''
    }
  }
  const submit = async action => {
    if (!ready.value || working.value || disposed) return
    const chatId = settings.chatId.trim()
    if ((chatId && !/^-?[1-9][0-9]{0,18}$/.test(chatId)) || (action === 'test' && !chatId)) {
      errorMessage.value = '请输入有效的 Telegram 聊天 ID；留空可保存为停用状态'
      return
    }
    working.value = action
    errorMessage.value = ''
    // 仅发送本人可维护的白名单字段，不接收插件 HTML 提供的路由或身份。
    const payload = { settings: { ...settings, chatId }, version: version.value }
    try {
      const response = action === 'test'
        ? await http.post(`${endpoint}/test`, payload)
        : await http.put(`${endpoint}/user`, payload)
      if (disposed) return
      const data = checkResponse(response, action === 'test' ? '发送测试消息失败' : '保存 Telegram 设置失败')
      if (action !== 'test') acceptSettings(data)
      ElMessage.success(action === 'test' ? '测试消息已发送' : 'Telegram 设置已保存')
    } catch (error) {
      if (!disposed) errorMessage.value = error.message || 'Telegram 操作失败'
    } finally {
      working.value = ''
    }
  }
  const refresh = async () => {
    if (await beforeLeave()) await load()
  }
  const preventUnload = event => {
    if (!hasChanges.value && !working.value) return
    event.preventDefault()
    event.returnValue = ''
  }

  onMounted(() => {
    if (typeof window !== 'undefined') window.addEventListener('beforeunload', preventUnload)
    load()
  })
  onBeforeUnmount(() => {
    disposed = true
    if (typeof window !== 'undefined') window.removeEventListener('beforeunload', preventUnload)
  })
  return { settings, version, ready, working, errorMessage, botLink, load, refresh, submit, beforeLeave, hasChanges }
}
