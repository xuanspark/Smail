import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'

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
  const savedGeneration = ref(0)
  const pendingCandidate = ref(null)
  let savedInput = ''
  const activeGeneration = ref(0)
  const validationResult = ref(null)
  const pluginsPayload = ref(null)
  const settings = reactive({ ...defaultSettings })
  const hasChanges = computed(() => JSON.stringify(settings) !== savedInput)

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
      notifyResponseError(pendingResponse, '未完成的配置更改读取失败')
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
      confirmed: true,
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

  const save = async ({ quiet = false } = {}) => {
    if (!canWrite() || loading.value || working.value || errorMessage.value || pendingCandidate.value) return false
    working.value = 'save'
    try {
      const response = await requestWithRecentAuthentication(
        () => http.put('/api/v1/config/plugins', buildRequest())
      )
      notifyResponseError(response, '插件配置保存失败')
      savedGeneration.value = Number(response.data?.generation || 0)
      pendingCandidate.value = { ...response.data, section: 'plugins', sameSession: true }
      savedInput = JSON.stringify(settings)
      validationResult.value = { valid: true, generation: savedGeneration.value, issues: [] }
      await load()
      if (!quiet) ElMessage.success('垃圾邮件插件配置已暂存，正在完成保存')
      return true
    } catch (error) {
      if (isRecentAuthenticationCancelled(error)) return false
      ElMessage.error(error.message || '插件配置保存失败')
      return false
    } finally {
      working.value = ''
    }
  }

  const apply = async ({ successMessage = '垃圾邮件插件配置已保存' } = {}) => {
    if (!canWrite() || loading.value || working.value || !savedGeneration.value) return false
    if (savedInput !== JSON.stringify(settings)) {
      ElMessage.warning('填写内容已变更，请刷新后核对，或放弃上次未完成的更改')
      return false
    }
    working.value = 'apply'
    try {
      const response = await requestWithRecentAuthentication(() => http.post(
        '/api/v1/config/plugins/apply',
        {
          generation: savedGeneration.value,
          candidateId: pendingCandidate.value?.candidateId
        }
      ))
      notifyResponseError(response, '插件配置保存失败')
      savedGeneration.value = 0
      await load()
      ElMessage.success(successMessage)
      return true
    } catch (error) {
      if (isRecentAuthenticationCancelled(error)) return false
      ElMessage.error(error.message || '垃圾邮件插件配置保存失败')
      return false
    } finally {
      working.value = ''
    }
  }

  const managePending = async action => {
    if (!canWrite() || loading.value || working.value || !pendingCandidate.value) return false
    working.value = action
    try {
      if (action === 'cancel') {
        await ElMessageBox.confirm('放弃后会保留当前生效配置，本次修改不会应用。', '放弃本次更改', { type: 'warning', confirmButtonText: '放弃更改', cancelButtonText: '返回' })
      }
      const response = await http.post('/api/v1/config/pending', {
        generation: pendingCandidate.value.generation, action,
        candidateId: pendingCandidate.value.candidateId,
        confirmation: { confirmed: true, method: 'recent_auth' }
      })
      notifyResponseError(response, '未完成的配置更改处理失败')
      await load()
      if (action === 'cancel') ElMessage.success('本次更改已放弃')
      return true
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || '未完成的配置更改处理失败')
      return false
    } finally {
      working.value = ''
    }
  }

  const saveAndApply = async () => {
    if (pendingCandidate.value) {
      await continuePending()
      return
    }
    const saved = await save({ quiet: true })
    if (saved) await apply({ successMessage: '垃圾邮件插件配置已保存' })
  }

  const continuePending = async () => {
    if (!pendingCandidate.value) return
    if (!pendingCandidate.value.sameSession) {
      const resumed = await managePending('resume')
      if (!resumed) return
    }
    await apply()
  }

  onMounted(load)

  return {
    loading,
    working,
    errorMessage,
    pendingCandidate,
    managePending,
    validationResult,
    settings,
    hasChanges,
    canWrite,
    load,
    validate,
    save,
    apply,
    saveAndApply,
    continuePending
  }
}
