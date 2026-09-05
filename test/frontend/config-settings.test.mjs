import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigSettings.js', import.meta.url), 'utf8')
const createPageWithKey = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage(pageKey)',
  ['computed', 'onMounted', 'reactive', 'ref', 'ElMessage', 'ElMessageBox', 'http', 'useGlobalStatusStore', 'pageKey', 'onBeforeUnmount']
)
const createPage = (...args) => createPageWithKey(...args, 'base', () => {})

function fixture(get, pageKey = 'base', options = {}) {
  const messages = []
  let dispose = () => {}
  const page = createPageWithKey(computed, () => {}, reactive, ref,
    { error: message => messages.push(message), warning: message => messages.push(message), success: () => {} },
    { confirm: options.confirm || (async () => {}) }, { get, ...options.http },
    () => ({ hasCapability: options.hasCapability || (() => true) }), pageKey, callback => { dispose = callback })
  return { page, messages, dispose: () => dispose() }
}

function deferred() {
  let resolve
  let reject
  const promise = new Promise((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

test('旧版配置仅请求状态，不并发请求分区、备份和审计', async () => {
  const requests = []
  const { page, messages } = fixture(async path => {
    requests.push(path)
    return { errorNo: 0, data: { available: false, unavailableReason: '在线配置管理尚未启用' } }
  })
  await page.loadAll()
  assert.deepEqual(requests, ['/api/v1/config/status'])
  assert.equal(page.availabilityError.value, '在线配置管理尚未启用')
  assert.deepEqual(messages, [])
  assert.equal(page.loading.value, false)
})

test('配置页不加载备份或审计，维护页失败只显示本页错误', async () => {
  const requests = []
  const get = async path => {
    requests.push(path)
    if (path.endsWith('/status')) return { errorNo: 0, data: { available: true } }
    if (path.endsWith('/base')) return { errorNo: 0, data: {} }
    throw new Error('配置服务暂时不可用')
  }
  const { page, messages } = fixture(get)
  await page.loadAll()
  assert.equal(page.availabilityError.value, '')
  assert.equal(page.editorText.value, '{}')
  assert.deepEqual(requests, ['/api/v1/config/status', '/api/v1/config/base'])
  const backup = fixture(get, 'backups').page
  const audit = fixture(get, 'audit').page
  await Promise.all([backup.loadAll(), audit.loadAll()])
  assert.equal(backup.backupsError.value, '配置服务暂时不可用')
  assert.equal(audit.auditError.value, '配置服务暂时不可用')
  assert.equal(backup.editorText.value, '')
  assert.equal(audit.editorText.value, '')
  assert.deepEqual(messages, [])
  await page.loadBackups()
  assert.deepEqual(messages, [])
  assert.equal(page.backupsError.value, '配置服务暂时不可用')
})

test('附属模块失败清除旧数据和回滚选择，重试成功清除错误', async () => {
  let fail = true
  const { page } = fixture(async () => {
    if (fail) throw new Error('网络不可用')
    return { errorNo: 0, data: [{ generation: 2 }] }
  })
  page.backups.value = [{ generation: 1 }]
  page.auditItems.value = [{ id: 'old' }]
  page.rollbackGeneration.value = 1
  page.rollbackPreview.value = { targetGeneration: 1 }
  page.rollbackConfirmed.value = true
  await Promise.all([page.loadBackups(), page.loadAudit()])
  assert.deepEqual(page.backups.value, [])
  assert.deepEqual(page.auditItems.value, [])
  assert.equal(page.rollbackGeneration.value, 0)
  assert.equal(page.rollbackPreview.value, null)
  assert.equal(page.rollbackConfirmed.value, false)
  fail = false
  await Promise.all([page.loadBackups(), page.loadAudit()])
  assert.equal(page.backupsError.value, '')
  assert.equal(page.auditError.value, '')
  assert.equal(page.backups.value.length, 1)
  assert.equal(page.auditItems.value.length, 1)
  assert.equal(page.backupsLoading.value, false)
  assert.equal(page.auditLoading.value, false)
})

test('状态请求失败后可重试恢复，不保留不可用提示', async () => {
  let available = false
  const { page } = fixture(async path => {
    if (!available) throw new Error('网络不可用')
    return { errorNo: 0, data: path.endsWith('/status') ? { available: true } : {} }
  })
  await page.loadAll()
  assert.equal(page.availabilityError.value, '网络不可用')
  available = true
  await page.loadAll()
  assert.equal(page.availabilityError.value, '')
})

test('独立页互不覆盖，旧页卸载时清除密码并丢弃较慢响应', async () => {
  const base = deferred()
  const email = deferred()
  const { page, dispose } = fixture(() => base.promise)
  page.editorText.value = '{"old":true}'
  page.confirmed.value = true
  page.secretValue.value = 'test-only-value'
  const baseRequest = page.loadSection()
  assert.equal(page.editorText.value, '')
  assert.equal(page.confirmed.value, false)
  assert.equal(page.secretValue.value, '')
  assert.equal(page.sectionLoading.value, true)
  page.secretValue.value = 'test-only-value'
  dispose()
  assert.equal(page.secretValue.value, '')
  const emailPage = fixture(() => email.promise, 'email').page
  const emailRequest = emailPage.loadSection()
  email.resolve({ errorNo: 0, data: { policy: { smtpPort: 25 } } })
  await emailRequest
  assert.equal(emailPage.currentSection.value.label, '邮箱策略')
  assert.equal(emailPage.sectionLoading.value, false)
  base.resolve({ errorNo: 0, data: { database: { type: 'sqlite' } } })
  await baseRequest
  assert.deepEqual(JSON.parse(emailPage.editorText.value), { policy: { smtpPort: 25 } })
  assert.equal(page.editorText.value, '')
  assert.equal(page.databaseConfig.type, '')
})

test('同一页重复加载时，过期失败不覆盖最新请求', async () => {
  const base = deferred()
  const ssl = deferred()
  let count = 0
  const { page } = fixture(() => ++count === 1 ? base.promise : ssl.promise, 'ssl')
  const baseRequest = page.loadSection()
  const sslRequest = page.loadSection()
  base.reject(new Error('旧分区读取失败'))
  await baseRequest
  assert.equal(page.sectionLoading.value, true)
  assert.equal(page.sectionError.value, '')
  ssl.resolve({ errorNo: 0, data: { enabled: true } })
  await sslRequest
  assert.equal(page.sectionLoading.value, false)
  assert.equal(page.sectionError.value, '')
  assert.deepEqual(JSON.parse(page.editorText.value), { enabled: true })
})

test('五个配置页固定分区并按需加载，所有分区均提供中文说明', async () => {
  const requests = []
  for (const key of ['base', 'smtp', 'email', 'ssl', 'plugins']) {
    const { page } = fixture(async path => { requests.push(path); return { errorNo: 0, data: {} } }, key)
    await page.loadAll()
    assert.equal(page.selectedSection.value, key)
    assert.match(page.currentSection.value.description, /[\u4e00-\u9fff]/)
    assert.equal(page.secretValue.value, '')
    assert.equal(page.confirmed.value, false)
    assert.equal(page.savedGeneration.value, 0)
  }
  assert.deepEqual(requests, ['base', 'smtp', 'email', 'ssl', 'plugins']
    .flatMap(key => ['/api/v1/config/status', `/api/v1/config/${key}`]))
})

test('迁移预览票据返回模板，迁移执行取消时不发送写入请求', async () => {
  const requests = []
  const page = createPage(computed, () => {}, reactive, ref,
    { success: () => {}, error: () => assert.fail('不应显示错误') },
    { confirm: async () => { throw 'cancel' } },
    { post: async (path, payload) => {
      requests.push({ path, payload })
      return { errorNo: 0, data: { migration: { id: 'test-migration', status: 'previewed' }, token: 'test-only-ticket' } }
    } }, () => ({ hasCapability: () => true }))
  page.status.activeGeneration = 3
  page.migrationTargetName.value = ' test-next.db '
  assert.equal(page.migrationToken.value, '')
  await page.previewDatabaseMigration()
  assert.equal(page.migrationToken.value, 'test-only-ticket')
  assert.equal(page.migrationPreview.value.id, 'test-migration')
  await page.executeDatabaseMigration()
  assert.deepEqual(requests, [{
    path: '/api/v1/database/migrations/preview',
    payload: { targetName: 'test-next.db', expectedGeneration: 3 }
  }])
})

test('独立页模板可编译，编辑器不再连接维护面板且权限绑定保留', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
  const template = readFileSync(new URL('ConfigEditor/ConfigEditor.vue', root), 'utf8')
  const maintenance = readFileSync(new URL('ConfigMaintenance/ConfigMaintenance.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigSettings.css', root), 'utf8')
  const { descriptor, errors } = parse(template)
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'config-layout', inlineTemplate: true }))
  assert.deepEqual(compileStyle({ source: css, id: 'data-v-config-layout', scoped: true }).errors, [])
  assert.match(template, /v-if="canWrite" class="editor-footer"/)
  assert.match(template, /v-model="confirmed"/)
  assert.doesNotMatch(template, /maintenance-panel|migration-panel|rollback-panel|audit-panel/)
  for (const [key, capability] of [['migration', 'canMigrate'], ['restart', 'canRestart'], ['backups', 'canRollback'], ['audit', 'canAudit']]) {
    assert.ok(maintenance.includes(`pageKey === '${key}' && ${capability}`))
  }
  assert.match(maintenance, /v-if="canExportAudit"/)
  for (const name of ['BaseConfig', 'SmtpConfig', 'EmailConfig', 'SslConfig', 'PluginsConfig', 'ConfigMaintenance']) {
    const page = readFileSync(new URL(`${name}/${name}.vue`, root), 'utf8')
    const { descriptor, errors } = parse(page)
    assert.deepEqual(errors, [])
    assert.doesNotThrow(() => compileScript(descriptor, { id: name, inlineTemplate: true }))
  }
  assert.match(css, /@container config-main \(max-width: 620px\)/)
  assert.match(css, /\.maintenance-summary:focus-visible/)
  assert.doesNotMatch(template, /&#x[0-9a-f]+;/i)
})

