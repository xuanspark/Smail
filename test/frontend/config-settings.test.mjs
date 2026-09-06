import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, reactive, ref } from './vue.mjs'

const formSource = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigForm/ConfigForm.js', import.meta.url), 'utf8')
const formHelpers = compileFunction(formSource.replaceAll('export function ', 'function ') + '\nreturn { readFormConfig, updateFormConfig, validateFormConfig }')()
const source = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigSettings.js', import.meta.url), 'utf8')
const compilePage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage(pageKey)',
  ['computed', 'onMounted', 'reactive', 'ref', 'ElMessage', 'ElMessageBox', 'http', 'useGlobalStatusStore', 'pageKey', 'onBeforeUnmount', 'readFormConfig', 'updateFormConfig', 'validateFormConfig']
)
const createPageWithKey = (...args) => compilePage(...args, formHelpers.readFormConfig, formHelpers.updateFormConfig, formHelpers.validateFormConfig)
const createPage = (...args) => createPageWithKey(...args, 'base', () => {})

function fixture(get, pageKey = 'base', options = {}) {
  const messages = []
  let dispose = () => {}
  const page = createPageWithKey(computed, () => {}, reactive, ref,
    { error: message => messages.push(message), warning: message => messages.push(message), success: () => {} },
    { confirm: options.confirm || (async () => {}) }, {
      get: path => path.endsWith('/pending') && !options.pendingGet ? Promise.resolve({ errorNo: 0, data: null }) : get(path),
      ...options.http
    },
    () => ({ hasCapability: options.hasCapability || (() => true) }), pageKey, callback => { dispose = callback })
  return { page, messages, dispose: () => dispose() }
}

