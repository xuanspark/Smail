import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, reactive, ref } from './vue.mjs'

const formSource = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigForm/ConfigForm.js', import.meta.url), 'utf8')
const formHelpers = compileFunction(
  formSource
    .replaceAll('export const ', 'const ')
    .replaceAll('export function ', 'function ')
  + '\nreturn { readFormConfig, updateFormConfig, validateFormConfig, relayConfigured, relaySecurityForMail }'
)()
const recentAuthenticationSource = readFileSync(new URL('../../frontend/src/utils/recentAuthentication.js', import.meta.url), 'utf8')
  .replace(/^import .*$/gm, '')
  .replaceAll('export async function ', 'async function ')
  .replaceAll('export function ', 'function ')
const compileRecentAuthentication = compileFunction(
  recentAuthenticationSource + '\nreturn { isRecentAuthenticationCancelled, requestWithRecentAuthentication }',
  ['ElMessageBox', 'http']
)
const source = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigSettings.js', import.meta.url), 'utf8')
const compilePage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage(pageKey)',
  ['computed', 'onMounted', 'reactive', 'ref', 'ElMessage', 'ElMessageBox', 'http', 'useGlobalStatusStore', 'pageKey', 'onBeforeUnmount', 'readFormConfig', 'updateFormConfig', 'validateFormConfig', 'relayConfigured', 'relaySecurityForMail', 'isRecentAuthenticationCancelled', 'requestWithRecentAuthentication']
)
const createPageWithKey = (...args) => {
  const http = args[6]
  const messageBox = args[5]
  const recentAuthentication = compileRecentAuthentication(messageBox, http)
  return compilePage(
    ...args,
    formHelpers.readFormConfig,
    formHelpers.updateFormConfig,
    formHelpers.validateFormConfig,
    formHelpers.relayConfigured,
    formHelpers.relaySecurityForMail,
    recentAuthentication.isRecentAuthenticationCancelled,
    recentAuthentication.requestWithRecentAuthentication
  )
}
const createPage = (...args) => createPageWithKey(...args, 'base', () => {})

function fixture(get, pageKey = 'base', options = {}) {
  const messages = []
  const successes = []
  let dispose = () => {}
  const page = createPageWithKey(computed, () => {}, reactive, ref,
    { error: message => messages.push(message), warning: message => messages.push(message), success: message => successes.push(message) },
    {
      confirm: options.confirm || (async () => {}),
      prompt: options.prompt || (async () => { throw 'cancel' })
    }, {
      get: path => path.endsWith('/pending') && !options.pendingGet ? Promise.resolve({ errorNo: 0, data: null }) : get(path),
      ...options.http
    },
    () => ({ role: options.role || 'super_admin', hasCapability: options.hasCapability || (() => true) }), pageKey, callback => { dispose = callback })
  return { page, messages, successes, dispose: () => dispose() }
}

for (const pageKey of ['telegram_push', 'wechat_push']) {
  for (const role of ['super_admin', 'admin', 'user']) {
    test(`${role} 的 ${pageKey} 系统配置写权限只对超级管理员开放`, () => {
      const { page } = fixture(async () => ({ errorNo: 0, data: {} }), pageKey, { role })
      assert.equal(page.canWrite.value, role === 'super_admin')
    })
  }
}

function deferred() {
  let resolve
  let reject
  const promise = new Promise((success, failure) => { resolve = success; reject = failure })
  return { promise, resolve, reject }
}

test('DNS 凭据只通过专用密钥变更提交，留空保持原值', async () => {
  const sent = []
  const current = {
    tls: { management: 'project', mode: 'managed_default', defaultDirectory: 'ssl', challenge: 'dns', dnsProvider: 'alidns', dnsAccessKey: {}, dnsSecretKey: {} },
    dnsAccessKeyStatus: { configured: true }, dnsSecretKeyStatus: { configured: true }
  }
  const { page } = fixture(async path => ({ errorNo: 0, data: path.endsWith('/status') ? { available: true, activeGeneration: 1 } : current }), 'ssl', {
    http: { post: async (path, body) => { sent.push(body); return { errorNo: 0, data: { valid: true } } } }
  })
  await page.loadAll()
  page.updateField(['tls', 'dnsSecretKeyInput'], 'new-test-secret')
  await page.validateConfig()
  assert.equal(sent.length, 1)
  assert.deepEqual(sent[0].secretChanges, { 'tls.dnsSecretKey': { operation: 'replace', value: { plaintext: 'new-test-secret' } } })
  assert.equal(sent[0].payload.dnsAccessKeyStatus, undefined)
  assert.equal(sent[0].payload.dnsSecretKeyStatus, undefined)
  assert.equal(sent[0].payload.tls.dnsSecretKeyInput, undefined)
  assert.equal(sent[0].payload.tls.dnsSecretKey, undefined)
  page.updateField(['tls', 'dnsSecretKeyInput'], '')
  await page.validateConfig()
  assert.deepEqual(sent[1].secretChanges, {})
})

for (const cancelled of [false, true]) {
  test(`共享身份确认保留单次重试，移除审计不影响其他操作：取消=${cancelled}`, async () => {
    const requests = []
    let prompts = 0
    const helpers = compileRecentAuthentication({
      prompt: async () => {
        prompts++
        if (cancelled) throw 'cancel'
        return { value: 'test-password' }
      }
    }, {
      post: async (url, body) => {
        requests.push(url)
        assert.equal(body.password, 'test-password')
        return { errorNo: 0 }
      }
    })
    let attempts = 0
    const action = helpers.requestWithRecentAuthentication(async () => {
      if (++attempts === 1) throw Object.assign(new Error('需要最近认证'), { apiErrorCode: 'RECENT_AUTH_REQUIRED' })
      return { errorNo: 0 }
    })
    if (cancelled) await assert.rejects(action, error => helpers.isRecentAuthenticationCancelled(error))
    else assert.equal((await action).errorNo, 0)
    assert.equal(prompts, 1)
    assert.equal(attempts, cancelled ? 1 : 2)
    assert.deepEqual(requests, cancelled ? [] : ['/api/v1/security/reauth'])
  })
}

