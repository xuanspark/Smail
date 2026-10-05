import { computed, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'
import { readFormConfig, updateFormConfig, validateFormConfig } from './ConfigForm/ConfigForm.js'

const sections = [
  { value: 'base', label: '基础配置' },
  { value: 'mail', label: '邮件服务' },
  { value: 'mail_policy', label: '邮件策略' },
  { value: 'security_policy', label: '安全策略' },
  { value: 'ssl', label: 'SSL 配置' },
  { value: 'plugins', label: '插件配置' },
  { value: 'spam_block', label: '垃圾邮件拦截' },
  { value: 'telegram_push', label: 'Telegram 推送' },
  { value: 'wechat_push', label: '微信推送' }
]

const serviceLabels = {
  database: '数据库服务',
  smtp: 'SMTP 服务',
  email: '邮箱服务',
  ssl: 'SSL 服务',
  plugins: '插件服务'
}

const sectionLabels = Object.fromEntries(sections.map(item => [item.value, item.label]))

export function usePage(pageKey = 'base') {
  const isConfigPage = sections.some(item => item.value === pageKey)
  const globalStatus = useGlobalStatusStore()
  const loading = ref(false)
  const availabilityError = ref('')
  const working = ref('')
  // 一次保存跨越暂存、应用和回读；中间候选不是需要用户恢复的失败记录。
  const saveFlow = ref('')
  const busy = computed(() => !!working.value || !!saveFlow.value)
  const backupsLoading = ref(false)
  const backupsError = ref('')
  const status = reactive({
    setupState: '',
    tlsMode: '',
    activeGeneration: 0,
    lastValidAt: '',
    pendingRestart: [],
    degradedReasons: []
  })
  const selectedSection = ref(isConfigPage ? pageKey : 'base')
  // 独立页面复用原配置分区的原子保存与候选保护，不丢弃其他页面的字段。
  const pluginPages = ['spam_block', 'telegram_push', 'wechat_push']
  const apiSection = () => pluginPages.includes(selectedSection.value) ? 'plugins'
    : ['mail_policy', 'security_policy'].includes(selectedSection.value) ? 'mail' : selectedSection.value
  const currentSection = computed(() => sections.find(item => item.value === selectedSection.value) || sections[0])
  const sectionLoading = ref(false)
  let sectionRequestId = 0
  let disposed = false
  const editorText = ref('')
  const formConfig = computed(() => readFormConfig(editorText.value))
  const formErrors = ref({})
  const loadedText = ref('')
  const sectionError = ref('')
  const saveError = ref('')
  const secretValue = ref('')
  const pluginSecretStatus = ref(null)
  const pluginSecretPage = computed(() => ['telegram_push', 'wechat_push'].includes(selectedSection.value))
  const confirmed = ref(false)
  const savedGeneration = ref(0)
  const pendingCandidate = ref(null)
  const showPendingRecovery = computed(() => !!pendingCandidate.value && !saveFlow.value)
  const continuingSave = computed(() => saveFlow.value === 'resume' || showPendingRecovery.value)
  const pendingScopeHint = computed(() => {
    const pending = pendingCandidate.value
    if (pending?.section === 'plugins' && apiSection() === 'plugins') {
      const active = readFormConfig(loadedText.value).plugins || {}
      const candidate = pending.payload?.plugins || {}
      const changed = Object.keys({ ...active, ...candidate }).filter(id =>
        ['enabled', 'executable', 'permissions', 'settings'].some(key =>
          JSON.stringify(active[id]?.[key]) !== JSON.stringify(candidate[id]?.[key]))
      ).map(id => sections.find(item => item.value === id)?.label || id)
      const scope = pending.payload && loadedText.value
        ? (changed.length ? `上次未完成的保存涉及：${changed.join('、')}。` : '上次未完成保存的插件运行配置未变化。')
        : '上次未完成保存的内容尚未完整读取，请刷新后核对。'
      return `${scope}三个插件共用一组运行配置，请在“插件配置”的对应页面核对；完成保存时会统一生效，其他插件的原有配置会保留。`
    }
    if (pending?.section !== 'mail' || apiSection() !== 'mail') return ''
    const active = readFormConfig(loadedText.value)
    const candidate = pending.payload || {}
    const changed = [
      ['listener', 'SMTP 监听'],
      ['imap', 'IMAP 监听'],
      ['pop3', 'POP3 监听'],
      ['relay', 'SMTP 中继'],
      ['policy', '邮件策略']
    ].filter(([key]) => {
      const before = active[key] || {}
      const after = candidate[key] || {}
      return Object.keys({ ...before, ...after }).some(field => JSON.stringify(before[field]) !== JSON.stringify(after[field]))
    }).map(([, label]) => label)
    const scope = pending.payload && loadedText.value
      ? (changed.length ? `上次未完成的保存涉及：${changed.join('、')}。` : '上次未完成保存的邮件配置未变化。')
      : '上次未完成保存的内容尚未完整读取，请刷新后核对。'
    return `${scope}邮件服务、邮件策略和安全策略共用一组运行配置；完成保存时全部变更一起生效，监听端口变更仍需重启后端。`
  })
  let savedInput = null
  const validationResult = ref(null)
  const backups = ref([])
  const rollbackGeneration = ref(0)
  const rollbackConfirmed = ref(false)
  const rollbackPreview = ref(null)
  const databaseConfig = reactive({ type: '', name: '', localRef: '' })
  const migrationTargetName = ref('')
  const migrationTarget = reactive({ type: 'sqlite', host: '127.0.0.1', port: 3306, username: '', password: '' })
  const migrationError = ref('')
  let migrationInput = ''
  const migrationPreview = ref(null)
  const migrationToken = ref('')
  const migrationRecord = ref(null)
  const invalidateMigration = () => {
    migrationPreview.value = null
    migrationToken.value = ''
    migrationInput = ''
    migrationError.value = ''
  }
  const changeMigrationType = type => {
    migrationTarget.type = type
    migrationTarget.port = type === 'postgresql' ? 5432 : 3306
    migrationTarget.password = ''
    migrationTargetName.value = ''
    invalidateMigration()
  }
  const migrationPayload = () => migrationTarget.type === 'sqlite'
    ? { type: 'sqlite', localRef: migrationTargetName.value.trim() }
    : {
        type: migrationTarget.type,
        host: migrationTarget.host.trim(),
        port: Number(migrationTarget.port),
        username: migrationTarget.username.trim(),
        name: migrationTargetName.value.trim(),
        password: { plaintext: migrationTarget.password }
      }
  const migrationReady = computed(() => {
    if (!migrationTargetName.value.trim() || status.pendingRestart?.length) return false
    if (migrationTarget.type === 'sqlite') return /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(migrationTargetName.value.trim())
    return ['mysql', 'postgresql'].includes(migrationTarget.type) &&
      /^[A-Za-z][A-Za-z0-9_-]{0,62}$/.test(migrationTargetName.value.trim()) &&
      !!migrationTarget.host.trim() && !!migrationTarget.username.trim() && !!migrationTarget.password &&
      Number.isInteger(Number(migrationTarget.port)) && Number(migrationTarget.port) > 0 && Number(migrationTarget.port) <= 65535
  })
  const restartPreview = ref(null)
  const hasChanges = computed(() => editorText.value !== loadedText.value || !!secretValue.value || !!savedGeneration.value)
  const hasConfigChanges = computed(() => editorText.value !== loadedText.value || (!pluginSecretPage.value && !!secretValue.value) || !!savedGeneration.value)

  const canWrite = computed(() => {
    if (pluginSecretPage.value && globalStatus.role !== 'super_admin') return false
    return globalStatus.hasCapability(apiSection() === 'plugins' ? 'plugin.write_global' : 'config.write_global')
  })
  const canRollback = computed(() => globalStatus.hasCapability('config.rollback'))
  const canMigrate = computed(() => globalStatus.hasCapability('database.migrate'))
  const canRestart = computed(() => globalStatus.hasCapability('service.restart'))
  const stateLabel = computed(() => ({ new: '未初始化', bootstrap: '初始化中', ready: '就绪', degraded: '降级' }[status.setupState] || status.setupState || '未知'))
  const stateTagType = computed(() => ({ ready: 'success', degraded: 'warning', bootstrap: 'info', new: 'danger' }[status.setupState] || 'info'))
  const secretField = computed(() => {
    if (selectedSection.value === 'base' && ['mysql', 'postgresql'].includes(formConfig.value.database?.type)) {
      return { label: '数据库密码', description: '只在本次请求内传输，保存后仅保留密文状态。留空表示保持原值。', placeholder: '输入新数据库密码（可选）' }
    }
    const pluginSecrets = { telegram_push: 'Telegram Bot Token', wechat_push: '微信 Secret' }
    const label = pluginSecrets[selectedSection.value]
    if (label && formConfig.value.plugins?.[selectedSection.value]) {
      const configured = pluginSecretStatus.value?.configured
      return {
        label,
        description: `${configured ? '已设置' : '未设置'}。加密存储，不回显；保存后下次启动插件时生效。`,
        placeholder: `输入新的 ${label}（可选）`
      }
    }
    return null
  })
  const sensitiveHint = computed(() => secretField.value ? '密码不会回填，修改请使用下方独立输入框，留空保持原值' : '')

  const invalidateCandidate = () => {
    savedGeneration.value = 0
    savedInput = null
    validationResult.value = null
    confirmed.value = false
  }
  const canOperate = () => !disposed && canWrite.value && !loading.value && !working.value && !sectionLoading.value && !sectionError.value && !availabilityError.value
  const canEdit = () => canOperate() && !pendingCandidate.value && !savedGeneration.value
  const updateField = (path, value) => {
    if (saveFlow.value || !canEdit()) return
    const updated = updateFormConfig(editorText.value, selectedSection.value, path, value)
    if (updated === editorText.value) return
    editorText.value = updated
    saveError.value = ''
    if (Object.keys(formErrors.value).length) formErrors.value = validateFormConfig(selectedSection.value, formConfig.value)
    invalidateCandidate()
  }
  const updateSecret = value => {
    if (saveFlow.value || !canEdit() || !secretField.value || secretValue.value === value) return
    if (pluginSecretPage.value && !pluginSecretStatus.value) return
    secretValue.value = value
    saveError.value = ''
    invalidateCandidate()
  }

  const notifyResponseError = (response, fallback) => {
    if (!response || response.errorNo !== 0) {
      throw new Error(response?.errorMsg || fallback)
    }
  }

  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
  const serviceLabel = value => serviceLabels[value] || value
  const sectionLabel = value => sectionLabels[value] || value || '-'
  const migrationStatusLabels = {
    previewed: '已生成预览',
    created: '已创建',
    copying: '复制中',
    verifying: '校验中',
    switching: '切换中',
    succeeded: '已完成',
    failed: '失败'
  }
  const migrationStatusLabel = value => migrationStatusLabels[value] || value || '未知'
  const migrationStatusType = value => ({
    previewed: 'info',
    created: 'info',
    copying: 'warning',
    verifying: 'warning',
    switching: 'warning',
    succeeded: 'success',
    failed: 'danger'
  }[value] || 'info')
  const databaseTypeLabel = value => ({ mysql: 'MySQL', postgresql: 'PostgreSQL', sqlite: 'SQLite' }[value] || value || '-')
  const loadStatus = async () => {
    const response = await http.get('/api/v1/config/status')
    notifyResponseError(response, '配置状态读取失败')
    Object.assign(status, response.data || {})
  }

  const loadPending = async () => {
    const response = await http.get('/api/v1/config/pending')
    if (disposed) return
    notifyResponseError(response, '未完成的配置更改读取失败')
    pendingCandidate.value = response.data || null
    savedGeneration.value = 0
    savedInput = null
    const pending = pendingCandidate.value
    const section = pending?.section === 'root' ? 'base' : pending?.section
    if (pending && section === apiSection()) {
      editorText.value = JSON.stringify(pending.payload, null, 2)
      secretValue.value = ''
      savedInput = { text: editorText.value, secret: '' }
      savedGeneration.value = pending.sameSession ? Number(pending.generation) : 0
    }
  }

  const managePending = async action => {
    if (!canOperate() || !pendingCandidate.value) return false
    saveError.value = ''
    working.value = action
    try {
      if (action === 'cancel') {
        await ElMessageBox.confirm(
          '放弃后会保留当前生效配置，本次修改不会应用。',
          '放弃本次更改',
          { type: 'warning', confirmButtonText: '放弃更改', cancelButtonText: '返回' }
        )
      }
      const response = await http.post('/api/v1/config/pending', {
        generation: pendingCandidate.value.generation,
        candidateId: pendingCandidate.value.candidateId,
        action,
        confirmation: { confirmed: true, method: 'recent_auth' }
      })
      notifyResponseError(response, '未完成的配置更改处理失败')
      await loadStatus()
      await loadSection()
      await loadPending()
      if (action === 'cancel') ElMessage.success('本次更改已放弃')
      return true
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') {
        saveError.value = error.message || '未完成的配置更改处理失败'
        ElMessage.error(saveError.value)
      }
      return false
    } finally {
      working.value = ''
    }
  }

  const loadSection = async ({ minimumGeneration = 0 } = {}) => {
    const requestId = ++sectionRequestId
    const section = apiSection()
    sectionLoading.value = true
    sectionError.value = ''
    saveError.value = ''
    formErrors.value = {}
    editorText.value = ''
    loadedText.value = ''
    secretValue.value = ''
    pluginSecretStatus.value = null
    confirmed.value = false
    validationResult.value = null
    savedGeneration.value = 0
    savedInput = null
    try {
      const response = await http.get(`/api/v1/config/${section}`)
      if (requestId !== sectionRequestId || disposed) return false
      notifyResponseError(response, '配置分区读取失败')
      const publicConfig = response.data || {}
      const generation = Number(section === 'base' ? publicConfig.generation : publicConfig.meta?.generation)
      if (minimumGeneration && (!Number.isSafeInteger(generation) || generation < minimumGeneration)) {
        throw new Error('读取到的配置版本落后于本次保存，请刷新状态核对')
      }
      editorText.value = JSON.stringify(publicConfig, null, 2)
      loadedText.value = editorText.value
      if (pluginSecretPage.value) {
        const secretResponse = await http.get(`/api/v1/plugin/secrets/${selectedSection.value}`)
        if (requestId !== sectionRequestId || disposed) return false
        notifyResponseError(secretResponse, '插件密钥状态读取失败')
        const secretStatus = secretResponse.data
        if (typeof secretStatus?.configured !== 'boolean' || !Number.isSafeInteger(secretStatus.version) || secretStatus.version < 0) {
          throw new Error('插件密钥状态不完整，请刷新后重试')
        }
        pluginSecretStatus.value = secretStatus
      }
      if (section === 'base') {
        const database = publicConfig.database || {}
        Object.assign(databaseConfig, { type: database.type || '', name: database.name || '', localRef: database.localRef || '' })
      }
      return true
    } catch (error) {
      if (requestId !== sectionRequestId || disposed) return false
      sectionError.value = error.message || '配置分区读取失败'
      editorText.value = ''
      return false
    } finally {
      if (requestId === sectionRequestId) sectionLoading.value = false
    }
  }

  const loadBackups = async () => {
    if (!canRollback.value) return
    backupsLoading.value = true
    backupsError.value = ''
    try {
      const response = await http.get('/api/v1/config/backups')
      notifyResponseError(response, '配置备份读取失败')
      backups.value = Array.isArray(response.data) ? response.data : []
    } catch (error) {
      backups.value = []
      rollbackGeneration.value = 0
      rollbackPreview.value = null
      rollbackConfirmed.value = false
      backupsError.value = error.message || '配置备份读取失败'
    } finally {
      backupsLoading.value = false
    }
  }

  const previewDatabaseMigration = async () => {
    if (!canMigrate.value || working.value || sectionError.value || !migrationReady.value) return
    working.value = 'migration-preview'
    migrationPreview.value = null
    migrationRecord.value = null
    migrationToken.value = ''
    migrationError.value = ''
    try {
      const target = migrationPayload()
      const response = await http.post('/api/v1/database/migrations/preview', {
        target,
        expectedGeneration: Number(status.activeGeneration)
      })
      if (disposed) return
      notifyResponseError(response, '数据库迁移预览失败')
      migrationPreview.value = response.data?.migration || null
      migrationRecord.value = migrationPreview.value
      migrationToken.value = response.data?.token || ''
      migrationInput = JSON.stringify(target)
      ElMessage.success('目标连接验证通过，请核对数据库类型和连接后执行')
    } catch (error) {
      migrationError.value = error.message || '数据库迁移预览失败'
      ElMessage.error(error.message || '数据库迁移预览失败')
    } finally {
      working.value = ''
    }
  }

  const loadMigrationStatus = async () => {
    if (!migrationRecord.value?.id) return
    working.value = 'migration-status'
    try {
      const response = await http.get(`/api/v1/database/migrations/${migrationRecord.value.id}`)
      notifyResponseError(response, '数据库迁移状态读取失败')
      migrationRecord.value = response.data || migrationRecord.value
    } catch (error) {
      ElMessage.error(error.message || '数据库迁移状态读取失败')
    } finally {
      working.value = ''
    }
  }

  const executeDatabaseMigration = async () => {
    if (!canMigrate.value || working.value || !migrationPreview.value?.id || !migrationToken.value) return
    if (migrationInput !== JSON.stringify(migrationPayload())) {
      invalidateMigration()
      migrationError.value = '目标连接已修改，请重新生成迁移预览'
      return
    }
    working.value = 'migration-confirm'
    try {
      await ElMessageBox.confirm(
        `将迁移到 ${databaseTypeLabel(migrationPreview.value.target?.type)}：${migrationPreview.value.target?.name || migrationPreview.value.target?.localRef || migrationTargetName.value.trim()}。迁移期间暂停写入，成功后必须立即重启后端。旧数据库及失败目标均保留。确认继续吗？`,
        '确认执行数据库迁移',
        { type: 'warning', confirmButtonText: '继续迁移', cancelButtonText: '取消' }
      )
      working.value = 'migration-execute'
      const response = await http.post('/api/v1/database/migrations', {
        id: migrationPreview.value.id,
        token: migrationToken.value
      }, { timeout: 11 * 60 * 1000 })
      notifyResponseError(response, '数据库迁移执行失败')
      migrationRecord.value = response.data || migrationPreview.value
      migrationToken.value = ''
      migrationPreview.value = null
      migrationTarget.password = ''
      migrationInput = ''
      await Promise.all([loadStatus(), loadSection()])
      if (migrationRecord.value.status === 'succeeded') {
        ElMessage.success('数据已复制校验并切换配置，请立即重启后端以恢复写入；旧数据库保留')
      } else {
        ElMessage.warning(`数据库迁移${migrationStatusLabel(migrationRecord.value.status)}`)
      }
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') {
        migrationToken.value = ''
        migrationPreview.value = null
        migrationTarget.password = ''
        migrationInput = ''
        migrationError.value = error.message || '数据库迁移执行失败'
        await loadMigrationStatus()
        ElMessage.error(migrationError.value)
      }
    } finally {
      working.value = ''
    }
  }

  const previewRestart = async () => {
    if (!canRestart.value || working.value) return
    working.value = 'restart-preview'
    try {
      const response = await http.post('/api/v1/system/restart/preview', {})
      notifyResponseError(response, '重启预览读取失败')
      restartPreview.value = response.data || null
    } catch (error) {
      ElMessage.error(error.message || '重启预览读取失败')
    } finally {
      working.value = ''
    }
  }

  const requestRestart = async () => {
    if (!canRestart.value || working.value || !restartPreview.value?.triggerAvailable) return
    working.value = 'restart-confirm'
    try {
      await ElMessageBox.confirm(
        '服务将按固定服务管理器重启，当前页面可能会短暂断开。确认继续吗？',
        '确认重启 Smail',
        { type: 'warning', confirmButtonText: '确认重启', cancelButtonText: '取消' }
      )
      working.value = 'restart'
      const response = await http.post('/api/v1/system/restart', {})
      notifyResponseError(response, '重启请求提交失败')
      ElMessage.success('重启请求已提交，请等待服务恢复')
      restartPreview.value = null
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || '重启请求提交失败')
    } finally {
      working.value = ''
    }
  }
  const loadAll = async () => {
    invalidateMigration()
    migrationTarget.password = ''
    loading.value = true
    availabilityError.value = ''
    try {
      await loadStatus()
      if (disposed) return
      if (status.available === false) {
        availabilityError.value = status.unavailableReason || '在线配置管理尚未启用'
        return
      }
      if (isConfigPage || (pageKey === 'migration' && canMigrate.value)) await loadSection()
      if (isConfigPage) await loadPending()
      if (pageKey === 'backups') await loadBackups()
    } catch (error) {
      availabilityError.value = error.message || '配置管理数据读取失败'
    } finally {
      loading.value = false
    }
  }

  const parsePayload = () => {
    try {
      const payload = JSON.parse(editorText.value)
      if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
        throw new Error('配置内容不可用，请刷新页面后重试')
      }
      if (selectedSection.value === 'base') {
        delete payload.databasePasswordStatus
      }
      if (apiSection() === 'ssl') {
        for (const name of ['dnsAccessKey', 'dnsSecretKey']) {
          delete payload[`${name}Status`]
          if (payload.tls) {
            delete payload.tls[`${name}Input`]
            delete payload.tls[name]
          }
        }
      }
      if (apiSection() === 'mail') {
        delete payload.relayPasswordStatus
        if (payload.relay && 'password' in payload.relay) {
          delete payload.relay.password
          if (Object.keys(payload.relay).length === 0) {
            delete payload.relay
          }
        }
      }
      if (apiSection() === 'plugins' && payload.plugins && typeof payload.plugins === 'object') {
        Object.values(payload.plugins).forEach(plugin => {
          if (plugin && typeof plugin === 'object') {
            delete plugin.settingsConfigured
            delete plugin.secretStatus
          }
        })
      }
      return payload
    } catch (error) {
      throw new Error('配置内容不可用，请刷新页面后重试')
    }
  }

  const buildRequest = () => {
    const secretChanges = {}
    const value = secretValue.value
    if (value && selectedSection.value === 'base' && secretField.value) {
      secretChanges['database.password'] = { operation: 'replace', value: { plaintext: value } }
    }
    const relayPassword = formConfig.value?.relay?.password
    if (relayPassword && selectedSection.value === 'mail' && !formConfig.value?.relay?.clear) {
      secretChanges['relay.password'] = { operation: 'replace', value: { plaintext: relayPassword } }
    }
    if (selectedSection.value === 'ssl' && formConfig.value?.tls?.dnsProvider === 'alidns') {
      for (const name of ['dnsAccessKey', 'dnsSecretKey']) {
        const input = formConfig.value.tls[`${name}Input`]
        if (input) secretChanges[`tls.${name}`] = { operation: 'replace', value: { plaintext: input } }
      }
    }
    return {
      expectedGeneration: Number(status.activeGeneration),
      payload: parsePayload(),
      secretChanges,
      confirmation: { confirmed: true, method: 'recent_auth' }
    }
  }

  const checkForm = () => {
    formErrors.value = validateFormConfig(selectedSection.value, formConfig.value)
    const issues = Object.entries(formErrors.value).map(([code, message]) => ({ code, message }))
    if (!issues.length) return true
    validationResult.value = { valid: false, issues }
    ElMessage.warning('请先检查表单中标红的字段')
    return false
  }

  const validateConfig = async () => {
    if (!canEdit() || !checkForm()) return
    working.value = 'validate'
    try {
      const request = buildRequest()
      const response = await http.post(`/api/v1/config/${apiSection()}/validate`, request)
      notifyResponseError(response, '配置验证失败')
      validationResult.value = response.data || null
      ElMessage[validationResult.value?.valid ? 'success' : 'warning'](validationResult.value?.valid ? '配置验证通过' : '配置验证未通过')
    } catch (error) {
      ElMessage.error(error.message || '配置验证失败')
    } finally {
      working.value = ''
    }
  }

  const saveConfig = async ({ quiet = false } = {}) => {
    if (!canEdit() || !checkForm()) return false
    saveError.value = ''
    working.value = 'save'
    try {
      const request = buildRequest()
      const response = await requestWithRecentAuthentication(
        () => http.put(`/api/v1/config/${apiSection()}`, request)
      )
      notifyResponseError(response, '配置保存失败')
      savedGeneration.value = response.data?.generation || 0
      pendingCandidate.value = { ...response.data, section: apiSection(), sameSession: true }
      savedInput = { text: editorText.value, secret: secretValue.value }
      validationResult.value = { valid: true, generation: savedGeneration.value, issues: [] }
      await loadStatus()
      await loadPending()
      if (!quiet) ElMessage.success('配置已暂存，正在完成保存')
      return true
    } catch (error) {
      if (isRecentAuthenticationCancelled(error)) return false
      saveError.value = error.message || '配置保存失败'
      ElMessage.error(saveError.value)
      return false
    } finally {
      working.value = ''
    }
  }

  const applyConfig = async ({ successMessage = '配置已保存' } = {}) => {
    if (!canOperate() || !savedGeneration.value) return false
    saveError.value = ''
    if (!savedInput || savedInput.text !== editorText.value || savedInput.secret !== secretValue.value) {
      invalidateCandidate()
      ElMessage.warning('填写内容已变更，请重新保存配置')
      return false
    }
    working.value = 'apply'
    const generation = savedGeneration.value
    let applied = false
    try {
      const response = await requestWithRecentAuthentication(() => http.post(
        `/api/v1/config/${apiSection()}/apply`,
        {
          generation: savedGeneration.value,
          candidateId: pendingCandidate.value?.candidateId
        }
      ))
      notifyResponseError(response, '配置保存失败')
      applied = true
      // 服务端已提交的候选不能再次应用；回读失败时只允许刷新，不误报保存失败或展示旧配置。
      pendingCandidate.value = null
      invalidateCandidate()
      await loadStatus()
      if (disposed) return false
      if (!Number.isSafeInteger(Number(status.activeGeneration)) || Number(status.activeGeneration) < generation) {
        throw new Error('配置状态尚未更新')
      }
      if (!await loadSection({ minimumGeneration: generation })) throw new Error('最新配置读取失败')
      await loadPending()
      if (disposed) return false
      ElMessage.success(successMessage)
      return true
    } catch (error) {
      if (disposed) return false
      if (isRecentAuthenticationCancelled(error)) return false
      if (applied) {
        sectionError.value = '配置已应用，但最新配置读取失败，请刷新状态核对'
        editorText.value = ''
        loadedText.value = ''
        ElMessage.warning(sectionError.value)
        return false
      }
      saveError.value = error.message || '配置保存失败'
      ElMessage.error(saveError.value)
      return false
    } finally {
      working.value = ''
    }
  }

  const saveAndApply = async () => {
    if (saveFlow.value || !canOperate()) return false
    if (pluginSecretPage.value && secretValue.value) {
      ElMessage.warning('请先保存密钥，再保存运行配置')
      return false
    }
    saveFlow.value = pendingCandidate.value ? 'resume' : 'save'
    try {
      if (pendingCandidate.value) {
        if (!pendingCandidate.value.sameSession && !await managePending('resume')) return false
        return await applyConfig()
      }
      const saved = await saveConfig({ quiet: true })
      return saved ? await applyConfig({ successMessage: '配置已保存' }) : false
    } finally {
      saveFlow.value = ''
    }
  }

  const continuePending = async () => {
    if (!pendingCandidate.value) return false
    return saveAndApply()
  }

  const savePluginSecret = async () => {
    if (saveFlow.value || !canEdit() || !pluginSecretPage.value || !pluginSecretStatus.value || !secretValue.value) return false
    working.value = 'plugin-secret'
    saveError.value = ''
    try {
      const request = { version: pluginSecretStatus.value.version, change: { operation: 'replace', value: { plaintext: secretValue.value } } }
      const response = await requestWithRecentAuthentication(() => http.put(`/api/v1/plugin/secrets/${selectedSection.value}`, request))
      notifyResponseError(response, '插件密钥保存失败')
      if (disposed) return false
      if (typeof response.data?.configured !== 'boolean' || !Number.isSafeInteger(response.data.version) || response.data.version < 0) {
        throw new Error('插件密钥保存结果不完整，请刷新后核对')
      }
      secretValue.value = ''
      pluginSecretStatus.value = response.data
      ElMessage.success('密钥已保存，下次启动插件时生效')
      return true
    } catch (error) {
      if (!disposed && !isRecentAuthenticationCancelled(error)) {
        saveError.value = error.message || '插件密钥保存失败'
        ElMessage.error(saveError.value)
      }
      return false
    } finally {
      working.value = ''
    }
  }

  const previewRollback = async () => {
    if (!canRollback.value || working.value || !rollbackGeneration.value) return
    working.value = 'preview'
    rollbackPreview.value = null
    rollbackConfirmed.value = false
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
    if (!canRollback.value || working.value || !rollbackPreview.value || !rollbackConfirmed.value) return
    working.value = 'rollback-confirm'
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
      await Promise.all([loadStatus(), loadBackups()])
      ElMessage.success('配置已回滚')
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || '配置回滚失败')
    } finally {
      working.value = ''
    }
  }

  const beforeLeave = async () => {
    if (busy.value) {
      ElMessage.warning('当前操作尚未完成，请稍后再切换页面')
      return false
    }
    if (!hasChanges.value && !migrationToken.value && !rollbackPreview.value) return true
    try {
      await ElMessageBox.confirm(
        '当前页有编辑内容、未完成的保存或操作预览。离开会清除本页状态，但不会撤销已暂存到服务器的更改。确认离开吗？',
        '确认离开当前页',
        { type: 'warning', confirmButtonText: '离开页面', cancelButtonText: '继续编辑' }
      )
      return !busy.value
    } catch {
      return false
    }
  }

  const refreshPage = async () => {
    if (await beforeLeave()) await loadAll()
  }

  onMounted(loadAll)
  onBeforeUnmount(() => {
    disposed = true
    sectionRequestId++
    secretValue.value = ''
    savedInput = null
    confirmed.value = false
    migrationToken.value = ''
    migrationTarget.password = ''
    migrationInput = ''
  })

  return {
    pendingScopeHint,
    saveFlow, busy, showPendingRecovery, continuingSave,
    loading, availabilityError, working, status, stateLabel, stateTagType, sectionOptions: sections,
    selectedSection, currentSection, sectionLoading, editorText, formConfig, formErrors, updateField, updateSecret,
    sectionError, saveError, sensitiveHint, secretField, secretValue, pluginSecretPage, pluginSecretStatus, savePluginSecret, hasConfigChanges,
    confirmed, savedGeneration, pendingCandidate, managePending, validationResult, canWrite, canRollback,
    backupsLoading, backupsError, backups, rollbackGeneration, rollbackConfirmed, rollbackPreview,
    loadAll, loadSection, validateConfig, saveConfig, applyConfig, saveAndApply, continuePending,
    loadBackups, previewRollback, executeRollback, formatTime,
    serviceLabel, sectionLabel, databaseConfig, databaseTypeLabel, canMigrate,
    migrationTargetName, migrationPreview, migrationToken, migrationRecord, migrationStatusLabel, migrationStatusType,
    migrationTarget, migrationReady, migrationError, invalidateMigration, changeMigrationType,
    previewDatabaseMigration, loadMigrationStatus, executeDatabaseMigration, canRestart, restartPreview,
    previewRestart, requestRestart, beforeLeave, refreshPage, hasChanges
  }
}