function deferred() {
  let resolve
  let reject
  const promise = new Promise((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

test('证书页从状态接口读取生效反代模式，不额外加载基础配置', async () => {
  const requests = []
  const { page } = fixture(async path => {
    requests.push(path)
    return { errorNo: 0, data: path.endsWith('/status') ? { available: true, tlsMode: 'proxy', activeGeneration: 2 } : { tls: { mode: 'managed_upload' } } }
  }, 'ssl')
  await page.loadAll()
  assert.equal(page.status.tlsMode, 'proxy')
  assert.deepEqual(requests, ['/api/v1/config/status', '/api/v1/config/ssl'])
  const template = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigEditor/ConfigEditor.vue', import.meta.url), 'utf8')
  assert.match(template, /v-if="selectedSection === 'ssl' && status.tlsMode === 'proxy'"/)
  assert.match(template, /Smail 不自动申请或续期 SSL 证书/)
})

for (const failedRequest of ['/status', '/pending']) {
  test(`候选保存成功后 ${failedRequest} 读取失败不能重新保存，刷新后可恢复或取消`, async () => {
    let pending = null
    let fail = false
    let saves = 0
    const { page } = fixture(() => {}, 'smtp', { http: {
      get: async path => {
        if (fail && path.endsWith(failedRequest)) throw new Error('模拟读取失败')
        return { errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: 1 } : { relay: { relayHost: 'old.example.com' } } }
      },
      put: async (path, body) => {
        saves++
        pending = { generation: 2, candidateId: 'pending-recovery', section: 'smtp', sameSession: true, payload: body.payload }
        fail = true
        return { errorNo: 0, data: pending }
      },
      post: async (path, body) => {
        assert.equal(body.candidateId, 'pending-recovery')
        if (body.action === 'resume') pending.sameSession = true
        if (body.action === 'cancel') pending = null
        return { errorNo: 0 }
      }
    } })
    await page.loadAll()
    page.updateField(['relay', 'relayHost'], 'new.example.com')
    await page.saveConfig()
    assert.equal(saves, 1)
    page.updateField(['relay', 'relayHost'], 'blocked.example.com')
    await page.saveConfig()
    assert.equal(saves, 1)
    assert.equal(page.formConfig.value.relay.relayHost, 'new.example.com')
    fail = false
    pending.sameSession = false
    await page.refreshPage()
    assert.equal(page.savedGeneration.value, 0)
    assert.equal(page.pendingCandidate.value.candidateId, 'pending-recovery')
    await page.managePending('resume')
    assert.equal(page.savedGeneration.value, 2)
    await page.managePending('cancel')
    assert.equal(page.pendingCandidate.value, null)
    page.updateField(['relay', 'relayHost'], 'editable.example.com')
    assert.equal(page.formConfig.value.relay.relayHost, 'editable.example.com')
  })
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

test('七个配置页按需加载，协议页复用正确分区且提供中文说明', async () => {
  const requests = []
  for (const key of ['base', 'smtp', 'imap', 'pop3', 'email', 'ssl', 'plugins']) {
    const { page } = fixture(async path => { requests.push(path); return { errorNo: 0, data: {} } }, key)
    await page.loadAll()
    assert.equal(page.selectedSection.value, key)
    assert.match(page.currentSection.value.description, /[\u4e00-\u9fff]/)
    assert.equal(page.secretValue.value, '')
    assert.equal(page.confirmed.value, false)
    assert.equal(page.savedGeneration.value, 0)
  }
  assert.deepEqual(requests, ['base', 'smtp', 'imap', 'pop3', 'email', 'ssl', 'plugins']
    .flatMap(key => ['/api/v1/config/status', `/api/v1/config/${['imap', 'pop3'].includes(key) ? 'email' : key}`, ...(key === 'email' ? ['/api/v1/config/smtp'] : [])]))
})

for (const key of ['imap', 'pop3']) {
  test(`${key} 页面验证、保存、候选恢复及应用均使用邮箱分区且保留其他字段`, async () => {
    const requests = []
    let pending = null
    let active = { meta: { schema: 1, generation: 1 }, policy: { maxMessageBytes: 1234 }, imap: { tlsPort: 993 }, pop3: { port: 110, tlsPort: 995 } }
    const { page } = fixture(() => {}, key, { http: {
      get: async path => ({ errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : structuredClone(active) }),
      put: async (path, body) => {
        requests.push(path)
        assert.equal(body.expectedGeneration, 1)
        assert.deepEqual(body.payload.policy, active.policy)
        assert.deepEqual(body.payload[key === 'imap' ? 'pop3' : 'imap'], active[key === 'imap' ? 'pop3' : 'imap'])
        pending = { generation: 2, candidateId: 'mail-candidate', section: 'email', sameSession: true, payload: body.payload }
        return { errorNo: 0, data: pending }
      },
      post: async (path, body) => {
        requests.push(path)
        if (path.endsWith('/apply')) {
          assert.equal(body.candidateId, 'mail-candidate')
          active = { ...pending.payload, meta: { schema: 1, generation: 2 } }
          pending = null
        }
        return { errorNo: 0, data: { valid: true } }
      }
    } })
    await page.loadAll()
    page.updateField([key, 'tlsPort'], key === 'imap' ? 1993 : 1995)
    await page.validateConfig()
    await page.saveConfig()
    await page.refreshPage()
    assert.equal(page.savedGeneration.value, 2)
    page.updateField([key, 'tlsPort'], 8888)
    assert.notEqual(page.formConfig.value[key].tlsPort, 8888)
    await page.applyConfig()
    assert.equal(page.status.activeGeneration, 2)
    assert.equal(page.savedGeneration.value, 0)
    assert.deepEqual(requests, ['/api/v1/config/email/validate', '/api/v1/config/email', '/api/v1/config/email/apply'])
  })
}

test('邮箱策略从三种协议配置汇总已应用端口，不能修改或被候选值误导', async () => {
  const config = { meta: { generation: 3 }, policy: { smtpPort: 25, imapPort: 143, pop3Port: 110 }, imap: { tlsPort: 1993 }, pop3: { port: 1110, tlsPort: 1995 } }
  const { page } = fixture(() => {}, 'email', { http: { get: async path => ({ errorNo: 0, data:
    path.endsWith('/status') ? { activeGeneration: 3 } : path.endsWith('/smtp') ? { meta: { generation: 3 }, listener: { port: 2525, starttlsPort: 2587, tlsPort: 2465 }, relay: { relayPort: 9999 } } :
      path.endsWith('/pending') ? { section: 'email', generation: 4, sameSession: true, payload: { ...config, imap: { tlsPort: 2993 } } } : config
  }) } })
  await page.loadAll()
  assert.deepEqual(page.mailPortSummary.value.flatMap(service => service.ports.map(port => port.value)), [2525, 2587, 2465, 1993, 1110, 1995])
  assert.equal(page.formConfig.value.imap.tlsPort, 2993)
  const before = page.editorText.value
  for (const path of [['policy', 'smtpPort'], ['policy', 'imapPort'], ['imap', 'tlsPort']]) page.updateField(path, 8888)
  assert.equal(page.editorText.value, before)
})

test('邮箱策略拒绝不同代次的端口汇总，后续加载失败清除旧汇总', async () => {
  let generation = 2
  const { page } = fixture(async path => ({ errorNo: 0, data: path.endsWith('/smtp')
    ? { meta: { generation }, listener: { port: 2525 } }
    : { meta: { generation: 1 }, imap: { tlsPort: 1993 }, pop3: { port: 1110, tlsPort: 1995 } } }), 'email')
  await page.loadSection()
  assert.match(page.sectionError.value, /代次/)
  assert.equal(page.mailPortSummary.value, null)
  generation = 1
  await page.loadSection()
  assert.ok(page.mailPortSummary.value)
  generation = 3
  await page.loadSection()
  assert.equal(page.mailPortSummary.value, null)
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
    payload: { target: { type: 'sqlite', localRef: 'test-next.db' }, expectedGeneration: 3 }
  }])
})

