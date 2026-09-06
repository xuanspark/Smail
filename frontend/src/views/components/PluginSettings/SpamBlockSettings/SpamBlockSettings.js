import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'

const defaultSettings = {
  url: '',
  timeout: 3000,
  threshold: 20
}

const isObject = value => value && typeof value === 'object' && !Array.isArray(value)

export function useSpamBlockSettings() {
  const globalStatus = useGlobalStatusStore()
  const loading = ref(false)
  const working = ref('')
  const errorMessage = ref('')
  const confirmed = ref(false)
  const savedGeneration = ref(0)
  const pendingCandidate = ref(null)
  let savedInput = ''
  const activeGeneration = ref(0)
  const validationResult = ref(null)
  const pluginsPayload = ref(null)
  const settings = reactive({ ...defaultSettings })

  const canWrite = () => globalStatus.hasCapability('config.write_global')

  const notifyResponseError = (response, fallback) => {
    if (!response || response.errorNo !== 0) {
      throw new Error(response?.errorMsg || fallback)
    }
  }

  const applySettings = value => {
    const source = isObject(value) ? value : {}
    settings.url = typeof source.url === 'string' ? source.url : defaultSettings.url
    settings.timeout = Number.isFinite(Number(source.timeout)) ? Number(source.timeout) : defaultSettings.timeout
    settings.threshold = Number.isFinite(Number(source.threshold)) ? Number(source.threshold) : defaultSettings.threshold
  }

  const load = async () => {
    loading.value = true
    errorMessage.value = ''
    try {
      const [statusResponse, pluginsResponse, pendingResponse] = await Promise.all([
        http.get('/api/v1/config/status'),
        http.get('/api/v1/config/plugins'),
        http.get('/api/v1/config/pending')
      ])
      notifyResponseError(statusResponse, '配置状态读取失败')
      notifyResponseError(pluginsResponse, '插件配置读取失败')
      notifyResponseError(pendingResponse, '候选配置读取失败')
      pendingCandidate.value = pendingResponse.data || null
      savedGeneration.value = 0
      activeGeneration.value = Number(statusResponse.data?.activeGeneration || 0)
      pluginsPayload.value = pluginsResponse.data || null
      if (pendingCandidate.value?.section === 'plugins') {
        pluginsPayload.value = pendingCandidate.value.payload
        if (pendingCandidate.value.sameSession) savedGeneration.value = Number(pendingCandidate.value.generation)
      }
      applySettings(pluginsPayload.value?.plugins?.spam_block?.settings)
      savedInput = JSON.stringify(settings)
    } catch (error) {
      errorMessage.value = error.message || '垃圾邮件插件配置读取失败'
    } finally {
      loading.value = false
    }
  }

  const buildPayload = () => {
    if (!isObject(pluginsPayload.value) || !isObject(pluginsPayload.value.plugins)) {
      throw new Error('插件配置格式无效')
    }
    const payload = JSON.parse(JSON.stringify(pluginsPayload.value))
    Object.values(payload.plugins).forEach(plugin => {
      if (isObject(plugin)) {
        delete plugin.secretStatus
        delete plugin.settingsConfigured
      }
    })
    if (!isObject(payload.plugins.spam_block)) {
      throw new Error('垃圾邮件插件未安装或配置不存在')
    }
    payload.plugins.spam_block.settings = {
      url: settings.url.trim(),
      timeout: Number(settings.timeout),
      threshold: Number(settings.threshold)
    }
    return payload
  }

  const buildRequest = () => ({
    expectedGeneration: activeGeneration.value,
    payload: buildPayload(),
    confirmation: {
      confirmed: confirmed.value,
      method: 'recent_auth'
    }
  })

  const validate = async () => {
    if (!canWrite() || loading.value || working.value || errorMessage.value || pendingCandidate.value) return
    working.value = 'validate'
    try {
      const response = await http.post('/api/v1/config/plugins/validate', buildRequest())
      notifyResponseError(response, '插件配置验证失败')
      validationResult.value = response.data || null
      ElMessage[validationResult.value?.valid ? 'success' : 'warning'](
        validationResult.value?.valid ? '垃圾邮件插件配置验证通过' : '垃圾邮件插件配置验证未通过'
      )
    } catch (error) {
      ElMessage.error(error.message || '插件配置验证失败')
    } finally {
      working.value = ''
    }
  }

  const save = async () => {
    if (!canWrite() || loading.value || working.value || errorMessage.value || pendingCandidate.value) return
    working.value = 'save'
    try {
      const response = await http.put('/api/v1/config/plugins', buildRequest())
      notifyResponseError(response, '插件配置保存失败')
      savedGeneration.value = Number(response.data?.generation || 0)
      pendingCandidate.value = { ...response.data, section: 'plugins', sameSession: true }
      savedInput = JSON.stringify(settings)
      validationResult.value = { valid: true, generation: savedGeneration.value, issues: [] }
      await load()
      ElMessage.success(`垃圾邮件插件配置已保存，代次 ${savedGeneration.value} 等待应用`)
    } catch (error) {
      ElMessage.error(error.message || '插件配置保存失败')
    } finally {
      working.value = ''
    }
  }

  const apply = async () => {
    if (!canWrite() || loading.value || working.value || !savedGeneration.value) return
    if (savedInput !== JSON.stringify(settings)) {
      ElMessage.warning('填写内容与候选不一致，请刷新查看候选，或先取消候选再修改')
      return
    }
    working.value = 'apply'
    try {
      const response = await http.post('/api/v1/config/plugins/apply', {
        generation: savedGeneration.value,
        candidateId: pendingCandidate.value?.candidateId
      })
      notifyResponseError(response, '插件配置应用失败')
      savedGeneration.value = 0
      await load()
      ElMessage.success('垃圾邮件插件配置已应用')
    } catch (error) {
      ElMessage.error(error.message || '垃圾邮件插件配置应用失败')
    } finally {
      working.value = ''
    }
  }

  const managePending = async action => {
    if (!canWrite() || loading.value || working.value || !pendingCandidate.value) return
    working.value = action
    try {
      await ElMessageBox.confirm(action === 'cancel' ? '取消候选后不会影响当前生效配置，确认继续吗？' : '确认将已保存候选恢复到当前会话吗？', '管理候选配置', { type: 'warning', confirmButtonText: '确认', cancelButtonText: '返回' })
      const response = await http.post('/api/v1/config/pending', {
        generation: pendingCandidate.value.generation, action,
        candidateId: pendingCandidate.value.candidateId,
        confirmation: { confirmed: confirmed.value, method: 'recent_auth' }
      })
      notifyResponseError(response, '候选配置操作失败')
      await load()
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || '候选配置操作失败')
    } finally {
      working.value = ''
    }
  }

  onMounted(load)

  return {
    loading,
    working,
    errorMessage,
    confirmed,
    savedGeneration,
    pendingCandidate,
    managePending,
    validationResult,
    settings,
    canWrite,
    load,
    validate,
    save,
    apply
  }
}