test('离开、刷新和正在执行的变更受保护，取消时保留编辑内容', async () => {
  let cancel = true
  let prompts = 0
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }), 'smtp', {
    confirm: async () => { prompts++; if (cancel) throw 'cancel' }
  })
  await page.loadAll()
  assert.equal(await page.beforeLeave(), true)
  assert.equal(prompts, 0)
  page.editorText.value = '{"test":true}'
  assert.equal(await page.beforeLeave(), false)
  await page.refreshPage()
  assert.equal(page.editorText.value, '{"test":true}')
  cancel = false
  assert.equal(await page.beforeLeave(), true)
  page.working.value = 'save'
  assert.equal(await page.beforeLeave(), false)
  page.working.value = ''
  await page.refreshPage()
  assert.equal(page.hasChanges.value, false)
  page.savedGeneration.value = 3
  cancel = true
  assert.equal(await page.beforeLeave(), false)
})

test('卸载发生在状态请求期间，不再发送后续分区请求', async () => {
  const pending = deferred()
  const requests = []
  const { page, dispose } = fixture(path => { requests.push(path); return pending.promise })
  const loading = page.loadAll()
  dispose()
  pending.resolve({ errorNo: 0, data: { available: true } })
  await loading
  assert.deepEqual(requests, ['/api/v1/config/status'])
})

