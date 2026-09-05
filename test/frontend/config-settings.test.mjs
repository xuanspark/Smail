import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigSettings.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'onMounted', 'reactive', 'ref', 'ElMessage', 'ElMessageBox', 'http', 'useGlobalStatusStore']
)

function fixture(get) {
  const messages = []
  const page = createPage(computed, () => {}, reactive, ref,
    { error: message => messages.push(message) }, {}, { get }, () => ({ hasCapability: () => true }))
  return { page, messages }
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

test('备份和审计失败只显示局部错误，不阻挡配置编辑区域', async () => {
  const { page, messages } = fixture(async path => {
    if (path.endsWith('/status')) return { errorNo: 0, data: { available: true } }
    if (path.endsWith('/base')) return { errorNo: 0, data: {} }
    throw new Error('配置服务暂时不可用')
  })
  await page.loadAll()
  assert.equal(page.availabilityError.value, '')
  assert.equal(page.editorText.value, '{}')
  assert.equal(page.backupsError.value, '配置服务暂时不可用')
  assert.equal(page.auditError.value, '配置服务暂时不可用')
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

test('快速切换分区时丢弃较慢的旧响应，并清除旧编辑内容与确认状态', async () => {
  const base = deferred()
  const email = deferred()
  const { page } = fixture(path => path.endsWith('/base') ? base.promise : email.promise)
  page.editorText.value = '{"old":true}'
  page.confirmed.value = true
  page.secretValue.value = 'test-only-value'
  const baseRequest = page.loadSection()
  assert.equal(page.editorText.value, '')
  assert.equal(page.confirmed.value, false)
  assert.equal(page.secretValue.value, '')
  assert.equal(page.sectionLoading.value, true)
  const emailRequest = page.selectSection('email')
  email.resolve({ errorNo: 0, data: { policy: { smtpPort: 25 } } })
  await emailRequest
  assert.equal(page.currentSection.value.label, '邮箱策略')
  assert.equal(page.sectionLoading.value, false)
  base.resolve({ errorNo: 0, data: { database: { type: 'sqlite' } } })
  await baseRequest
  assert.deepEqual(JSON.parse(page.editorText.value), { policy: { smtpPort: 25 } })
  assert.equal(page.databaseConfig.type, '')
})

test('过期请求失败不清除新分区的加载状态或覆盖错误信息', async () => {
  const base = deferred()
  const ssl = deferred()
  const { page } = fixture(path => path.endsWith('/base') ? base.promise : ssl.promise)
  const baseRequest = page.loadSection()
  const sslRequest = page.selectSection('ssl')
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

test('变更执行期间或分区值无效时不切换，所有分区均提供中文说明', async () => {
  const requests = []
  const { page } = fixture(async path => { requests.push(path); return { errorNo: 0, data: {} } })
  page.working.value = 'save'
  await page.selectSection('smtp')
  assert.equal(page.selectedSection.value, 'base')
  page.working.value = ''
  await page.selectSection('invalid-section')
  await page.selectSection('base')
  assert.deepEqual(requests, [])
  for (const section of page.sectionOptions) {
    assert.match(section.description, /[\u4e00-\u9fff]/)
  }
  await page.selectSection('plugins')
  assert.deepEqual(requests, ['/api/v1/config/plugins'])
  assert.equal(page.currentSection.value.label, '插件配置')
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

test('配置模板保留权限与确认绑定，维护操作使用原生可访问折叠面板', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
  const template = readFileSync(new URL('ConfigSettings.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigSettings.css', root), 'utf8')
  const { descriptor, errors } = parse(template)
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'config-layout', inlineTemplate: true }))
  assert.deepEqual(compileStyle({ source: css, id: 'data-v-config-layout', scoped: true }).errors, [])
  assert.match(template, /v-if="canWrite" class="editor-footer"/)
  assert.match(template, /v-model="confirmed"/)
  assert.match(template, /<details v-if="canMigrate && selectedSection === 'base'"/)
  assert.match(template, /<details v-if="canRestart && status.pendingRestart\?\.length"[^>]+ open>/)
  assert.match(template, /<details v-if="canRollback"/)
  assert.match(template, /<details v-if="canAudit"/)
  assert.equal((template.match(/<summary class="maintenance-summary">/g) || []).length, 4)
  assert.match(css, /@container config-main \(max-width: 620px\)/)
  assert.match(css, /\.maintenance-summary:focus-visible/)
  assert.doesNotMatch(template, /&#x[0-9a-f]+;/i)
})