test('三种数据库使用独立目标字段，类型切换清除密码及旧预览', async () => {
  const requests = []
  const { page, dispose } = fixture(async () => ({ errorNo: 0, data: {} }), 'migration', {
    http: { post: async (path, payload) => {
      requests.push({ path, payload })
      return { errorNo: 0, data: { migration: { id: 'next', target: payload.target }, token: 'ticket' } }
    } }
  })
  for (const type of ['mysql', 'postgresql']) {
    page.changeMigrationType(type)
    assert.equal(page.migrationTarget.port, type === 'mysql' ? 3306 : 5432)
    assert.equal(page.migrationReady.value, false)
    page.migrationTargetName.value = 'smail_next'
    Object.assign(page.migrationTarget, { host: ' db.local ', username: ' smail ', password: ' test-secret ' })
    assert.equal(page.migrationReady.value, true)
    await page.previewDatabaseMigration()
    assert.equal(requests.at(-1).payload.target.type, type)
    assert.equal(requests.at(-1).payload.target.host, 'db.local')
    assert.equal(requests.at(-1).payload.target.password.plaintext, ' test-secret ')
    page.migrationTarget.host = 'other.local'
    await page.executeDatabaseMigration()
    assert.equal(page.migrationToken.value, '')
    assert.match(page.migrationError.value, /重新生成/)
  }
  assert.equal(requests.length, 2)
  page.changeMigrationType('sqlite')
  assert.equal(page.migrationTarget.password, '')
  assert.equal(page.migrationPreview.value, null)
  page.migrationTargetName.value = '../bad.db'
  assert.equal(page.migrationReady.value, false)
  page.migrationTargetName.value = 'next.db'
  page.status.pendingRestart = ['database']
  assert.equal(page.migrationReady.value, false)
  page.migrationTarget.password = 'temporary'
  dispose()
  assert.equal(page.migrationTarget.password, '')
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
  for (const name of ['BaseConfig', 'SmtpConfig', 'ImapConfig', 'Pop3Config', 'EmailConfig', 'SslConfig', 'PluginsConfig', 'ConfigMaintenance', 'MailPortSummary']) {
    const page = readFileSync(new URL(`${name}/${name}.vue`, root), 'utf8')
    const { descriptor, errors } = parse(page)
    assert.deepEqual(errors, [])
    assert.doesNotThrow(() => compileScript(descriptor, { id: name, inlineTemplate: true }))
  }
  assert.match(css, /@container config-main \(max-width: 620px\)/)
  assert.match(css, /\.maintenance-summary:focus-visible/)
  assert.doesNotMatch(template, /&#x[0-9a-f]+;/i)
})

test('配置编辑页移除步骤提示条和专用样式，保留实际操作及确认控件', () => {
  const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
  const template = readFileSync(new URL('ConfigEditor/ConfigEditor.vue', root), 'utf8')
  const sharedCss = readFileSync(new URL('ConfigSettings.css', root), 'utf8')
  const editorCss = readFileSync(new URL('ConfigEditor/ConfigEditor.css', root), 'utf8')
  assert.doesNotMatch(template, /change-workflow|step-number|配置变更流程/)
  assert.doesNotMatch(sharedCss + editorCss, /change-workflow|step-number/)
  for (const action of ['validateConfig', 'saveConfig', 'applyConfig']) {
    assert.match(template, new RegExp(`<el-button\\b[^>]*@click="${action}"[^>]*>`))
  }
  assert.match(template, /v-if="canWrite" class="editor-footer"/)
  assert.match(template, /<el-checkbox\b[^>]*v-model="confirmed"/)
  assert.match(template, /@click="managePending\('resume'\)"/)
  assert.match(template, /@click="managePending\('cancel'\)"/)
})

test('配置编辑区移除填写标题、说明与重复标签，保留主标题和必要提示', () => {
  const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
  const template = readFileSync(new URL('ConfigEditor/ConfigEditor.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigSettings.css', root), 'utf8')
  assert.doesNotMatch(template, /'填写配置'|'查看配置'|按分组填写内容|当前页面的脱敏配置内容|panel-heading|selected-section-label/)
  assert.doesNotMatch(css, /selected-section-label/)
  assert.match(template, /<h3 id="config-page-title">{{ currentSection.label }}/)
  assert.match(template, /v-if="selectedSection === 'ssl' && status.tlsMode === 'proxy'"[^>]*role="note"/)
  assert.match(template, /v-if="sectionError"[\s\S]*?:title="sectionError"/)
  assert.match(template, /id="config-sensitive-hint"/)
  assert.match(template, /v-if="secretField" class="secret-editor"/)
  assert.match(template, /v-if="validationResult"[^>]*role="status"/)
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

test('标题合并配置状态，保留刷新保护和降级提示，不再渲染快照卡片', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  const root = new URL('../../frontend/src/views/components/ConfigSettings/ConfigEditor/', import.meta.url)
  const template = readFileSync(new URL('ConfigEditor.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigEditor.css', root), 'utf8')
  const { descriptor, errors } = parse(template)
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'config-overview', inlineTemplate: true }))
  assert.deepEqual(compileStyle({ source: css, id: 'data-v-config-overview', scoped: true }).errors, [])
  const header = template.match(/<header[\s\S]*?<\/header>/)?.[0] || ''
  assert.match(header, /aria-labelledby="config-page-title"/)
  assert.match(header, /<h3 id="config-page-title">{{ currentSection.label }}/)
  assert.match(header, /<span class="sr-only">运行状态：<\/span>{{ stateLabel }}/)
  assert.match(header, /v-for="issue in status.degradedReasons \|\| \[\]"/)
  assert.match(header, /v-if="!loading && !availabilityError"/)
  assert.match(header, /:disabled="!!working \|\| sectionLoading \|\| loading"/)
  assert.match(header, /@click="refreshPage"/)
  assert.doesNotMatch(header, /<dl class="overview-meta"/)
  assert.doesNotMatch(header, /access-tag/)
  assert.doesNotMatch(template, /配置快照|status-panel|status-grid/)
  assert.match(css, /@container config-main \(max-width: 620px\)/)
  assert.match(css, /overflow-wrap: anywhere/)
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
  let pending = null
  const { page } = fixture(async path => ({ errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: 8 } : { relayPasswordStatus: 'configured' } }), 'smtp', {
    http: {
      get: async path => ({ errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: 8 } : { relayPasswordStatus: 'configured' } }),
      post: async (path, body) => { requests.push({ path, body }); if (path.endsWith('/apply')) pending = null; return { errorNo: 0, data: { valid: true } } },
      put: async (path, body) => {
        requests.push({ path, body })
        pending = { generation: 9, candidateId: 'candidate-9', section: 'smtp', sameSession: true, payload: {} }
        return { errorNo: 0, data: pending }
      }
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
  assert.deepEqual(requests[2].body, { generation: 9, candidateId: 'candidate-9' })
  assert.equal(page.secretValue.value, '')
  assert.equal(page.confirmed.value, false)
  assert.equal(page.savedGeneration.value, 0)
})