test('移除配置审计查询、导出和维护面板，不保留隐藏请求', async () => {
  const requests = []
  const { page } = fixture(async path => {
    requests.push(path)
    return { errorNo: 0, data: { available: true } }
  }, 'audit')
  await page.loadAll()
  assert.deepEqual(requests, ['/api/v1/config/status'])
  assert.equal(page.loadAudit, undefined)
  assert.equal(page.exportAudit, undefined)
  assert.doesNotMatch(source, /audits\/config|auditItems|canExportAudit/)
})

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
  assert.doesNotMatch(template, /selectedSection === 'ssl' && status\.tlsMode === 'proxy'|Smail 不自动申请或续期 SSL 证书/)
})

for (const failedRequest of ['/status', '/pending']) {
  test(`候选保存成功后 ${failedRequest} 读取失败不能重新保存，刷新后可恢复或取消`, async () => {
    let pending = null
    let fail = false
    let saves = 0
    const active = {
      listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
      relay: {
        relayHost: 'old.example.com', relayPort: 587, security: 'starttls',
        from: 'sender@example.com', fromDomain: 'example.com'
      },
      imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 }, policy: {}
    }
    const { page } = fixture(() => {}, 'mail', { http: {
      get: async path => {
        if (fail && path.endsWith(failedRequest)) throw new Error('模拟读取失败')
        return { errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: 1 } : active }
      },
      put: async (path, body) => {
        saves++
        pending = { generation: 2, candidateId: 'pending-recovery', section: 'mail', sameSession: true, payload: body.payload }
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
  await backup.loadAll()
  assert.equal(backup.backupsError.value, '配置服务暂时不可用')
  assert.equal(backup.editorText.value, '')
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
  page.rollbackGeneration.value = 1
  page.rollbackPreview.value = { targetGeneration: 1 }
  page.rollbackConfirmed.value = true
  await page.loadBackups()
  assert.deepEqual(page.backups.value, [])
  assert.equal(page.rollbackGeneration.value, 0)
  assert.equal(page.rollbackPreview.value, null)
  assert.equal(page.rollbackConfirmed.value, false)
  fail = false
  await page.loadBackups()
  assert.equal(page.backupsError.value, '')
  assert.equal(page.backups.value.length, 1)
  assert.equal(page.backupsLoading.value, false)
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

test('独立页互不覆盖，页面卸载时清除密码并丢弃较慢响应', async () => {
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
  const emailPage = fixture(() => email.promise, 'mail').page
  const emailRequest = emailPage.loadSection()
  email.resolve({ errorNo: 0, data: { policy: { smtpPort: 25 } } })
  await emailRequest
  assert.equal(emailPage.currentSection.value.label, '邮件服务')
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

test('配置页按需加载，邮件服务、邮件策略和安全策略页共用 mail 分区', async () => {
  const requests = []
  for (const key of ['base', 'mail', 'mail_policy', 'security_policy', 'ssl', 'plugins']) {
    const { page } = fixture(async path => { requests.push(path); return { errorNo: 0, data: {} } }, key)
    await page.loadAll()
    assert.equal(page.selectedSection.value, key)
    assert.match(page.currentSection.value.label, /[\u4e00-\u9fff]/)
    assert.equal(page.secretValue.value, '')
    assert.equal(page.confirmed.value, false)
    assert.equal(page.savedGeneration.value, 0)
  }
  assert.deepEqual(requests, ['base', 'mail', 'mail_policy', 'security_policy', 'ssl', 'plugins']
    .flatMap(key => ['/api/v1/config/status', `/api/v1/config/${['mail_policy', 'security_policy'].includes(key) ? 'mail' : key}`]))
})

for (const key of ['mail', 'mail_policy', 'security_policy']) {
  test(`${key} 页面验证、保存、候选恢复及应用均使用 mail 分区且保留未展示字段`, async () => {
    const requests = []
    const payloads = []
    let pending = null
    let active = {
      meta: { schema: 1, generation: 1 },
      listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
      relay: { verificationEnabled: true, loginPolicy: 'new_device' },
      imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 },
      policy: { maxMessageBytes: 1234, spamFilterLevel: 1, externalRecoveryEnabled: true }
    }
    const { page } = fixture(() => {}, key, { http: {
      get: async path => ({ errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : structuredClone(active) }),
      put: async (path, body) => {
        requests.push(path)
        payloads.push(structuredClone(body))
        pending = { generation: 2, candidateId: 'mail-candidate', section: 'mail', sameSession: true, payload: body.payload }
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
    const edit = {
      mail: { path: ['imap', 'tlsPort'], value: 1993, rejected: 8888 },
      mail_policy: { path: ['policy', 'maxMessageBytes'], value: 5678, rejected: 8765 },
      security_policy: { path: ['relay', 'loginPolicy'], value: 'always', rejected: 'off' }
    }[key]
    page.updateField(edit.path, edit.value)
    await page.validateConfig()
    await page.saveConfig()
    assert.equal(page.savedGeneration.value, 2)
    assert.equal(payloads[0].expectedGeneration, 1)
    assert.deepEqual(payloads[0].payload.listener, active.listener)
    assert.deepEqual(payloads[0].payload.pop3, active.pop3)
    if (key !== 'mail') assert.equal(payloads[0].secretChanges['relay.password'], undefined)
    if (key === 'security_policy') {
      await page.refreshPage()
      assert.equal(page.savedGeneration.value, 2)
    }
    page.updateField(edit.path, edit.rejected)
    assert.notEqual(edit.path.reduce((value, part) => value?.[part], page.formConfig.value), edit.rejected)
    await page.applyConfig()
    assert.equal(page.status.activeGeneration, 2)
    assert.equal(page.savedGeneration.value, 0)
    assert.deepEqual(requests, ['/api/v1/config/mail/validate', '/api/v1/config/mail', '/api/v1/config/mail/apply'])
  })
}

test('安全策略页只编辑登录验证和账号恢复字段，完整保留邮件监听、中继与邮件策略', async () => {
  const config = {
    listener: { port: 25, starttlsPort: 587, tlsPort: 465 },
    relay: { relayHost: 'smtp.example.com', relayPort: 587, security: 'starttls', verificationEnabled: true, loginPolicy: 'new_device' },
    imap: { tlsPort: 993 }, pop3: { port: 110, tlsPort: 995 },
    policy: { maxMessageBytes: 10485760, spamFilterLevel: 2, externalRecoveryEnabled: true }
  }
  let request
  const { page } = fixture(async () => ({ errorNo: 0, data: config }), 'security_policy', {
    http: { put: async (path, body) => { request = { path, body }; return { errorNo: 0, data: { generation: 2 } } } }
  })
  await page.loadSection()
  page.updateField(['relay', 'loginPolicy'], 'always')
  page.updateField(['listener', 'port'], 2525)
  await page.saveConfig()
  assert.equal(request.path, '/api/v1/config/mail')
  assert.equal(request.body.payload.listener.port, 25)
  assert.equal(request.body.payload.relay.loginPolicy, 'always')
  assert.deepEqual(request.body.payload.imap, config.imap)
  assert.deepEqual(request.body.payload.pop3, config.pop3)
  assert.equal(request.body.payload.policy.maxMessageBytes, config.policy.maxMessageBytes)
  assert.deepEqual(request.body.secretChanges, {})
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
  assert.doesNotMatch(template, /v-model="confirmed"/)
  assert.doesNotMatch(template, /maintenance-panel|migration-panel|rollback-panel|audit-panel/)
  for (const [key, capability] of [['migration', 'canMigrate'], ['restart', 'canRestart'], ['backups', 'canRollback']]) {
    assert.ok(maintenance.includes(`pageKey === '${key}' && ${capability}`))
  }
  assert.doesNotMatch(maintenance, /audit-panel|canExportAudit|loadAudit/)
  for (const name of ['BaseConfig', 'EmailConfig', 'SslConfig', 'PluginsConfig', 'ConfigMaintenance']) {
    const page = readFileSync(new URL(`${name}/${name}.vue`, root), 'utf8')
    const { descriptor, errors } = parse(page)
    assert.deepEqual(errors, [])
    assert.doesNotThrow(() => compileScript(descriptor, { id: name, inlineTemplate: true }))
  }
  assert.match(css, /@container config-main \(max-width: 620px\)/)
  assert.match(css, /\.maintenance-summary:focus-visible/)
  assert.doesNotMatch(template, /&#x[0-9a-f]+;/i)
})

test('配置编辑页将验证、候选保存和应用精简为单一保存操作', () => {
  const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
  const template = readFileSync(new URL('ConfigEditor/ConfigEditor.vue', root), 'utf8')
  const sharedCss = readFileSync(new URL('ConfigSettings.css', root), 'utf8')
  const editorCss = readFileSync(new URL('ConfigEditor/ConfigEditor.css', root), 'utf8')
  assert.doesNotMatch(template, /change-workflow|step-number|配置变更流程/)
  assert.doesNotMatch(sharedCss + editorCss, /change-workflow|step-number/)
  assert.match(template, /@click="saveAndApply"/)
  assert.match(template, /保存配置/)
  assert.match(template, /continuingSave \? '继续保存' : '保存配置'/)
  assert.match(template, /v-if="showPendingRecovery"/)
  assert.match(template, /v-if="showPendingRecovery && pendingScopeHint"/)
  assert.match(template, /v-if="saveError"[\s\S]*?:title="saveError"[\s\S]*?type="error"/)
  assert.match(template, /继续保存/)
  assert.match(template, /v-if="canWrite" class="editor-footer"/)
  assert.match(template, /@click="managePending\('cancel'\)"/)
  assert.match(template, /放弃更改/)
  assert.doesNotMatch(template, /只验证|保存候选配置|我已确认这是受控变更|应用代次/)
})

test('保存并应用一次点击按候选协议连续完成保存和应用', async () => {
  const requests = []
  let pending = null
  let active = {
    meta: { generation: 4 },
    listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
    relay: { relayHost: '', relayPort: 0, security: '', from: '', fromDomain: '' },
    imap: { tlsPort: 143 },
    pop3: { port: 110, tlsPort: 0 },
    policy: { maxMessageBytes: 10 << 20, spamFilterLevel: 2 }
  }
  const { page, successes, messages } = fixture(async path => ({
    errorNo: 0,
    data: path.endsWith('/status') ? { activeGeneration: 4 } : active
  }), 'mail', {
    http: {
      get: async path => ({
        errorNo: 0,
        data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : active
      }),
      put: async (path, body) => {
        requests.push(path)
        pending = { generation: 5, candidateId: 'one-click-5', section: 'mail', sameSession: true, payload: body.payload }
        return { errorNo: 0, data: pending }
      },
      post: async (path, body) => {
        requests.push(path)
        assert.equal(body.generation, 5)
        assert.equal(body.candidateId, 'one-click-5')
        active = { ...pending.payload, meta: { generation: 5 } }
        pending = null
        return { errorNo: 0, data: { generation: 5 } }
      }
    }
  })
  await page.loadAll()
  page.updateField(['listener', 'port'], 2525)
  await page.saveAndApply()
  assert.deepEqual(requests, ['/api/v1/config/mail', '/api/v1/config/mail/apply'])
  assert.equal(page.pendingCandidate.value, null)
  assert.equal(page.savedGeneration.value, 0)
  assert.deepEqual(successes, ['配置已保存'])
  assert.deepEqual(messages, [])
  assert.equal(page.formConfig.value.listener.port, 2525)
})

test('一键保存的所有异步阶段保持忙碌，不闪现恢复提示或允许重复操作', { timeout: 5000 }, async t => {
  const stages = Array.from({ length: 7 }, () => ({ entered: deferred(), release: deferred() }))
  t.after(() => stages.forEach(stage => stage.release.resolve()))
  let requestIndex = 0
  let testing = false
  let pending = null
  let active = {
    meta: { generation: 1 },
    tls: { management: 'custom', mode: 'managed_upload', defaultDirectory: 'config/ssl', customDirectory: 'config/ssl', certificateRef: 'config/ssl/public.crt', privateKeyRef: 'config/ssl/private.key' }
  }
  const pause = async () => {
    if (!testing) return
    const stage = stages[requestIndex++]
    assert.ok(stage, '不应产生重复请求')
    stage.entered.resolve()
    await stage.release.promise
  }
  const { page, successes } = fixture(() => {}, 'ssl', { http: {
    get: async path => {
      await pause()
      return { errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : active }
    },
    put: async (path, body) => {
      await pause()
      pending = { generation: 2, candidateId: 'continuous-save', section: 'ssl', sameSession: true, payload: body.payload }
      return { errorNo: 0, data: pending }
    },
    post: async () => {
      await pause()
      active = { ...pending.payload, meta: { generation: 2 } }
      pending = null
      return { errorNo: 0, data: { generation: 2 } }
    }
  } })
  await page.loadAll()
  page.updateField(['tls', 'certificateRef'], 'D:/certs/public.crt')
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { watch } = require('vue')
  const transitions = []
  const stop = watch(() => [page.working.value, page.saveFlow.value, page.pendingCandidate.value], () => {
    transitions.push({ working: page.working.value, flow: page.saveFlow.value, pending: !!page.pendingCandidate.value, busy: page.busy.value, recovery: page.showPendingRecovery.value })
  }, { flush: 'sync' })
  t.after(stop)
  testing = true
  const saving = page.saveAndApply()
  for (const stage of stages) {
    await Promise.race([stage.entered.promise, saving.then(() => { throw new Error('保存流程提前结束') })])
    assert.equal(page.busy.value, true)
    assert.equal(page.saveFlow.value, 'save')
    assert.equal(page.showPendingRecovery.value, false)
    assert.equal(page.continuingSave.value, false)
    const input = page.editorText.value
    page.updateField(['tls', 'certificateRef'], 'D:/blocked.crt')
    assert.equal(page.editorText.value, input)
    assert.equal(await page.saveAndApply(), false)
    assert.equal(await page.continuePending(), false)
    assert.equal(await page.beforeLeave(), false)
    stage.release.resolve()
  }
  assert.equal(await saving, true)
  assert.equal(requestIndex, 7)
  assert.equal(page.busy.value, false)
  assert.equal(page.saveFlow.value, '')
  assert.equal(page.showPendingRecovery.value, false)
  assert.deepEqual(successes, ['配置已保存'])
  const gaps = transitions.filter(state => state.pending && !state.working && state.flow)
  assert.ok(gaps.length > 0, '必须检查暂存与应用之间的同步状态切换')
  assert.ok(gaps.every(state => state.busy && !state.recovery))
})

for (const failure of ['save', 'apply', 'apply-auth-cancel', 'readback']) {
  test(`一键保存 ${failure} 后解除忙碌，真实失败仍提示且未应用的候选可以恢复`, async () => {
    let fail = true
    let applied = false
    let pending = null
    let writes = 0
    const config = {
      meta: { generation: 1 },
      tls: { management: 'custom', mode: 'managed_upload', defaultDirectory: 'config/ssl', customDirectory: 'config/ssl', certificateRef: 'config/ssl/public.crt', privateKeyRef: 'config/ssl/private.key' }
    }
    const { page, messages, successes } = fixture(() => {}, 'ssl', { http: {
      get: async path => {
        if (fail && applied && failure === 'readback') throw new Error('模拟回读失败')
        return { errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: applied ? 2 : 1 } : { ...config, meta: { generation: applied ? 2 : 1 } } }
      },
      put: async (path, body) => {
        writes++
        if (fail && failure === 'save') throw new Error('模拟保存失败')
        pending = { generation: 2, candidateId: 'failed-save', section: 'ssl', sameSession: true, payload: body.payload }
        return { errorNo: 0, data: pending }
      },
      post: async () => {
        writes++
        if (fail && failure === 'apply') throw new Error('模拟应用失败')
        if (fail && failure === 'apply-auth-cancel') throw Object.assign(new Error('需要最近认证'), { apiErrorCode: 'RECENT_AUTH_REQUIRED' })
        applied = true
        pending = null
        return { errorNo: 0, data: { generation: 2 } }
      }
    } })
    await page.loadAll()
    page.updateField(['tls', 'certificateRef'], 'D:/certs/public.crt')
    assert.equal(await page.saveAndApply(), false)
    assert.equal(page.busy.value, false)
    assert.equal(page.saveFlow.value, '')
    assert.deepEqual(successes, [])
    const recoverable = ['apply', 'apply-auth-cancel'].includes(failure)
    assert.equal(page.showPendingRecovery.value, recoverable)
    assert.equal(page.continuingSave.value, recoverable)
    if (failure === 'apply-auth-cancel') {
      assert.deepEqual(messages, [])
      assert.equal(page.saveError.value, '')
    } else {
      assert.ok(messages.length > 0, '真实失败不能被隐藏')
      assert.equal(page.saveError.value || page.sectionError.value, messages.at(-1), '错误必须保留在页面中')
    }
    if (failure === 'save') {
      page.updateField(['tls', 'certificateRef'], 'D:/certs/fixed/public.crt')
      assert.equal(page.saveError.value, '')
    }
    if (recoverable) {
      fail = false
      const writesBefore = writes
      const continuing = page.continuePending()
      assert.equal(page.saveFlow.value, 'resume')
      assert.equal(page.continuingSave.value, true)
      assert.equal(page.showPendingRecovery.value, false)
      assert.equal(page.saveError.value, '')
      assert.equal(await continuing, true)
      assert.equal(writes, writesBefore + 1, '只应用已有候选，不重复保存')
      assert.equal(page.showPendingRecovery.value, false)
      assert.deepEqual(successes, ['配置已保存'])
    }
  })
}

for (const rejected of [false, true]) {
  test(`恢复其他会话候选期间保持按钮稳定，恢复失败保留入口：失败=${rejected}`, async () => {
    let active = {
      meta: { generation: 1 },
      tls: { management: 'custom', mode: 'managed_upload', defaultDirectory: 'config/ssl', customDirectory: 'config/ssl', certificateRef: 'config/ssl/public.crt', privateKeyRef: 'config/ssl/private.key' }
    }
    let pending = { generation: 2, section: 'ssl', sameSession: false, candidateId: 'another-session', payload: active }
    const requests = []
    const { page, messages } = fixture(() => {}, 'ssl', { http: {
      get: async path => ({ errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : active }),
      post: async (path, body) => {
        assert.equal(page.saveFlow.value, 'resume')
        assert.equal(page.busy.value, true)
        assert.equal(page.continuingSave.value, true)
        assert.equal(page.showPendingRecovery.value, false)
        requests.push(path)
        if (path.endsWith('/pending')) {
          assert.equal(body.action, 'resume')
          if (rejected) throw new Error('模拟候选恢复失败')
          pending = { ...pending, sameSession: true }
        } else {
          assert.equal(body.candidateId, 'another-session')
          active = { ...active, meta: { generation: 2 } }
          pending = null
        }
        return { errorNo: 0, data: { generation: 2 } }
      }
    } })
    await page.loadAll()
    assert.equal(page.showPendingRecovery.value, true)
    assert.equal(await page.continuePending(), !rejected)
    assert.equal(page.busy.value, false)
    assert.equal(page.showPendingRecovery.value, rejected)
    assert.equal(page.saveError.value, rejected ? '模拟候选恢复失败' : '')
    assert.deepEqual(requests, rejected ? ['/api/v1/config/pending'] : ['/api/v1/config/pending', '/api/v1/config/ssl/apply'])
    assert.deepEqual(messages, rejected ? ['模拟候选恢复失败'] : [])
  })
}

test('最近认证过期时在当前页确认密码并自动重试保存和应用', async () => {
  const requests = []
  let pending = null
  let saveAttempts = 0
  let prompts = 0
  let active = {
    meta: { generation: 1 },
    listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
    relay: { relayHost: '', relayPort: 0, security: '', from: '', fromDomain: '' },
    imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 }, policy: {}
  }
  const recentAuthError = Object.assign(new Error('需要最近认证'), { apiErrorCode: 'RECENT_AUTH_REQUIRED' })
  const { page, successes, messages } = fixture(async () => ({ errorNo: 0, data: active }), 'mail', {
    prompt: async () => { prompts++; return { value: 'CurrentPass12' } },
    http: {
      get: async path => ({
        errorNo: 0,
        data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : active
      }),
      put: async (path, body) => {
        requests.push(path)
        saveAttempts++
        if (saveAttempts === 1) throw recentAuthError
        pending = { generation: 2, candidateId: 'reauth-candidate', section: 'mail', sameSession: true, payload: body.payload }
        return { errorNo: 0, data: pending }
      },
      post: async (path, body) => {
        requests.push(path)
        if (path.endsWith('/reauth')) {
          assert.deepEqual(body, { password: 'CurrentPass12' })
          return { errorNo: 0, data: { reauthenticated: true } }
        }
        active = { ...pending.payload, meta: { generation: 2 } }
        pending = null
        return { errorNo: 0, data: { generation: 2 } }
      }
    }
  })
  await page.loadAll()
  page.updateField(['listener', 'port'], 2525)
  await page.saveAndApply()
  assert.equal(prompts, 1)
  assert.deepEqual(successes, ['配置已保存'])
  assert.deepEqual(messages, [])
  assert.deepEqual(requests, [
    '/api/v1/config/mail',
    '/api/v1/security/reauth',
    '/api/v1/config/mail',
    '/api/v1/config/mail/apply'
  ])
})

test('取消最近身份确认时保留配置输入且不重试请求', async () => {
  let saves = 0
  const active = {
    listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
    relay: {}, imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 }, policy: {}
  }
  const { page, messages } = fixture(async path => ({
    errorNo: 0,
    data: path.endsWith('/status') ? { activeGeneration: 1 } : active
  }), 'mail', {
    prompt: async () => { throw 'cancel' },
    http: {
      put: async () => {
        saves++
        throw Object.assign(new Error('需要最近认证'), { apiErrorCode: 'RECENT_AUTH_REQUIRED' })
      }
    }
  })
  await page.loadAll()
  page.updateField(['listener', 'port'], 2525)
  await page.saveAndApply()
  assert.equal(saves, 1)
  assert.equal(page.formConfig.value.listener.port, 2525)
  assert.deepEqual(messages, [])
})

test('配置编辑区移除填写标题、说明与重复标签，保留主标题和必要提示', () => {
  const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
  const template = readFileSync(new URL('ConfigEditor/ConfigEditor.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigSettings.css', root), 'utf8')
  assert.doesNotMatch(template, /'填写配置'|'查看配置'|按分组填写内容|当前页面的脱敏配置内容|panel-heading|selected-section-label/)
  assert.doesNotMatch(css, /selected-section-label/)
  assert.match(template, /<h3 id="config-page-title">{{ currentSection.label }}/)
  assert.doesNotMatch(template, /selectedSection === 'ssl' && status\.tlsMode === 'proxy'|role="note"/)
  assert.match(template, /v-if="sectionError"[\s\S]*?:title="sectionError"/)
  assert.doesNotMatch(template, /config-sensitive-hint|sensitiveHint/)
  assert.match(template, /v-if="secretField" class="secret-editor"/)
  assert.match(template, /v-if="validationResult && !validationResult\.valid"[^>]*role="status"/)
})

test('离开、刷新和正在执行的变更受保护，取消时保留编辑内容', async () => {
  let cancel = true
  let prompts = 0
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }), 'mail', {
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
  assert.doesNotMatch(header, /stateLabel|运行状态|state-tag/)
  assert.match(header, /v-for="issue in status.degradedReasons \|\| \[\]"/)
  assert.match(header, /v-if="!loading && !availabilityError"/)
  assert.match(header, /:disabled="busy \|\| sectionLoading \|\| loading"/)
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

test('邮件页保持验证、保存、应用端点和代次协议，中继密码只作为 secretChanges 发送', async () => {
  const requests = []
  let pending = null
  let active = {
    meta: { generation: 8 },
    listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
    relay: {
      relayHost: 'smtp.example.com', relayPort: 587, security: 'starttls',
      from: 'sender@example.com', fromDomain: 'example.com'
    },
    imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 }, policy: {}
  }
  const { page } = fixture(async path => ({ errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: 8 } : active }), 'mail', {
    http: {
      get: async path => ({ errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: active.meta.generation } : active }),
      post: async (path, body) => {
        requests.push({ path, body })
        if (path.endsWith('/apply')) {
          active = { ...pending.payload, meta: { generation: 9 } }
          pending = null
        }
        return { errorNo: 0, data: { valid: true } }
      },
      put: async (path, body) => {
        requests.push({ path, body })
        pending = { generation: 9, candidateId: 'candidate-9', section: 'mail', sameSession: true, payload: body.payload }
        return { errorNo: 0, data: pending }
      }
    }
  })
  await page.loadAll()
  page.updateField(['relay', 'password'], 'test-only-value')
  page.confirmed.value = true
  await page.validateConfig()
  await page.saveConfig()
  assert.equal(page.savedGeneration.value, 9)
  assert.equal(await page.applyConfig(), true)
  assert.deepEqual(requests.map(item => item.path), ['/api/v1/config/mail/validate', '/api/v1/config/mail', '/api/v1/config/mail/apply'])
  assert.equal(requests[0].body.expectedGeneration, 8)
  assert.equal(requests[0].body.payload.relay.password, undefined)
  assert.deepEqual(requests[0].body.payload.listener, active.listener)
  assert.deepEqual(requests[0].body.secretChanges, { 'relay.password': { operation: 'replace', value: { plaintext: 'test-only-value' } } })
  assert.deepEqual(requests[0].body.confirmation, { confirmed: true, method: 'recent_auth' })
  assert.deepEqual(requests[2].body, { generation: 9, candidateId: 'candidate-9' })
  assert.equal(page.secretValue.value, '')
  assert.equal(page.confirmed.value, false)
  assert.equal(page.savedGeneration.value, 0)
})

test('自定义证书导入后使用规范化的候选引用继续应用，不误判表单已变化', async () => {
  const requests = []
  let pending = null
  let generation = 8
  let active = {
    tls: {
      management: 'custom', mode: 'managed_upload', defaultDirectory: 'config/ssl', customDirectory: 'config/ssl',
      certificateRef: 'D:/certs/public.crt', privateKeyRef: 'D:/certs/private.key'
    }
  }
  const normalized = {
    meta: { generation: 9 },
    tls: {
      ...active.tls,
      certificateRef: 'config/ssl/managed/upload/version-0123456789abcdef0123456789abcdef/public.crt',
      privateKeyRef: 'config/ssl/managed/upload/version-0123456789abcdef0123456789abcdef/private.key'
    }
  }
  const { page, successes, messages } = fixture(async () => ({ errorNo: 0, data: active }), 'ssl', {
    http: {
      get: async path => ({ errorNo: 0, data: path.endsWith('/pending') ? pending : path.endsWith('/status') ? { activeGeneration: generation } : active }),
      put: async (path, body) => {
        requests.push({ path, body })
        pending = { generation: 9, candidateId: 'ssl-candidate', section: 'ssl', sameSession: true, payload: normalized }
        return { errorNo: 0, data: pending }
      },
      post: async (path, body) => {
        requests.push({ path, body })
        active = normalized
        generation = 9
        pending = null
        return { errorNo: 0, data: { generation: 9 } }
      }
    }
  })
  await page.loadAll()
  await page.saveAndApply()
  assert.deepEqual(requests.map(item => item.path), ['/api/v1/config/ssl', '/api/v1/config/ssl/apply'])
  assert.deepEqual(requests[1].body, { generation: 9, candidateId: 'ssl-candidate' })
  assert.equal(JSON.parse(page.editorText.value).tls.certificateRef, normalized.tls.certificateRef)
  assert.equal(page.savedGeneration.value, 0)
  assert.deepEqual(successes, ['配置已保存'])
  assert.deepEqual(messages, [])
})

for (const failure of ['section-network', 'section-response', 'status-network', 'pending-network', 'section-stale', 'status-stale']) {
  test(`证书应用后的 ${failure} 不得误报完成，刷新后恢复真实配置`, async () => {
    const original = {
      meta: { generation: 8 },
      tls: {
        management: 'custom', mode: 'managed_upload', defaultDirectory: 'config/ssl', customDirectory: 'config/ssl',
        certificateRef: 'config/ssl/old/public.crt', privateKeyRef: 'config/ssl/old/private.key'
      }
    }
    const normalized = {
      meta: { generation: 9 },
      tls: {
        ...original.tls,
        certificateRef: 'config/ssl/managed/upload/version-0123456789abcdef0123456789abcdef/public.crt',
        privateKeyRef: 'config/ssl/managed/upload/version-0123456789abcdef0123456789abcdef/private.key'
      }
    }
    let applied = false
    let fail = true
    let pending = null
    let writes = 0
    const { page, messages, successes } = fixture(() => {}, 'ssl', { http: {
      get: async path => {
        if (applied && fail) {
          if (failure === 'section-network' && path.endsWith('/ssl') ||
              failure === 'status-network' && path.endsWith('/status') ||
              failure === 'pending-network' && path.endsWith('/pending')) throw new Error('模拟回读失败')
          if (failure === 'section-response' && path.endsWith('/ssl')) return { errorNo: 500, errorMsg: '配置分区读取失败' }
        }
        const statusGeneration = applied && !(fail && failure === 'status-stale') ? 9 : 8
        const active = applied && !(fail && failure === 'section-stale') ? normalized : original
        return { errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: statusGeneration } : path.endsWith('/pending') ? pending : active }
      },
      put: async () => {
        writes++
        pending = { generation: 9, candidateId: 'ssl-readback', section: 'ssl', sameSession: true, payload: normalized }
        return { errorNo: 0, data: pending }
      },
      post: async () => {
        writes++
        applied = true
        pending = null
        return { errorNo: 0, data: { generation: 9 } }
      }
    } })
    await page.loadAll()
    page.updateField(['tls', 'certificateRef'], 'D:/certs/new/public.crt')
    page.updateField(['tls', 'privateKeyRef'], 'D:/certs/new/private.key')
    await page.saveAndApply()
    assert.equal(applied, true)
    assert.deepEqual(successes, [])
    assert.match(messages.at(-1), /配置已应用.*刷新/)
    assert.ok(page.sectionError.value)
    assert.equal(page.pendingCandidate.value, null)
    assert.equal(page.savedGeneration.value, 0)
    assert.equal(page.editorText.value, '')
    await page.saveAndApply()
    assert.equal(writes, 2)
    fail = false
    await page.refreshPage()
    assert.equal(page.sectionError.value, '')
    assert.equal(page.formConfig.value.tls.certificateRef, normalized.tls.certificateRef)
    assert.equal(page.status.activeGeneration, 9)
    assert.equal(page.hasChanges.value, false)
  })
}

test('可视化编辑按权限和工作状态保护，编辑会清除过期确认及候选代次', async () => {
  const config = {
    listener: { port: 25, starttlsPort: 587, tlsPort: 465 },
    relay: { relayHost: 'smtp.example.com' }, imap: { tlsPort: 993 }, pop3: { port: 110, tlsPort: 995 }, policy: {}
  }
  const { page, dispose } = fixture(async () => ({ errorNo: 0, data: config }), 'mail')
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
  const readonly = fixture(async () => ({ errorNo: 0, data: {} }), 'mail', { hasCapability: () => false }).page
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
  const config = {
    listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
    relay: {
      relayHost: 'smtp.example.com', relayPort: 587, security: 'starttls',
      from: 'sender@example.com', fromDomain: 'example.com'
    },
    imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 }, policy: {}
  }
  const { page } = fixture(async () => ({ errorNo: 0, data: config }), 'mail', {
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
  assert.deepEqual(requests, ['/api/v1/config/mail/validate'])
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

for (const key of ['telegram_push', 'wechat_push']) {
  test(`${key} 密钥独立提交、留空保留，保存回读与离开后清空`, async () => {
    const active = { meta: { generation: 1 }, plugins: {
      telegram_push: { enabled: false, settings: { timeout: 30 }, secretStatus: { configured: false } },
      wechat_push: { enabled: false, settings: { appId: 'test-only', future: 'preserved' }, secretStatus: { configured: false } }
    } }
    const original = structuredClone(active)
    let secretStatus = { configured: false, version: 0 }
    const requests = []
    const http = {
      get: async path => ({ errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: active.meta.generation }
        : path.endsWith('/pending') ? null : path.includes('/plugin/secrets/') ? secretStatus : active }),
      put: async (path, body) => {
        requests.push({ path, body })
        assert.equal(path, `/api/v1/plugin/secrets/${key}`)
        assert.deepEqual(body, { version: 0, change: { operation: 'replace', value: { plaintext: ' test-only-plugin-secret ' } } })
        secretStatus = { configured: true, version: 1 }
        return { errorNo: 0, data: secretStatus }
      },
      post: async (path, body) => {
        requests.push({ path, body })
        assert.equal(path, '/api/v1/config/plugins/validate')
        return { errorNo: 0, data: { valid: true } }
      }
    }
    const { page, dispose } = fixture(() => {}, key, { http })
    await page.loadAll()
    assert.match(page.secretField.value.description, /^未设置/)
    await page.validateConfig()
    assert.deepEqual(requests.at(-1).body.secretChanges, {})
    page.updateSecret(' test-only-plugin-secret ')
    assert.equal(page.hasChanges.value, true)
    assert.equal(page.hasConfigChanges.value, false)
    assert.equal(page.editorText.value.includes('test-only-plugin-secret'), false)
    await page.validateConfig()
    assert.equal(requests.at(-1).path, '/api/v1/config/plugins/validate')
    assert.deepEqual(requests.at(-1).body.secretChanges, {})
    for (const id of Object.keys(original.plugins)) {
      assert.deepEqual(requests.at(-1).body.payload.plugins[id].settings, original.plugins[id].settings)
      assert.equal(requests.at(-1).body.payload.plugins[id].secretStatus, undefined)
      assert.equal(requests.at(-1).body.payload.plugins[id].enabled, false)
    }
    assert.equal(await page.saveAndApply(), false)
    assert.equal(await page.savePluginSecret(), true)
    assert.equal(page.secretValue.value, '')
    assert.equal(page.hasChanges.value, false)
    assert.equal(page.pendingCandidate.value, null)
    assert.deepEqual(active, original)
    assert.match(page.secretField.value.description, /^已设置/)
    await page.validateConfig()
    assert.deepEqual(requests.at(-1).body.secretChanges, {})
    page.updateSecret('discard-on-leave')
    dispose()
    assert.equal(page.secretValue.value, '')

    const readonly = fixture(() => {}, key, { http, hasCapability: () => false }).page
    await readonly.loadAll()
    const previousRequests = requests.length
    readonly.updateSecret('forbidden')
    await readonly.savePluginSecret()
    assert.equal(readonly.secretValue.value, '')
    assert.equal(requests.length, previousRequests)
  })
}

test('插件密钥状态读取失败禁止保存，冲突后保留输入并显示错误', async () => {
  let available = false
  let calls = 0
  const { page } = fixture(async path => {
    if (path.includes('/plugin/secrets/')) {
      if (!available) throw new Error('状态读取失败')
      return { errorNo: 0, data: { configured: true, version: 3 } }
    }
    return { errorNo: 0, data: path.endsWith('/status') ? { activeGeneration: 1 } : { plugins: { telegram_push: { enabled: false, settings: {} } } } }
  }, 'telegram_push', { http: { put: async () => {
    calls++
    return { errorNo: 100, errorMsg: '配置已被其他操作更新，请刷新后重试' }
  } } })
  await page.loadAll()
  page.updateSecret('blocked')
  assert.equal(await page.savePluginSecret(), false)
  assert.equal(calls, 0)
  assert.equal(page.secretValue.value, '')
  available = true
  await page.loadAll()
  page.updateSecret('test-only-secret')
  assert.equal(await page.savePluginSecret(), false)
  assert.equal(calls, 1)
  assert.equal(page.secretValue.value, 'test-only-secret')
  assert.equal(page.pluginSecretStatus.value.version, 3)
  assert.match(page.saveError.value, /配置已被其他操作更新/)
  assert.equal(page.busy.value, false)
})

test('插件密钥保存近期认证后重试一次，并阻止并发保存与离页', async () => {
  const waiting = deferred()
  let writes = 0
  let reauth = 0
  const { page } = fixture(async path => ({ errorNo: 0, data: path.includes('/plugin/secrets/')
    ? { configured: false, version: 0 }
    : path.endsWith('/status') ? { activeGeneration: 1 } : { plugins: { telegram_push: { enabled: false, settings: {} } } } }), 'telegram_push', {
    prompt: () => waiting.promise,
    http: {
      put: async () => {
        writes++
        return writes === 1 ? { errorNo: 405, data: { code: 'RECENT_AUTH_REQUIRED' } }
          : { errorNo: 0, data: { configured: true, version: 1 } }
      },
      post: async path => {
        assert.equal(path, '/api/v1/security/reauth')
        reauth++
        return { errorNo: 0 }
      }
    }
  })
  await page.loadAll()
  page.updateSecret('test-only-secret')
  const save = page.savePluginSecret()
  assert.equal(page.busy.value, true)
  assert.equal(await page.savePluginSecret(), false)
  assert.equal(await page.beforeLeave(), false)
  page.updateSecret('concurrent-edit')
  assert.equal(page.secretValue.value, 'test-only-secret')
  waiting.resolve({ value: 'test-only-password' })
  assert.equal(await save, true)
  assert.equal(writes, 2)
  assert.equal(reauth, 1)
  assert.equal(page.secretValue.value, '')
  assert.equal(page.busy.value, false)
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
  const mail = {
    listener: { port: 25, starttlsPort: 587, tlsPort: 0, security: 'starttls' },
    relay: {
      relayHost: 'smtp.example.com', relayPort: 587, security: 'starttls',
      from: 'sender@example.com', fromDomain: 'example.com'
    },
    imap: { tlsPort: 143 }, pop3: { port: 110, tlsPort: 0 }, policy: {}
  }
  const { page } = fixture(async () => ({ errorNo: 0, data: mail }), 'mail', {
    http: { post: async (path, body) => { request = body; return { errorNo: 0, data: { valid: true } } } }
  })
  await page.loadSection()
  page.updateField(['relay', 'password'], ' smtp-test-only ')
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
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }), 'mail', {
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

test('邮件服务、邮件策略和安全策略页共享候选并明确提示改动范围，忽略代次与字段顺序', async () => {
  const active = {
    meta: { generation: 1 }, listener: { port: 25, starttlsPort: 587, tlsPort: 465 },
    relay: { verificationEnabled: true, loginPolicy: 'new_device' },
    imap: { tlsPort: 993 }, pop3: { port: 110, tlsPort: 995 },
    policy: { maxMessageBytes: 1234, externalRecoveryEnabled: true }
  }
  for (const key of ['mail', 'mail_policy', 'security_policy']) {
    const { page } = fixture(async () => ({ errorNo: 0, data: active }), key)
    await page.loadSection()
    page.pendingCandidate.value = { section: 'mail', payload: { ...active, meta: { generation: 2 }, pop3: { tlsPort: 995, port: 110 } } }
    assert.match(page.pendingScopeHint.value, /邮件配置未变化/)
    page.pendingCandidate.value.payload = { ...active, pop3: { port: 1110, tlsPort: 1995 }, policy: { maxMessageBytes: 5678 } }
    assert.match(page.pendingScopeHint.value, /上次未完成的保存涉及：POP3 监听、邮件策略。/)
    assert.match(page.pendingScopeHint.value, /邮件服务、邮件策略和安全策略共用一组运行配置/)
    page.pendingCandidate.value = { section: 'smtp' }
    assert.equal(page.pendingScopeHint.value, '')
    page.pendingCandidate.value = { section: 'mail' }
    assert.match(page.pendingScopeHint.value, /上次未完成保存的内容尚未完整读取/)
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
          : path.endsWith('/pending') ? pending : path.includes('/plugin/secrets/') ? { configured: false, version: 0 } : active }
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
    assert.deepEqual(requests, ['/api/v1/config/status', '/api/v1/config/plugins',
      ...(key === 'spam_block' ? [] : [`/api/v1/plugin/secrets/${key}`]), '/api/v1/config/pending'])
    assert.equal(page.selectedSection.value, key)
    page.updateField(['plugins', key, 'enabled'], true)
    await page.validateConfig()
    await page.saveConfig()
    assert.ok(requests.includes('/api/v1/config/plugins/validate'))
    assert.match(page.pendingScopeHint.value, new RegExp(`上次未完成的保存涉及：${page.currentSection.value.label}。`))
    const other = Object.keys(original.plugins).find(id => id !== key)
    page.updateField(['plugins', other, 'enabled'], true)
    assert.equal(page.formConfig.value.plugins[other].enabled, false)
    dispose()
    const restored = fixture(() => {}, other, { http }).page
    await restored.loadAll()
    assert.equal(restored.savedGeneration.value, 2)
    assert.match(restored.pendingScopeHint.value, /三个插件共用一组运行配置/)
    assert.ok(restored.pendingScopeHint.value.includes(page.currentSection.value.label))
    await restored.applyConfig()
    assert.ok(requests.includes('/api/v1/config/plugins/apply'))
    assert.equal(restored.savedGeneration.value, 0)
    assert.deepEqual(messages, [])
  })
}
