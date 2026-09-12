import { onMounted, reactive, ref } from 'vue'

import { ElMessageBox, ElNotification } from 'element-plus'

import { http } from '@/utils/axios'

export function usePage() {
  const protocolLoading = ref(false)
  const protocolCredentials = ref([])
  const generatedProtocolCode = ref('')
  const generatedProtocol = ref('')

  // 解密后的各协议授权码明文 { imap: 'xxxx', pop3: 'yyyy', smtp: 'zzzz' }
  const revealedCodes = reactive({})
  // 是否已完成密码安全验证并展示
  const codesRevealed = ref(false)

  // 密码验证查看弹窗
  const verifyDialogVisible = ref(false)
  const verifyPassword = ref('')
  const verifyLoading = ref(false)

  const notify = (message, type = 'error') => {
    ElNotification({
      title: type === 'success' ? '操作成功' : '操作失败',
      message,
      type,
    })
  }

  const protocolLabel = (protocol = '') => {
    const map = {
      smtp: 'SMTP (代发/发信)',
      imap: 'IMAP (收信/同步)',
      pop3: 'POP3 (收信)',
    }
    return map[protocol.toLowerCase()] || protocol.toUpperCase()
  }

  const loadProtocolCredentials = async () => {
    protocolLoading.value = true
    try {
      const response = await http.get('/api/v1/security/credentials/protocols')
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '读取邮件客户端协议设置失败')
      }
      protocolCredentials.value = Array.isArray(response.data) ? response.data : []
    } catch (error) {
      notify(error?.message || '读取邮件客户端协议设置失败')
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
        throw new Error(response.errorMsg || '更新协议开启状态失败')
      }
      if (!enabled) {
        if (generatedProtocol.value === item.protocol) {
          generatedProtocol.value = ''
          generatedProtocolCode.value = ''
        }
        delete revealedCodes[item.protocol]
      }
      notify(`${protocolLabel(item.protocol)} 当前账号客户端访问已${enabled ? '开启' : '关闭'}`, 'success')
      await loadProtocolCredentials()
    } catch (error) {
      item.enabled = previous
      notify(error?.message || '更新协议开启状态失败')
    } finally {
      protocolLoading.value = false
    }
  }

  const copyProtocolCode = async (codeToCopy) => {
    const targetCode = typeof codeToCopy === 'string' && codeToCopy
      ? codeToCopy
      : generatedProtocolCode.value

    if (!targetCode) {
      notify('未找到可复制的授权码')
      return
    }

    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(targetCode)
      } else {
        const input = document.createElement('input')
        input.value = targetCode
        document.body.appendChild(input)
        input.select()
        document.execCommand('copy')
        document.body.removeChild(input)
      }
      notify('协议授权码已复制到剪贴板', 'success')
    } catch (error) {
      notify('复制失败，请手动选择复制')
    }
  }

  const generateProtocolCode = async (item) => {
    const actionText = item.configured ? '重新生成' : '生成'
    try {
      await ElMessageBox.confirm(
        `确定为 ${protocolLabel(item.protocol)} ${actionText}专属授权码吗？旧授权码将立即失效并更新。`,
        '安全提示',
        {
          confirmButtonText: '确定生成',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )
    } catch {
      return
    }

    protocolLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/protocols/generate', {
        protocol: item.protocol,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '生成协议授权码失败')
      }

      const newCode = response?.data?.code || ''
      generatedProtocol.value = item.protocol
      generatedProtocolCode.value = newCode

      // 自动同步到已解密展示状态，无需再次密码验证
      revealedCodes[item.protocol] = newCode
      codesRevealed.value = true

      notify(`${protocolLabel(item.protocol)} 授权码已生成并持久保存`, 'success')
      await loadProtocolCredentials()
    } catch (error) {
      notify(error?.message || '生成协议授权码失败')
    } finally {
      protocolLoading.value = false
    }
  }

  const revokeProtocolCode = async (item) => {
    try {
      await ElMessageBox.confirm(
        `确定撤销 ${protocolLabel(item.protocol)} 的授权码吗？撤销后客户端将无法使用该授权码登录。`,
        '安全提示',
        {
          confirmButtonText: '确定撤销',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )
    } catch {
      return
    }

    protocolLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/protocols/revoke', {
        protocol: item.protocol,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '撤销协议授权码失败')
      }
      delete revealedCodes[item.protocol]
      if (generatedProtocol.value === item.protocol) {
        generatedProtocol.value = ''
        generatedProtocolCode.value = ''
      }
      notify(`${protocolLabel(item.protocol)} 授权码已撤销`, 'success')
      await loadProtocolCredentials()
    } catch (error) {
      notify(error?.message || '撤销协议授权码失败')
    } finally {
      protocolLoading.value = false
    }
  }

  // 密码验证弹窗控制
  const openVerifyDialog = () => {
    verifyPassword.value = ''
    verifyDialogVisible.value = true
  }

  const closeVerifyDialog = () => {
    verifyDialogVisible.value = false
    verifyPassword.value = ''
  }

  // 提交登录密码验证并解密回显
  const submitVerifyAndReveal = async () => {
    const password = verifyPassword.value.trim()
    if (!password) {
      notify('请输入当前账号登录密码')
      return
    }

    verifyLoading.value = true
    try {
      const response = await http.post('/api/v1/security/credentials/protocols/reveal', {
        password,
      })
      if (response.errorNo !== 0) {
        throw new Error(response.errorMsg || '密码验证失败，无法查看授权码')
      }

      const codes = response.data?.codes || response.data || {}
      Object.keys(revealedCodes).forEach(k => delete revealedCodes[k])
      Object.assign(revealedCodes, codes)
      codesRevealed.value = true
      closeVerifyDialog()
      notify('安全验证通过，授权码已解密显示', 'success')
    } catch (error) {
      notify(error?.message || '验证失败，请检查密码是否正确')
    } finally {
      verifyLoading.value = false
    }
  }

  // 安全隐藏授权码
  const hideRevealedCodes = () => {
    Object.keys(revealedCodes).forEach(k => delete revealedCodes[k])
    codesRevealed.value = false
    notify('已恢复安全隐藏保护', 'info')
  }

  onMounted(() => {
    loadProtocolCredentials()
  })

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