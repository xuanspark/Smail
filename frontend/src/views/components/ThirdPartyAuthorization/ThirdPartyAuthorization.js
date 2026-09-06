import { onMounted, ref } from 'vue'

import { ElMessageBox, ElNotification } from 'element-plus'

import { http } from '@/utils/axios'

export function usePage() {
  const protocolLoading = ref(false)
  const protocolCredentials = ref([])
  const generatedProtocolCode = ref('')
  const generatedProtocol = ref('')

  const notify = (message, type = 'error') => {
    ElNotification({
      title: type === 'success' ? '操作成功' : '操作失败',
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
        throw new Error(response.errorMsg || '读取第三方授权设置失败')
      }
      protocolCredentials.value = Array.isArray(response.data) ? response.data : []
    } catch (error) {
      notify(error?.message || '读取第三方授权设置失败')
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
        throw new Error(response.errorMsg || '协议设置失败')
      }
      notify(`${protocolLabel(item.protocol)} 协议已${enabled ? '开启' : '关闭'}`, 'success')
    } catch (error) {
      item.enabled = previous
      notify(error?.message || '协议设置失败')
    } finally {
      protocolLoading.value = false
    }
  }

  const generateProtocolCode = async item => {
    if (!item.enabled) {
      notify(`请先开启 ${protocolLabel(item.protocol)} 协议`)
      return
    }

    protocolLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/protocols/generate', {
        protocol: item.protocol,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '生成授权码失败')
      }
      generatedProtocol.value = item.protocol
      generatedProtocolCode.value = response.data?.code || ''
      item.configured = true
      notify(`${protocolLabel(item.protocol)} 授权码已生成，请立即保存`, 'success')
    } catch (error) {
      notify(error?.message || '生成授权码失败')
    } finally {
      protocolLoading.value = false
    }
  }

  const copyProtocolCode = async () => {
    if (!generatedProtocolCode.value) return
    try {
      await navigator.clipboard.writeText(generatedProtocolCode.value)
      notify('授权码已复制', 'success')
    } catch {
      notify('复制失败，请手动复制授权码')
    }
  }

  const revokeProtocolCode = async item => {
    try {
      await ElMessageBox.confirm(
        `撤销后 ${protocolLabel(item.protocol)} 授权码将立即失效，是否继续？`,
        '撤销协议授权码',
        { type: 'warning' },
      )
      protocolLoading.value = true
      const response = await http.post('/api/v1/security/credentials/protocols/revoke', {
        protocol: item.protocol,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '撤销授权码失败')
      }
      item.configured = false
      if (generatedProtocol.value === item.protocol) {
        generatedProtocol.value = ''
        generatedProtocolCode.value = ''
      }
      notify(`${protocolLabel(item.protocol)} 授权码已撤销`, 'success')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') {
        notify(error?.message || '撤销授权码失败')
      }
    } finally {
      protocolLoading.value = false
    }
  }

  onMounted(loadProtocolCredentials)

  return {
    protocolLoading,
    protocolCredentials,
    generatedProtocolCode,
    generatedProtocol,
    protocolLabel,
    setProtocolEnabled,
    generateProtocolCode,
    copyProtocolCode,
    revokeProtocolCode,
  }
}
