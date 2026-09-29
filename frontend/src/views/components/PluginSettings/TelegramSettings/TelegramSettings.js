import { computed, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import QRCode from 'qrcode'
import { http } from '@/utils/axios'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'

const endpoint = '/api/v1/plugin/settings/telegram_push'
const defaults = {
  enabled: false,
  showContent: false,
  spoilerContent: false,
  sendAttachments: false,
  disableLinkPreview: true
}

export function useTelegramSettings() {
  const settings = reactive({ ...defaults })
  const status = reactive({ bound: false, displayName: '', needsRebind: false, available: false, message: '' })
  const binding = reactive({ ticket: '', phase: '', displayName: '', expiresAt: 0, url: '', image: '' })
  const version = ref(0)
  const ready = ref(false)
  const working = ref('')
  const errorMessage = ref('')
  const bindingError = ref('')
  const dialogVisible = ref(false)
  const polling = ref(false)
  const savedSettings = ref('')
  const hasChanges = computed(() => ready.value && JSON.stringify(settings) !== savedSettings.value)
  let disposed = false
  let generation = 0
  let pollTimer
  let expiryTimer
  const bindingLabel = computed(() => status.needsRebind ? '需要重新绑定' : status.bound ? '已绑定' : '未绑定')

  const checkResponse = (response, fallback) => {
    if (!response || response.errorNo !== 0) throw new Error(response?.errorMsg || fallback)
    return response.data
  }
  const acceptSettings = data => {
    if (!data || typeof data.bound !== 'boolean' || !Number.isSafeInteger(data.version) || data.version < 0) {
      throw new Error('插件设置响应不完整，请重新加载')
    }
    for (const key of Object.keys(defaults)) {
      settings[key] = data[key] === true
    }
    status.bound = data.bound
    status.displayName = typeof data.displayName === 'string' ? data.displayName : ''
    status.needsRebind = data.needsRebind === true
    if (typeof data.available === 'boolean') {
      status.available = data.available
      status.message = typeof data.message === 'string' ? data.message : ''
    }
    version.value = data.version
    savedSettings.value = JSON.stringify(settings)
    ready.value = true
  }
  const confirmDiscard = async () => {
    if (!hasChanges.value) return true
    try {
      await ElMessageBox.confirm('当前通知设置尚未保存，确定放弃这些更改吗？', '未保存的更改', {
        type: 'warning', confirmButtonText: '放弃更改', cancelButtonText: '继续编辑'
      })
      return !disposed
    } catch {
      return false
    }
  }
  const load = async () => {
    if (working.value || disposed || dialogVisible.value) return
    working.value = 'load'
    ready.value = false
    errorMessage.value = ''
    try {
      const response = await http.get(`${endpoint}/user`)
      if (disposed) return
      acceptSettings(checkResponse(response, '读取 Telegram 设置失败'))
    } catch (error) {
      if (!disposed) errorMessage.value = error.message || '读取 Telegram 设置失败'
    } finally {
      working.value = ''
    }
  }
  const reportSync = data => {
    if (data.synced === false) {
      errorMessage.value = '设置已保存，但运行中的推送服务尚未同步，请联系超级管理员。'
    } else {
      ElMessage.success('Telegram 通知设置已保存')
    }
  }
  const recentRequest = request => requestWithRecentAuthentication(() => {
    if (disposed) throw new Error('当前页面已关闭')
    return request()
  })
  const bindingRequest = (action, ticket = binding.ticket) => http.post(`${endpoint}/binding`, { action, ticket })
  const stopTimers = () => {
    clearTimeout(pollTimer)
    clearTimeout(expiryTimer)
    polling.value = false
  }
  const clearBinding = () => {
    generation++
    stopTimers()
    dialogVisible.value = false
    Object.assign(binding, { ticket: '', phase: '', displayName: '', expiresAt: 0, url: '', image: '' })
    bindingError.value = ''
  }
  const expire = () => {
    generation++
    stopTimers()
    binding.phase = 'expired'
    binding.url = ''
    binding.image = ''
    bindingError.value = '授权链接已过期，请关闭后重新申请。'
  }
  const poll = async () => {
    if (disposed || polling.value || !dialogVisible.value || !binding.ticket || binding.phase === 'expired') return
    if (Date.now() >= binding.expiresAt) return expire()
    clearTimeout(pollTimer)
    const current = generation
    polling.value = true
    bindingError.value = ''
    try {
      const data = checkResponse(await bindingRequest('status'), '读取授权状态失败')
      if (disposed || current !== generation) return
      if (!['waiting', 'claimed'].includes(data?.phase)) throw new Error('授权状态异常，请重新绑定')
      binding.phase = data.phase
      binding.displayName = typeof data.displayName === 'string' ? data.displayName : ''
      if (data.phase === 'claimed') {
        binding.url = ''
        binding.image = ''
      } else {
        pollTimer = setTimeout(poll, 2000)
      }
    } catch (error) {
      if (!disposed && current === generation) bindingError.value = error.message || '读取授权状态失败，请重试'
    } finally {
      if (current === generation) polling.value = false
    }
  }
  const closeBinding = async () => {
    if (working.value) return false
    working.value = 'cancel'
    generation++
    stopTimers()
    try {
      if (binding.ticket) checkResponse(await bindingRequest('cancel'), '取消绑定失败')
      clearBinding()
      return true
    } catch (error) {
      if (!disposed) bindingError.value = error.message || '取消绑定失败，请重试'
      return false
    } finally {
      working.value = ''
    }
  }
  const beginBinding = async () => {
    if (!ready.value || !status.available || working.value || disposed || dialogVisible.value) return
    working.value = 'begin'
    errorMessage.value = ''
    let ticket = ''
    try {
      if (!await confirmDiscard()) return
      const data = checkResponse(await recentRequest(() => bindingRequest('begin', '')), '创建授权请求失败')
      ticket = typeof data?.ticket === 'string' ? data.ticket : ''
      if (!/^[A-Za-z0-9_-]{43}$/.test(ticket)) throw new Error('绑定凭据无效，请重新申请')
      if (disposed) return
      const url = new URL(data.url)
      const expiresAt = Date.parse(data.expiresAt)
      if (url.origin !== 'https://t.me' || !/^\/[A-Za-z0-9_]{5,32}$/.test(url.pathname) ||
          url.username || url.password || url.hash || [...url.searchParams].length !== 1 ||
          url.searchParams.get('start') !== `smail_${ticket}` || !Number.isFinite(expiresAt) ||
          expiresAt <= Date.now() || expiresAt > Date.now() + 310000) {
        throw new Error('Telegram 授权地址或有效期异常，请重新申请')
      }
      const image = await QRCode.toDataURL(url.href, { width: 240, margin: 2 })
      if (disposed) return
      clearBinding()
      Object.assign(binding, { ticket, phase: 'waiting', displayName: '', expiresAt, url: url.href, image })
      dialogVisible.value = true
      ticket = ''
      expiryTimer = setTimeout(expire, Math.max(0, expiresAt - Date.now()))
      await poll()
    } catch (error) {
      if (!disposed && !isRecentAuthenticationCancelled(error)) errorMessage.value = error.message || '创建授权请求失败'
    } finally {
      if (/^[A-Za-z0-9_-]{43}$/.test(ticket)) {
        // 页面关闭或二维码生成失败时撤销请求，失败时仍由服务端有效期兜底。
        try { await bindingRequest('cancel', ticket) } catch { /* 不再展示已失效的授权地址。 */ }
      }
      working.value = ''
    }
  }
  const confirmBinding = async () => {
    if (disposed || working.value || binding.phase !== 'claimed') return
    if (Date.now() >= binding.expiresAt) return expire()
    working.value = 'confirm'
    bindingError.value = ''
    const ticket = binding.ticket
    try {
      const data = checkResponse(await recentRequest(() => {
        if (Date.now() >= binding.expiresAt) throw new Error('授权已过期，请重新绑定')
        return bindingRequest('confirm', ticket)
      }), '确认绑定失败')
      if (disposed) return
      acceptSettings(data)
      clearBinding()
      reportSync(data)
    } catch (error) {
      if (!disposed && !isRecentAuthenticationCancelled(error)) bindingError.value = error.message || '确认绑定失败'
    } finally {
      working.value = ''
    }
  }
  const submit = async action => {
    if (!ready.value || working.value || disposed || dialogVisible.value) return
    working.value = action
    errorMessage.value = ''
    try {
      if (action === 'unbind') {
        try {
          await ElMessageBox.confirm('解除后将停止向此 Telegram 账号发送通知，确定继续吗？', '解除本人绑定', {
            type: 'warning', confirmButtonText: '解除绑定', cancelButtonText: '取消'
          })
        } catch { return }
      }
      // 页面不提交接收身份，只能修改本人偏好或使用已确认的绑定。
      const payload = { ...settings, version: version.value }
      const response = await recentRequest(() => action === 'test'
        ? http.post(`${endpoint}/test`)
        : action === 'unbind'
          ? http.delete(`${endpoint}/user`, { data: payload })
          : http.put(`${endpoint}/user`, payload))
      if (disposed) return
      const data = checkResponse(response, action === 'test' ? '发送测试消息失败' : '保存 Telegram 设置失败')
      if (action === 'test') ElMessage.success('测试请求已提交，请到 Telegram 核对')
      else {
        acceptSettings(data)
        reportSync(data)
      }
    } catch (error) {
      if (!disposed && !isRecentAuthenticationCancelled(error)) errorMessage.value = error.message || 'Telegram 操作失败'
    } finally {
      working.value = ''
    }
  }
  const beforeLeave = async () => {
    if (working.value || !await confirmDiscard()) return false
    if (!dialogVisible.value) return true
    try {
      await ElMessageBox.confirm('当前授权尚未完成，确定取消并离开吗？', '取消绑定', {
        type: 'warning', confirmButtonText: '取消并离开', cancelButtonText: '继续绑定'
      })
    } catch { return false }
    return closeBinding()
  }
  const refresh = async () => {
    if (!working.value && await confirmDiscard()) await load()
  }
  const preventUnload = event => {
    if (!hasChanges.value && !working.value && !dialogVisible.value) return
    event.preventDefault()
    event.returnValue = ''
  }

  onMounted(() => {
    if (typeof window !== 'undefined') window.addEventListener('beforeunload', preventUnload)
    load()
  })
  onBeforeUnmount(() => {
    disposed = true
    const ticket = binding.ticket
    clearBinding()
    if (typeof window !== 'undefined') window.removeEventListener('beforeunload', preventUnload)
    if (ticket) bindingRequest('cancel', ticket).catch(() => {})
  })
  return {
    settings, status, binding, ready, working, errorMessage, bindingError,
    dialogVisible, polling, hasChanges, bindingLabel, refresh, submit,
    beginBinding, confirmBinding, closeBinding, poll, beforeLeave
  }
}
