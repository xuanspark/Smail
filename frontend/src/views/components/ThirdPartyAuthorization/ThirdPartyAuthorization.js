import { onMounted, reactive, ref } from 'vue'

import { ElMessageBox, ElNotification } from 'element-plus'

import { http } from '@/utils/axios'

export function usePage() {
  const protocolLoading = ref(false)
  const protocolCredentials = ref([])
  const generatedProtocolCode = ref('')
  const generatedProtocol = ref('')

  // ??????????{ imap: 'xxxx', pop3: 'yyyy', smtp: 'zzzz' }
  const revealedCodes = reactive({})
  // ???????????????
  const codesRevealed = ref(false)

  // ????????
  const verifyDialogVisible = ref(false)
  const verifyPassword = ref('')
  const verifyLoading = ref(false)

  const notify = (message, type = 'error') => {
    ElNotification({
      title: type === 'success' ? '????' : '????',
      message,
      type,
    })
  }

  const protocolLabel = protocol => ({
    imap: 'IMAP',
    pop3: 'POP3',
    smtp: 'SMTP',
  }[protocol] || protocol.toUpperCase())

  const loadProtocolCredentials = async () => {
    protocolLoading.value = true
    try {
      const response = await http.get('/api/v1/security/credentials/protocols')
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '?????????????')
      }
      protocolCredentials.value = Array.isArray(response.data) ? response.data : []
    } catch (error) {
      notify(error?.message || '?????????????')
    } finally {
      protocolLoading.value = false
    }
  }

  const setProtocolEnabled = async (item, enabled) => {
    if (protocolLoading.value || item.enabled === enabled) return
    const previous = item.enabled
    item.enabled = enabled
    protocolLoading.value = true
    try {
      const response = await http.put('/api/v1/security/credentials/protocols/enabled', {
        protocol: item.protocol,
        enabled,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '??????')
      }
      if (!enabled) {
        if (generatedProtocol.value === item.protocol) {
          generatedProtocol.value = ''
          generatedProtocolCode.value = ''
        }
        delete revealedCodes[item.protocol]
      }
      notify(`${protocolLabel(item.protocol)} ??????????${enabled ? '??' : '??'}`, 'success')
    } catch (error) {
      item.enabled = previous
      notify(error?.message || '??????')
    } finally {
      protocolLoading.value = false
    }
  }

  const generateProtocolCode = async item => {
    if (!item.enabled) {
      notify(`???? ${protocolLabel(item.protocol)} ??`)
      return
    }

    protocolLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/protocols/generate', {
        protocol: item.protocol,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '???????')
      }
      const newCode = response.data?.code || ''
      generatedProtocol.value = item.protocol
      generatedProtocolCode.value = newCode
      item.configured = true
      // ??????????
      revealedCodes[item.protocol] = newCode
      codesRevealed.value = true
      notify(`${protocolLabel(item.protocol)} ???????????`, 'success')
    } catch (error) {
      notify(error?.message || '???????')
    } finally {
      protocolLoading.value = false
    }
  }

  const copyProtocolCode = async (code) => {
    const targetCode = typeof code === 'string' && code ? code : generatedProtocolCode.value
    if (!targetCode) return
    try {
      await navigator.clipboard.writeText(targetCode)
      notify('??????????', 'success')
    } catch {
      notify('?????????????')
    }
  }

  const revokeProtocolCode = async item => {
    try {
      await ElMessageBox.confirm(
        `??? ${protocolLabel(item.protocol)} ??????????????`,
        '???????',
        { type: 'warning' },
      )
      protocolLoading.value = true
      const response = await http.post('/api/v1/security/credentials/protocols/revoke', {
        protocol: item.protocol,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '???????')
      }
      item.configured = false
      if (generatedProtocol.value === item.protocol) {
        generatedProtocol.value = ''
        generatedProtocolCode.value = ''
      }
      delete revealedCodes[item.protocol]
      notify(`${protocolLabel(item.protocol)} ??????`, 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') {
        notify(error?.message || '???????')
      }
    } finally {
      protocolLoading.value = false
    }
  }

  // ????????
  const openVerifyDialog = () => {
    verifyPassword.value = ''
    verifyDialogVisible.value = true
  }

  // ????????
  const closeVerifyDialog = () => {
    verifyPassword.value = ''
    verifyDialogVisible.value = false
  }

  // ????????????????
  const submitVerifyAndReveal = async () => {
    if (!verifyPassword.value.trim()) {
      notify('???????????????')
      return
    }

    verifyLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/protocols/reveal', {
        password: verifyPassword.value,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '????????')
      }
      const data = response.data || {}
      Object.keys(revealedCodes).forEach(k => delete revealedCodes[k])
      Object.assign(revealedCodes, data)
      codesRevealed.value = true
      closeVerifyDialog()
      notify('???????????????', 'success')
    } catch (error) {
      notify(error?.message || '????????')
    } finally {
      verifyLoading.value = false
    }
  }

  // ???????????
  const hideRevealedCodes = () => {
    codesRevealed.value = false
    Object.keys(revealedCodes).forEach(k => delete revealedCodes[k])
    generatedProtocolCode.value = ''
    generatedProtocol.value = ''
    notify('????????', 'success')
  }

  onMounted(loadProtocolCredentials)

  return {
    protocolLoading,
    protocolCredentials,
    generatedProtocolCode,
    generatedProtocol,
    revealedCodes,
    codesRevealed,
    verifyDialogVisible,
    verifyPassword,
    verifyLoading,
    protocolLabel,
    setProtocolEnabled,
    generateProtocolCode,
    copyProtocolCode,
    revokeProtocolCode,
    openVerifyDialog,
    closeVerifyDialog,
    submitVerifyAndReveal,
    hideRevealedCodes,
  }
}