test('可视化编辑按权限和工作状态保护，编辑会清除过期确认及候选代次', async () => {
  const { page, dispose } = fixture(async () => ({ errorNo: 0, data: { relay: { relayHost: 'smtp.example.com' } } }), 'smtp')
  await page.loadAll()
  const original = page.editorText.value
  for (const state of ['loading', 'sectionLoading', 'working', 'sectionError']) {
    page[state].value = true
    page.updateField(['relay', 'relayHost'], 'blocked.example.com')
    page.updateSecret('blocked-value')
    assert.equal(page.editorText.value, original)
    assert.equal(page.secretValue.value, '')
    page[state].value = false
  }
  const readonly = fixture(async () => ({ errorNo: 0, data: {} }), 'smtp', { hasCapability: () => false }).page
  readonly.updateField(['relay', 'relayHost'], 'blocked.example.com')
  assert.equal(readonly.editorText.value, '')
  page.savedGeneration.value = 8
  page.updateField(['relay', 'relayHost'], 'blocked.example.com')
  assert.equal(page.editorText.value, original)
  page.savedGeneration.value = 0
  page.confirmed.value = true
  page.validationResult.value = { valid: true }
  page.updateField(['relay', 'relayHost'], 'new.example.com')
  assert.equal(page.formConfig.value.relay.relayHost, 'new.example.com')
  assert.equal(page.savedGeneration.value, 0)
  assert.equal(page.confirmed.value, false)
  assert.equal(page.validationResult.value, null)
  dispose()
  page.updateField(['relay', 'relayHost'], 'blocked.example.com')
  assert.equal(page.formConfig.value.relay.relayHost, 'new.example.com')
})

