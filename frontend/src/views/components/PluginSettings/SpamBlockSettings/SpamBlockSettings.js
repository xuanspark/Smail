import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
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
      const [statusResponse, pluginsResponse] = await Promise.all([
        http.get('/api/v1/config/status'),
        http.get('/api/v1/config/plugins')
      ])
      notifyResponseError(statusResponse, '配置状态读取失败')
      notifyResponseError(pluginsResponse, '插件配置读取失败')
      activeGeneration.value = Number(statusResponse.data?.activeGeneration || 0)
      pluginsPayload.value = pluginsResponse.data || null
      applySettings(pluginsPayload.value?.plugins?.spam_block?.settings)
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
    working.value = 'save'
    try {
      const response = await http.put('/api/v1/config/plugins', buildRequest())
      notifyResponseError(response, '插件配置保存失败')
      savedGeneration.value = Number(response.data?.generation || 0)
      validationResult.value = { valid: true, generation: savedGeneration.value, issues: [] }
      ElMessage.success(`垃圾邮件插件配置已保存，代次 ${savedGeneration.value} 等待应用`)
    } catch (error) {
      ElMessage.error(error.message || '插件配置保存失败')
    } finally {
      working.value = ''
    }
  }

  const apply = async () => {
    if (!savedGeneration.value) return
    working.value = 'apply'
    try {
      const response = await http.post('/api/v1/config/plugins/apply', { generation: savedGeneration.value })
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

  onMounted(load)

  return {
    loading,
    working,
    errorMessage,
    confirmed,
    savedGeneration,
    validationResult,
    settings,
    canWrite,
    load,
    validate,
    save,
    apply
  }
}