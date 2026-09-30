import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'

import { ElMessageBox, ElNotification } from 'element-plus'

import { http } from '@/utils/axios'

export function usePage() {
  const loading = ref(false)
  const clientList = ref([])
  const selectedClientId = ref(null)
  const currentDomain = ref('')

  // 解密后的各客户端授权码明文 { [clientId]: 'xxxx-yyyy-...' }
  const revealedCodes = reactive({})
  const codesRevealed = ref(false)
  const revealCountdown = ref(0)
  let countdownTimer = null

  // 新增客户端弹窗
  const createDialogVisible = ref(false)
  const newClientName = ref('')
  const createLoading = ref(false)

  // 密码验证查看弹窗
  const verifyDialogVisible = ref(false)
  const verifyPassword = ref('')
  const verifyLoading = ref(false)

  const currentClient = computed(() => {
    if (!selectedClientId.value || !Array.isArray(clientList.value)) return null
    return clientList.value.find(c => c.id === selectedClientId.value) || null
  })

  const notify = (message, type = 'error') => {
    ElNotification({
      title: type === 'success' ? '操作成功' : (type === 'info' ? '提示' : '操作失败'),
      message,
      type,
    })
  }

  const clearCountdownTimer = () => {
    if (countdownTimer) {
      clearInterval(countdownTimer)
      countdownTimer = null
    }
  }

  const hideCodesAutomatically = (silent = false) => {
    clearCountdownTimer()
    revealCountdown.value = 0
    Object.keys(revealedCodes).forEach(k => delete revealedCodes[k])
    codesRevealed.value = false
    if (!silent) {
      notify('授权码明文已达到 60 秒安全保护期，已自动隐藏', 'info')
    }
  }

  const startRevealCountdown = (seconds = 60) => {
    clearCountdownTimer()
    revealCountdown.value = seconds
    codesRevealed.value = true

    countdownTimer = setInterval(() => {
      revealCountdown.value -= 1
      if (revealCountdown.value <= 0) {
        hideCodesAutomatically(false)
      }
    }, 1000)
  }

  const formatTime = (timeStr) => {
    if (!timeStr) return '--'
    try {
      const d = new Date(timeStr)
      if (isNaN(d.getTime())) return timeStr
      return d.toLocaleDateString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return timeStr
    }
  }

  const loadBaseInfo = async () => {
    try {
      const infoRes = await http.post('/api/v1/user/info', {})
      const email = infoRes?.data?.email
      const domains = Array.isArray(infoRes?.data?.domains) ? infoRes.data.domains : []
      if (email && email.includes('@')) {
        currentDomain.value = email.split('@')[1]
      } else if (domains.length > 0 && domains[0]) {
        currentDomain.value = domains[0]
      }
    } catch {
      // 忽略
    }
  }

  const loadClientCredentials = async () => {
    loading.value = true
    try {
      const response = await http.get('/api/v1/security/credentials/clients')
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '读取客户端授权码列表失败')
      }
      const list = Array.isArray(response.data) ? response.data : []
      clientList.value = list
      if (list.length > 0) {
        if (!selectedClientId.value || !list.some(c => c.id === selectedClientId.value)) {
          selectedClientId.value = list[0].id
        }
      } else {
        selectedClientId.value = null
      }
    } catch (error) {
      notify(error?.message || '读取客户端授权码列表失败')
    } finally {
      loading.value = false
    }
  }

  const handleClientSelect = (val) => {
    selectedClientId.value = val
  }

  const openCreateDialog = () => {
    newClientName.value = ''
    createDialogVisible.value = true
  }

  const closeCreateDialog = () => {
    createDialogVisible.value = false
    newClientName.value = ''
  }

  const submitCreateClient = async () => {
    const name = newClientName.value.trim()
    if (!name) {
      notify('请输入客户端名称')
      return
    }
    createLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/clients/create', {
        client_name: name,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '创建客户端专属授权码失败')
      }
      const code = response.data?.code || ''
      const item = response.data?.client
      closeCreateDialog()
      await loadClientCredentials()

      if (item && item.id) {
        selectedClientId.value = item.id
        revealedCodes[item.id] = code
        startRevealCountdown(60)
      }
      notify(`客户端「${name}」专属授权码已创建并就绪（60秒内有效显示）`, 'success')
    } catch (error) {
      notify(error?.message || '创建客户端授权码失败')
    } finally {
      createLoading.value = false
    }
  }

  const regenerateClientCode = async (client) => {
    if (!client) return
    try {
      await ElMessageBox.confirm(
        `确定重新生成客户端「${client.client_name}」的专属授权码吗？旧授权码将立即失效。`,
        '安全提示',
        {
          confirmButtonText: '确定重新生成',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )
    } catch {
      return
    }

    loading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/clients/regenerate', {
        client_id: client.id,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '重新生成客户端授权码失败')
      }
      const newCode = response.data?.code || ''
      revealedCodes[client.id] = newCode
      startRevealCountdown(60)
      notify(`客户端「${client.client_name}」专属授权码已重新生成（60秒内有效显示）`, 'success')
      await loadClientCredentials()
    } catch (error) {
      notify(error?.message || '重新生成失败')
    } finally {
      loading.value = false
    }
  }

  const revokeClientCode = async (client) => {
    if (!client) return
    try {
      await ElMessageBox.confirm(
        `确定撤销并删除客户端「${client.client_name}」吗？撤销后该设备将无法通过授权码登录收发信。`,
        '安全提示',
        {
          confirmButtonText: '确定撤销删除',
          cancelButtonText: '取消',
          type: 'danger',
        }
      )
    } catch {
      return
    }

    loading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/clients/revoke', {
        client_id: client.id,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '撤销客户端失败')
      }
      delete revealedCodes[client.id]
      notify(`客户端「${client.client_name}」已成功撤销`, 'success')
      await loadClientCredentials()
    } catch (error) {
      notify(error?.message || '撤销失败')
    } finally {
      loading.value = false
    }
  }

  const copyText = async (text, label = '内容') => {
    if (!text) return
    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        const input = document.createElement('input')
        input.value = text
        document.body.appendChild(input)
        input.select()
        document.execCommand('copy')
        document.body.removeChild(input)
      }
      notify(`${label}已复制到剪贴板`, 'success')
    } catch {
      notify('复制失败，请手动选择复制')
    }
  }

  const copyCode = async (codeText) => {
    if (!codeText) {
      notify('未找到可复制的授权码')
      return
    }
    await copyText(codeText, '专属授权码')
  }

  const openVerifyDialog = () => {
    verifyPassword.value = ''
    verifyDialogVisible.value = true
  }

  const closeVerifyDialog = () => {
    verifyDialogVisible.value = false
    verifyPassword.value = ''
  }

  const submitVerifyAndReveal = async () => {
    const password = verifyPassword.value.trim()
    if (!password) {
      notify('请输入当前账号登录密码')
      return
    }
    verifyLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/clients/reveal', {
        password,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '密码验证失败，无法查看授权码')
      }
      const map = response.data || {}
      Object.keys(revealedCodes).forEach(k => delete revealedCodes[k])
      Object.assign(revealedCodes, map)
      closeVerifyDialog()
      const count = Object.keys(map).length
      if (count > 0) {
        startRevealCountdown(60)
        notify('安全验证通过，授权码将在 60 秒内明文显示', 'success')
      } else {
        notify('安全验证通过，当前未发现加密密文，可点击重新生成', 'warning')
      }
    } catch (error) {
      notify(error?.message || '验证失败，请检查密码是否正确')
    } finally {
      verifyLoading.value = false
    }
  }

  onMounted(() => {
    loadBaseInfo()
    loadClientCredentials()
  })

  onUnmounted(() => {
    clearCountdownTimer()
  })

  return {
    loading,
    clientList,
    selectedClientId,
    currentClient,
    currentDomain,
    revealedCodes,
    codesRevealed,
    revealCountdown,
    createDialogVisible,
    newClientName,
    createLoading,
    verifyDialogVisible,
    verifyPassword,
    verifyLoading,
    handleClientSelect,
    openCreateDialog,
    closeCreateDialog,
    submitCreateClient,
    regenerateClientCode,
    revokeClientCode,
    copyText,
    copyCode,
    formatTime,
    openVerifyDialog,
    closeVerifyDialog,
    submitVerifyAndReveal,
  }
}