test('前端无效字段阻止网络请求，修正字段后同步清除错误', async () => {
  const requests = []
  const { page } = fixture(async () => ({ errorNo: 0, data: { imap: { tlsPort: 993 } } }), 'imap', {
    http: { post: async path => { requests.push(path); return { errorNo: 0, data: { valid: true } } } }
  })
  await page.loadAll()
  page.updateField(['imap', 'tlsPort'], null)
  await page.validateConfig()
  await page.saveConfig()
  assert.deepEqual(requests, [])
  assert.ok(page.formErrors.value['imap.tlsPort'])
  page.updateField(['imap', 'tlsPort'], 1993)
  assert.deepEqual(page.formErrors.value, {})
  await page.validateConfig()
  assert.deepEqual(requests, ['/api/v1/config/email/validate'])
})

test('插件读取状态字段不传回严格解析接口，未展示字段和权限仍保留', async () => {
  let request
  const plugin = { enabled: true, executable: 'pmail_telegram_push.exe', permissions: { capabilities: ['network', 'userSettings', 'settingsHtml'], network: [] }, settings: { timeout: 30, future: 'preserved' }, secretStatus: { configured: true }, settingsConfigured: true }
  const { page } = fixture(async () => ({ errorNo: 0, data: { meta: { generation: 7 }, plugins: { telegram_push: plugin } } }), 'plugins', {
    http: { put: async (path, body) => { request = body; return { errorNo: 0, data: { generation: 8 } } } }
  })
  await page.loadSection()
  page.updateField(['plugins', 'telegram_push', 'settings', 'timeout'], 40)
  await page.saveConfig()
  const actual = request.payload.plugins.telegram_push
  assert.equal(actual.secretStatus, undefined)
  assert.equal(actual.settingsConfigured, undefined)
  assert.deepEqual(actual.permissions, plugin.permissions)
  assert.deepEqual(actual.settings, { timeout: 40, future: 'preserved' })
  assert.equal(request.payload.meta.generation, 7)
  assert.deepEqual(request.secretChanges, {})
  assert.equal(page.formConfig.value.plugins.telegram_push.secretStatus.configured, true)
})

