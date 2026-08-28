import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'

const sections = [
  { value: 'base', label: '基础配置' },
  { value: 'smtp', label: 'SMTP 配置' },
  { value: 'email', label: '邮箱策略' },
  { value: 'ssl', label: 'SSL 配置' },
  { value: 'plugins', label: '插件配置' }
]

const serviceLabels = {
  smtp: 'SMTP 服务',
  email: '邮箱服务',
  ssl: 'SSL 服务',
  plugins: '插件服务'
}

const sectionLabels = Object.fromEntries(sections.map(item => [item.value, item.label]))
const operationLabels = {
  validate: '验证配置',
  update: '保存配置',
  apply: '应用配置',
  rollback_preview: '预览回滚',
  rollback_ticket: '签发回滚票据',
  rollback: '执行回滚'
}

export function usePage() {
  const globalStatus = useGlobalStatusStore()
  const loading = ref(false)
  const working = ref('')
  const backupsLoading = ref(false)
  const auditLoading = ref(false)
  const status = reactive({
    setupState: '',
    activeGeneration: 0,
    lastValidAt: '',
    pendingRestart: [],
    degradedReasons: []
  })
  const selectedSection = ref('base')
  const editorText = ref('')
  const sectionError = ref('')
  const secretValue = ref('')
  const confirmed = ref(false)
  const savedGeneration = ref(0)
  const validationResult = ref(null)
  const backups = ref([])
  const rollbackGeneration = ref(0)
  const rollbackConfirmed = ref(false)
  const rollbackPreview = ref(null)
  const auditItems = ref([])

  const canWrite = computed(() => globalStatus.hasCapability('config.write_global'))
  const canRollback = computed(() => globalStatus.hasCapability('config.rollback'))
  const canAudit = computed(() => globalStatus.hasCapability('audit.read'))
  const canExportAudit = computed(() => globalStatus.hasCapability('audit.export'))
  const stateLabel = computed(() => ({ new: '未初始化', bootstrap: '初始化中', ready: '就绪', degraded: '降级' }[status.setupState] || status.setupState || '未知'))
  const stateTagType = computed(() => ({ ready: 'success', degraded: 'warning', bootstrap: 'info', new: 'danger' }[status.setupState] || 'info'))
  const secretField = computed(() => {
    if (selectedSection.value === 'base') {
      return { label: '数据库密码', description: '只在本次请求内传输，保存后仅保留密文状态。留空表示保持原值。', placeholder: '输入新数据库密码（可选）' }
    }
    if (selectedSection.value === 'smtp') {
      return { label: 'SMTP Relay 密码', description: '只在本次请求内传输，保存后仅保留密文状态。留空表示保持原值。', placeholder: '输入新的 Relay 密码（可选）' }
    }
    return null
  })
  const sensitiveHint = computed(() => secretField.value ? '密码字段已脱敏，修改请使用下方输入框' : '当前分区不包含可编辑密文')

  const notifyResponseError = (response, fallback) => {
    if (!response || response.errorNo !== 0) {
      throw new Error(response?.errorMsg || fallback)
    }
  }

  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
  const serviceLabel = value => serviceLabels[value] || value
  const sectionLabel = value => sectionLabels[value] || value || '-'
  const operationLabel = value => operationLabels[value] || value || '-'

  const loadStatus = async () => {
    const response = await http.get('/api/v1/config/status')
    notifyResponseError(response, '配置状态读取失败')
    Object.assign(status, response.data || {})
  }

  const loadSection = async () => {
    sectionError.value = ''
    secretValue.value = ''
    validationResult.value = null
    savedGeneration.value = 0
    try {
      const response = await http.get(`/api/v1/config/${selectedSection.value}`)
      notifyResponseError(response, '配置分区读取失败')
      editorText.value = JSON.stringify(response.data || {}, null, 2)
    } catch (error) {
      sectionError.value = error.message || '配置分区读取失败'
      editorText.value = ''
    }
  }

  const loadBackups = async () => {
    if (!canRollback.value) return
    backupsLoading.value = true
    try {
      const response = await http.get('/api/v1/config/backups')
      notifyResponseError(response, '配置备份读取失败')
      backups.value = Array.isArray(response.data) ? response.data : []
    } catch (error) {
      ElMessage.error(error.message || '配置备份读取失败')
    } finally {
      backupsLoading.value = false
    }
  }

  const loadAudit = async () => {
    if (!canAudit.value) return
    auditLoading.value = true
    try {
      const response = await http.get('/api/v1/audits/config?limit=50')
      notifyResponseError(response, '配置审计读取失败')
      auditItems.value = Array.isArray(response.data) ? response.data : []
    } catch (error) {
      ElMessage.error(error.message || '配置审计读取失败')
    } finally {
      auditLoading.value = false
    }
  }

  const loadAll = async () => {
    loading.value = true
    try {
      await Promise.all([loadStatus(), loadSection(), loadBackups(), loadAudit()])
    } catch (error) {
      ElMessage.error(error.message || '配置管理数据读取失败')
    } finally {
      loading.value = false
    }
  }

  const parsePayload = () => {
    try {
      const payload = JSON.parse(editorText.value)
      if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
        throw new Error('配置必须是 JSON 对象')
      }
      if (selectedSection.value === 'base') {
        delete payload.databasePasswordStatus
      }
      if (selectedSection.value === 'smtp') {
        delete payload.relayPasswordStatus
      }
      if (selectedSection.value === 'plugins' && payload.plugins && typeof payload.plugins === 'object') {
        Object.values(payload.plugins).forEach(plugin => {
          if (plugin && typeof plugin === 'object') {
            delete plugin.settingsConfigured
          }
        })
      }
      return payload
    } catch (error) {
      throw new Error(error.message || 'JSON 格式错误')
    }
  }

  const buildRequest = () => {
    const secretChanges = {}
    const value = secretValue.value.trim()
    if (value && selectedSection.value === 'base') {
      secretChanges['database.password'] = { operation: 'replace', value: { plaintext: value } }
    }
    if (value && selectedSection.value === 'smtp') {
      secretChanges['relay.password'] = { operation: 'replace', value: { plaintext: value } }
    }
    return {
      expectedGeneration: Number(status.activeGeneration),
      payload: parsePayload(),
      secretChanges,
      confirmation: { confirmed: confirmed.value, method: 'recent_auth' }
    }
  }

  const validateConfig = async () => {
    working.value = 'validate'
    try {
      const request = buildRequest()
      const response = await http.post(`/api/v1/config/${selectedSection.value}/validate`, request)
      notifyResponseError(response, '配置验证失败')
      validationResult.value = response.data || null
      ElMessage[validationResult.value?.valid ? 'success' : 'warning'](validationResult.value?.valid ? '配置验证通过' : '配置验证未通过')
    } catch (error) {
      ElMessage.error(error.message || '配置验证失败')
    } finally {
      working.value = ''
    }
  }

  const saveConfig = async () => {
    working.value = 'save'
    try {
      const request = buildRequest()
      const response = await http.put(`/api/v1/config/${selectedSection.value}`, request)
      notifyResponseError(response, '配置保存失败')
      savedGeneration.value = response.data?.generation || 0
      validationResult.value = { valid: true, generation: savedGeneration.value, issues: [] }
      await loadStatus()
      ElMessage.success(`候选配置已保存，代次 ${savedGeneration.value} 等待应用`)
    } catch (error) {
      ElMessage.error(error.message || '配置保存失败')
    } finally {
      working.value = ''
    }
  }

  const applyConfig = async () => {
    if (!savedGeneration.value) return
    working.value = 'apply'
    try {
      const response = await http.post(`/api/v1/config/${selectedSection.value}/apply`, { generation: savedGeneration.value })
      notifyResponseError(response, '配置应用失败')
      savedGeneration.value = 0
      await Promise.all([loadStatus(), loadSection()])
      ElMessage.success('配置已应用')
    } catch (error) {
      ElMessage.error(error.message || '配置应用失败')
    } finally {
      working.value = ''
    }
  }

  const previewRollback = async () => {
    if (!rollbackGeneration.value) return
    working.value = 'preview'
    rollbackPreview.value = null
    try {
      const response = await http.post('/api/v1/config/rollback/preview', { targetGeneration: rollbackGeneration.value })
      notifyResponseError(response, '回滚预览失败')
      rollbackPreview.value = response.data || null
      ElMessage.success('回滚预览已生成，请确认变更分区')
    } catch (error) {
      ElMessage.error(error.message || '回滚预览失败')
    } finally {
      working.value = ''
    }
  }

  const executeRollback = async () => {
    if (!rollbackPreview.value || !rollbackConfirmed.value) return
    try {
      await ElMessageBox.confirm('回滚会替换当前候选配置并可能需要重启服务，确定继续吗？', '确认执行回滚', { type: 'warning', confirmButtonText: '继续回滚', cancelButtonText: '取消' })
      working.value = 'rollback'
      const ticketResponse = await http.post('/api/v1/config/rollback/ticket', {
        targetGeneration: rollbackPreview.value.targetGeneration,
        confirmation: { confirmed: true, method: 'recent_auth' }
      })
      notifyResponseError(ticketResponse, '回滚票据签发失败')
      const response = await http.post('/api/v1/config/rollback', { ticket: ticketResponse.data?.ticket })
      notifyResponseError(response, '配置回滚失败')
      rollbackPreview.value = null
      rollbackConfirmed.value = false
      await Promise.all([loadStatus(), loadSection(), loadBackups()])
      ElMessage.success('配置已回滚')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || '配置回滚失败')
    } finally {
      working.value = ''
    }
  }

  const exportAudit = async format => {
    working.value = format === 'csv' ? 'export-csv' : 'export'
    try {
      const response = await http.post('/api/v1/audits/config', { format, limit: 500 }, { responseType: 'blob' })
      const blob = response.data instanceof Blob ? response.data : new Blob([response.data], { type: format === 'csv' ? 'text/csv' : 'application/json' })
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `smail-config-audit.${format}`
      anchor.click()
      URL.revokeObjectURL(url)
      ElMessage.success('审计记录已导出')
    } catch (error) {
      ElMessage.error(error.message || '审计导出失败')
    } finally {
      working.value = ''
    }
  }

  onMounted(loadAll)

  return {
    loading, working, status, stateLabel, stateTagType, sectionOptions: sections,
    selectedSection, editorText, sectionError, sensitiveHint, secretField, secretValue,
    confirmed, savedGeneration, validationResult, canWrite, canRollback, canAudit, canExportAudit,
    backupsLoading, backups, rollbackGeneration, rollbackConfirmed, rollbackPreview,
    auditLoading, auditItems, loadAll, loadSection, validateConfig, saveConfig, applyConfig,
    loadBackups, previewRollback, executeRollback, loadAudit, exportAudit, formatTime,
    serviceLabel, sectionLabel, operationLabel
  }
}