test('只读账号不得发送配置写入请求或读取无权限维护数据', async () => {
  const requests = []
  const { page } = fixture(async path => { requests.push(path); return { errorNo: 0, data: {} } }, 'audit', {
    hasCapability: capability => capability === 'config.read'
  })
  await page.loadAll()
  await page.validateConfig()
  await page.saveConfig()
  page.savedGeneration.value = 3
  await page.applyConfig()
  assert.deepEqual(requests, ['/api/v1/config/status'])
})

test('独立页保持验证、保存、应用端点和代次协议，敏感值不串页', async () => {
  const requests = []
  const { page } = fixture(async path => ({ errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: 8 } : { relayPasswordStatus: 'configured' } }), 'smtp', {
    http: {
      post: async (path, body) => { requests.push({ path, body }); return { errorNo: 0, data: { valid: true } } },
      put: async (path, body) => { requests.push({ path, body }); return { errorNo: 0, data: { generation: 9 } } }
    }
  })
  await page.loadAll()
  page.secretValue.value = 'test-only-value'
  page.confirmed.value = true
  await page.validateConfig()
  await page.saveConfig()
  assert.equal(page.savedGeneration.value, 9)
  await page.applyConfig()
  assert.deepEqual(requests.map(item => item.path), ['/api/v1/config/smtp/validate', '/api/v1/config/smtp', '/api/v1/config/smtp/apply'])
  assert.equal(requests[0].body.expectedGeneration, 8)
  assert.deepEqual(requests[0].body.payload, {})
  assert.deepEqual(requests[0].body.secretChanges, { 'relay.password': { operation: 'replace', value: { plaintext: 'test-only-value' } } })
  assert.deepEqual(requests[0].body.confirmation, { confirmed: true, method: 'recent_auth' })
  assert.deepEqual(requests[2].body, { generation: 9 })
  assert.equal(page.secretValue.value, '')
  assert.equal(page.confirmed.value, false)
  assert.equal(page.savedGeneration.value, 0)
})