test('SQLite 不显示或发送密码，网络数据库和中继密码留空保留且不裁剪空格', async () => {
  for (const type of ['sqlite', 'mysql', 'postgresql']) {
    let request
    const { page } = fixture(async () => ({ errorNo: 0, data: {
      database: { type, host: 'localhost', port: 3306, username: 'test' },
      domains: { primary: 'example.com', web: 'mail.example.com', accepted: ['example.com'] }
    } }), 'base', {
      http: { post: async (path, body) => { request = body; return { errorNo: 0, data: { valid: true } } } }
    })
    await page.loadSection()
    assert.equal(!!page.secretField.value, type !== 'sqlite')
    await page.validateConfig()
    assert.deepEqual(request.secretChanges, {})
    page.secretValue.value = ' test-only-value '
    await page.validateConfig()
    assert.deepEqual(request.secretChanges, type === 'sqlite' ? {} : { 'database.password': { operation: 'replace', value: { plaintext: ' test-only-value ' } } })
  }
  let request
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }), 'smtp', {
    http: { post: async (path, body) => { request = body; return { errorNo: 0, data: { valid: true } } } }
  })
  await page.loadSection()
  page.updateSecret(' smtp-test-only ')
  await page.validateConfig()
  assert.equal(request.secretChanges['relay.password'].value.plaintext, ' smtp-test-only ')
})

test('基础配置保存完整保留隐藏的额外访问主机、数据库和代理信任配置', async () => {
  const config = {
    database: { type: 'sqlite', localRef: 'preserved.db' },
    domains: { primary: 'example.com', web: 'mail.example.com', accepted: ['example.com'], accessHosts: ['alternate.example.com', '192.0.2.10'] },
    http: { tlsMode: 'proxy', port: 8080, httpsEnabled: false, trustedProxyCidrs: ['127.0.0.1/32', '192.0.2.10/32'] },
    security: { externalEmailEnabled: false, webAuthnEnabled: false }
  }
  let request
  const { page } = fixture(async () => ({ errorNo: 0, data: config }), 'base', {
    http: { put: async (path, body) => { request = { path, body }; return { errorNo: 0, data: { generation: 2 } } } }
  })
  await page.loadSection()
  page.updateField(['http', 'port'], 9080)
  await page.saveConfig()
  assert.equal(request.path, '/api/v1/config/base')
  assert.deepEqual(request.body.payload, { ...config, http: { ...config.http, port: 9080 } })
  assert.deepEqual(request.body.secretChanges, {})
})

test('保存后更改密码或填写内容不能应用旧候选', async () => {
  const requests = []
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }), 'smtp', {
    http: {
      put: async () => ({ errorNo: 0, data: { generation: 8 } }),
      post: async path => { requests.push(path); return { errorNo: 0 } }
    }
  })
  await page.loadSection()
  await page.saveConfig()
  page.updateSecret('test-only-value')
  assert.equal(page.savedGeneration.value, 0)
  await page.applyConfig()
  await page.saveConfig()
  page.editorText.value = '{"relay":{"relayHost":"new.example.com"}}'
  await page.applyConfig()
  assert.equal(page.savedGeneration.value, 0)
  assert.deepEqual(requests, [])
})

test('共用邮箱分区的候选明确提示其他页面的改动，忽略代次与字段顺序', async () => {
  const active = { meta: { generation: 1 }, imap: { tlsPort: 993 }, pop3: { port: 110, tlsPort: 995 }, policy: { maxMessageBytes: 1234 } }
  for (const key of ['imap', 'pop3', 'email']) {
    const { page } = fixture(async path => ({ errorNo: 0, data: path.endsWith('/smtp') ? { meta: { generation: 1 }, listener: {} } : active }), key)
    await page.loadSection()
    page.pendingCandidate.value = { section: 'email', payload: { ...active, meta: { generation: 2 }, pop3: { tlsPort: 995, port: 110 } } }
    assert.match(page.pendingScopeHint.value, /协议端口和邮箱策略未变化/)
    page.pendingCandidate.value.payload = { ...active, pop3: { port: 1110, tlsPort: 1995 }, policy: { maxMessageBytes: 5678 } }
    assert.match(page.pendingScopeHint.value, /本次候选涉及：POP3 配置、邮箱策略。/)
    assert.match(page.pendingScopeHint.value, /请逐页核对；应用时会一起生效/)
    page.pendingCandidate.value = { section: 'smtp' }
    assert.equal(page.pendingScopeHint.value, '')
    page.pendingCandidate.value = { section: 'email' }
    assert.match(page.pendingScopeHint.value, /候选内容尚未完整读取/)
  }
  const template = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigEditor/ConfigEditor.vue', import.meta.url), 'utf8')
  assert.match(template, /:title="pendingScopeHint"/)
})

for (const key of ['spam_block', 'telegram_push', 'wechat_push']) {
  test(`${key} 独立页面保留整个插件分区，验证、候选恢复和应用使用统一接口`, async () => {
    let active = { meta: { generation: 1 }, plugins: {
      spam_block: { enabled: false, executable: 'spam_block.exe', permissions: { network: [] }, settings: { threshold: 20 } },
      telegram_push: { enabled: false, executable: 'pmail_telegram_push.exe', settings: { timeout: 30 }, secretStatus: { configured: true }, settingsConfigured: true },
      wechat_push: { enabled: false, executable: 'wechat_push.exe', settings: { appId: 'test-only', future: 'preserved' } }
    } }
    const original = structuredClone(active)
    let pending = null
    const requests = []
    const http = {
      get: async path => {
        requests.push(path)
        return { errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: active.meta.generation }
          : path.endsWith('/pending') ? pending : active }
      },
      put: async (path, body) => {
        assert.equal(path, '/api/v1/config/plugins')
        assert.equal(body.expectedGeneration, 1)
        assert.deepEqual(body.secretChanges, {})
        assert.equal(body.payload.plugins.telegram_push.secretStatus, undefined)
        assert.equal(body.payload.plugins.telegram_push.settingsConfigured, undefined)
        for (const id of Object.keys(original.plugins)) {
          assert.deepEqual(body.payload.plugins[id].settings, original.plugins[id].settings)
          assert.equal(body.payload.plugins[id].enabled, id === key)
        }
        pending = { generation: 2, section: 'plugins', sameSession: true, candidateId: 'plugin-test', payload: body.payload }
        return { errorNo: 0, data: pending }
      },
      post: async (path, body) => {
        requests.push(path)
        if (path.endsWith('/apply')) {
          assert.equal(body.generation, 2)
          assert.equal(body.candidateId, 'plugin-test')
          active = pending.payload
          active.meta.generation = 2
          pending = null
        }
        return { errorNo: 0, data: { valid: true } }
      }
    }
    const { page, messages, dispose } = fixture(() => {}, key, { http })
    await page.loadAll()
    assert.deepEqual(requests, ['/api/v1/config/status', '/api/v1/config/plugins', '/api/v1/config/pending'])
    assert.equal(page.selectedSection.value, key)
    page.updateField(['plugins', key, 'enabled'], true)
    await page.validateConfig()
    await page.saveConfig()
    assert.ok(requests.includes('/api/v1/config/plugins/validate'))
    assert.match(page.pendingScopeHint.value, new RegExp(`本次候选涉及：${page.currentSection.value.label}。`))
    const other = Object.keys(original.plugins).find(id => id !== key)
    page.updateField(['plugins', other, 'enabled'], true)
    assert.equal(page.formConfig.value.plugins[other].enabled, false)
    dispose()
    const restored = fixture(() => {}, other, { http }).page
    await restored.loadAll()
    assert.equal(restored.savedGeneration.value, 2)
    assert.match(restored.pendingScopeHint.value, /三个插件共用一个配置分区/)
    assert.ok(restored.pendingScopeHint.value.includes(page.currentSection.value.label))
    await restored.applyConfig()
    assert.ok(requests.includes('/api/v1/config/plugins/apply'))
    assert.equal(restored.savedGeneration.value, 0)
    assert.deepEqual(messages, [])
  })
}
