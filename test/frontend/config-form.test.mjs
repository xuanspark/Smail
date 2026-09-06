import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'

const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
const source = readFileSync(new URL('ConfigForm/ConfigForm.js', root), 'utf8')
const { readFormConfig, getFieldGroups, updateFormConfig, validateFormConfig } = compileFunction(
  source.replaceAll('export function ', 'function ') + '\nreturn { readFormConfig, getFieldGroups, updateFormConfig, validateFormConfig }'
)()

test('站点安全配置不再依赖旧全局开关，用户权限仍由账号设置管理', () => {
  const config = { security: { webAuthnEnabled: false, externalEmailEnabled: false } }
  const groups = getFieldGroups('base', config)
  const security = groups.find(group => group.id === 'security')
  assert.ok(security)
  const keys = security.fields.map(field => field.path.join('.'))
  assert.deepEqual(keys, ['security.webAuthnRpId', 'security.webAuthnRpDisplayName', 'security.webAuthnRpOrigins'])
  const updated = readFormConfig(updateFormConfig(JSON.stringify(config), 'base', ['security', 'webAuthnRpId'], 'mail.example.com'))
  assert.equal(updated.security.webAuthnRpId, 'mail.example.com')
  assert.equal(updated.security.webAuthnEnabled, false)
  assert.equal(updated.security.externalEmailEnabled, false)
})

test('七个页面使用分组表单，控件和样式能编译且不再提供 JSON 编辑器', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  for (const name of ['ConfigForm', 'ConfigField', 'ConfigEditor']) {
    const template = readFileSync(new URL(`${name}/${name}.vue`, root), 'utf8')
    const css = readFileSync(new URL(`${name}/${name}.css`, root), 'utf8')
    const { descriptor, errors } = parse(template)
    assert.deepEqual(errors, [])
    assert.doesNotThrow(() => compileScript(descriptor, { id: name, inlineTemplate: true }))
    assert.deepEqual(compileStyle({ source: css, id: `data-v-${name}`, scoped: true }).errors, [])
    assert.doesNotMatch(template, /&#x[0-9a-f]+;|type="textarea"|配置 JSON 编辑器/i)
  }
  for (const section of ['base', 'smtp', 'imap', 'pop3', 'email', 'ssl', 'plugins']) {
    const groups = getFieldGroups(section, { plugins: { spam_block: { settings: null } } })
    assert.ok(groups.length)
    const keys = groups.flatMap(group => group.fields.map(field => field.path.join('.')))
    assert.equal(keys.length, new Set(keys).size)
    assert.ok(groups.every(group => /[\u4e00-\u9fff]/.test(group.title)))
    assert.ok(!keys.some(key => /generation|password|encryptionKey|capabilities/.test(key)))
  }
})

test('各协议仅编辑自己的监听端口，邮箱策略没有重复端口输入项', () => {
  const config = { listener: { port: 25, starttlsPort: 587, tlsPort: 465 }, relay: { relayPort: 465 }, imap: { tlsPort: 993 }, pop3: { port: 110, tlsPort: 995 }, policy: { smtpPort: 25, imapPort: 143, pop3Port: 110 } }
  const text = JSON.stringify(config)
  assert.equal(updateFormConfig(text, 'email', ['policy', 'smtpPort'], 2525), text)
  assert.equal(updateFormConfig(text, 'imap', ['pop3', 'port'], 1110), text)
  assert.equal(updateFormConfig(text, 'pop3', ['imap', 'tlsPort'], 1993), text)
  const edited = readFormConfig(updateFormConfig(text, 'smtp', ['listener', 'tlsPort'], 2465))
  assert.equal(edited.listener.tlsPort, 2465)
  assert.equal(edited.relay.relayPort, 465)
  assert.deepEqual(getFieldGroups('email', config).flatMap(group => group.fields).filter(field => /port/i.test(field.path.join('.'))), [])
  assert.ok(validateFormConfig('smtp', { ...config, listener: { ...config.listener, tlsPort: 587 } })['listener.tlsPort'])
})

test('三个插件页面只展示和编辑自身字段，完整保留其他插件和未知字段', () => {
  const config = { meta: { generation: 9 }, plugins: {
    spam_block: { enabled: false, settings: { threshold: 20 }, secretStatus: { configured: true } },
    telegram_push: { enabled: false, settings: { timeout: 30, futureField: '保留值' } },
    wechat_push: { enabled: true, settings: { appId: 'test-only' } },
    future_plugin: { enabled: false, settings: ['原样保留'] }
  } }
  const text = JSON.stringify(config)
  for (const id of ['spam_block', 'telegram_push', 'wechat_push']) {
    const groups = getFieldGroups(id, config)
    assert.equal(groups.length, 1)
    assert.equal(groups[0].id, id)
    assert.ok(groups[0].fields.every(field => field.path[1] === id))
    const edited = readFormConfig(updateFormConfig(text, id, ['plugins', id, 'enabled'], true))
    for (const other of Object.keys(config.plugins).filter(key => key !== id)) {
      assert.deepEqual(edited.plugins[other], config.plugins[other])
      assert.equal(updateFormConfig(text, id, ['plugins', other, 'enabled'], true), text)
    }
    assert.deepEqual(edited.meta, config.meta)
    assert.deepEqual(edited.plugins[id].settings, config.plugins[id].settings)
  }
  assert.deepEqual(getFieldGroups('wechat_push', { plugins: {} }), [])
  const invalidOther = structuredClone(config)
  invalidOther.plugins.telegram_push.settings.timeout = 999
  assert.deepEqual(validateFormConfig('spam_block', invalidOther), {})
  assert.ok(validateFormConfig('telegram_push', invalidOther)['plugins.telegram_push.settings.timeout'])
})

test('文本输入的左右留白属于原生输入框，不影响数字、选择和开关控件', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileStyle } = require('@vue/compiler-sfc')
  const postcss = require('postcss')
  const template = readFileSync(new URL('ConfigField/ConfigField.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigField/ConfigField.css', root), 'utf8')
  const { descriptor } = parse(template)
  const ast = require('@vue/compiler-dom').parse(descriptor.template.content)
  const inputs = []
  const otherControls = []
  const visit = node => {
    if (node.tag === 'el-input') inputs.push(node)
    if (['el-input-number', 'el-select', 'el-switch'].includes(node.tag)) otherControls.push(node)
    for (const child of node.children || []) visit(child)
  }
  visit(ast)
  const classes = node => node.props.find(prop => prop.type === 6 && prop.name === 'class')?.value?.content || ''
  assert.equal(inputs.length, 3)
  assert.ok(inputs.every(node => classes(node).split(/\s+/).includes('field-text-input')))
  assert.ok(otherControls.every(node => !classes(node).includes('field-text-input')))
  const compiled = compileStyle({ source: css, id: 'data-v-caret-test', scoped: true })
  assert.deepEqual(compiled.errors, [])
  const rules = new Map()
  postcss.parse(compiled.code).walkRules(rule => {
    if (rule.selector.startsWith('.field-text-input[data-v-caret-test] ')) {
      const declarations = {}
      rule.walkDecls(declaration => { declarations[declaration.prop] = declaration.value })
      rules.set(rule.selector, declarations)
    }
  })
  assert.deepEqual(rules.get('.field-text-input[data-v-caret-test] .el-input__wrapper'), { 'padding-inline': '0' })
  assert.deepEqual(rules.get('.field-text-input[data-v-caret-test] .el-input__inner'), { 'padding-inline': '11px' })
})

test('分组不显示重复标题和介绍，保留无障碍名称、凭据状态及异常提示', () => {
  const template = readFileSync(new URL('ConfigForm/ConfigForm.vue', root), 'utf8')
  const css = readFileSync(new URL('ConfigForm/ConfigForm.css', root), 'utf8')
  assert.match(template, /<h4>{{ group.title }}<\/h4>/)
  assert.match(template, /form-group-heading|form-group-description|group\.description/)
  assert.doesNotMatch(template, /aria-labelledby/)
  assert.match(template, /<section\b[^>]*:aria-label="group.title"/)
  assert.match(template, /v-if="group.secretConfigured !== undefined" class="credential-status"/)
  assert.match(template, /group.secretConfigured \? '凭据已配置' : '未配置凭据'/)
  assert.match(template, /v-if="group.note" class="form-group-note" role="note"/)
  assert.match(template, /v-if="!groups.length" class="form-empty"/)
  assert.match(template, /<ConfigField\b/)
  for (const binding of ['v-for="field in group.fields"', ':value="getValue(config, field.path)"', ':error="errors[fieldKey(field)]"', ':disabled="disabled"', "emit('change', path, value)"]) {
    assert.ok(template.includes(binding))
  }
  assert.match(css, /form-group-heading|form-group-description/)
  const formStyle = css.match(/\.config-form\s*\{([^}]+)\}/)?.[1] || ''
  assert.doesNotMatch(formStyle, /margin-top|padding-top/)
  assert.match(css, /\.config-form:not\(:first-child\)\s*\{\s*margin-top: 10px;/)
  assert.match(css, /\.form-group \+ \.form-group\s*\{[^}]*border-top:/)
})

test('单字段更新完整保留内部数据，锁定字段和污染路径无法写入', () => {
  const config = {
    generation: 8, schemaVersion: 2, database: { type: 'sqlite', localRef: 'test.db', password: null },
    domains: { primary: 'example.com', accepted: ['example.com'] },
    security: { passwordVersion: 'argon2id', encryptionKeyRef: 'system-managed' },
    future: { nested: [true, 1, null] }
  }
  const text = JSON.stringify(config, null, 2)
  const updated = readFormConfig(updateFormConfig(text, 'base', ['domains', 'primary'], 'mail.example.com'))
  assert.deepEqual(updated, { ...config, domains: { ...config.domains, primary: 'mail.example.com' } })
  for (const path of [['database', 'type'], ['database', 'localRef'], ['generation'], ['security', 'encryptionKeyRef'], ['__proto__', 'polluted'], ['constructor', 'prototype', 'polluted'], ['relay', 'relayHost']]) {
    assert.equal(updateFormConfig(text, 'base', path, 'changed'), text)
  }
  assert.equal({}.polluted, undefined)
})

test('基础配置隐藏额外访问主机、代理网段和数据库只读信息，保留内部值且无空分组', () => {
  for (const type of ['sqlite', 'mysql', 'postgresql']) {
    for (const tlsMode of ['direct', 'proxy']) {
      const config = {
        database: { type, localRef: 'preserved.db', name: 'smail', host: 'db.example.com', port: type === 'postgresql' ? 5432 : 3306, username: 'smail', password: null },
        domains: { primary: 'example.com', web: 'mail.example.com', accepted: ['example.com'], accessHosts: ['alternate.example.com', '192.0.2.10'] },
        http: { tlsMode, port: 8080, trustedProxyCidrs: ['127.0.0.1/32', '192.0.2.10/32'] },
        security: { externalEmailEnabled: false, webAuthnEnabled: false }
      }
      const text = JSON.stringify(config)
      const groups = getFieldGroups('base', config)
      const keys = groups.flatMap(item => item.fields.map(input => input.path.join('.')))
      const hidden = ['database.type', 'database.localRef', 'http.trustedProxyCidrs', 'domains.accessHosts']
      assert.ok(groups.every(item => item.fields.length > 0))
      assert.ok(hidden.every(key => !keys.includes(key)))
      assert.doesNotMatch(JSON.stringify(groups), /额外允许的访问主机|按需填写域名或 IP 地址/)
      assert.ok(keys.includes('domains.accepted'))
      assert.equal(groups.some(item => item.id === 'database'), type !== 'sqlite')
      for (const key of hidden) {
        assert.equal(updateFormConfig(text, 'base', key.split('.'), 'changed'), text)
      }
      for (const key of ['security.externalEmailEnabled', 'security.webAuthnEnabled']) {
        assert.ok(!keys.includes(key))
        assert.equal(updateFormConfig(text, 'base', key.split('.'), true), text)
      }
      const updated = readFormConfig(updateFormConfig(text, 'base', ['http', 'port'], 9080))
      assert.deepEqual(updated, { ...config, http: { ...config.http, port: 9080 } })
      if (type !== 'sqlite') {
        for (const key of ['database.name', 'database.host', 'database.port', 'database.username']) {
          assert.ok(keys.includes(key))
        }
      }
    }
  }
})

test('开关、数字、空数字和列表保持真实类型，代理 HTTPS 正确联动', () => {
  const modeField = getFieldGroups('base', {}).flatMap(group => group.fields).find(field => field.path.join('.') === 'http.tlsMode')
  assert.match(modeField.hint, /不自动申请或续期 SSL 证书/)
  assert.match(modeField.hint, /邮件协议仍需有效的本地证书/)
  let text = JSON.stringify({ http: { tlsMode: 'direct', httpsEnabled: true, httpsPort: 443 } })
  text = updateFormConfig(text, 'base', ['http', 'tlsMode'], 'proxy')
  assert.deepEqual(readFormConfig(text).http, { tlsMode: 'proxy', httpsEnabled: false, httpsPort: 0, port: 8080 })
  assert.equal(updateFormConfig(text, 'base', ['http', 'httpsEnabled'], true), text)
  const direct = updateFormConfig(text, 'base', ['http', 'tlsMode'], 'direct')
  const disabled = readFormConfig(updateFormConfig(direct, 'base', ['http', 'httpsEnabled'], false))
  assert.equal(disabled.http.httpsEnabled, false)
  assert.equal(disabled.http.httpsPort, 0)
  text = updateFormConfig(text, 'base', ['http', 'port'], 8080)
  assert.equal(readFormConfig(text).http.port, 8080)
  text = updateFormConfig(text, 'base', ['http', 'port'], undefined)
  assert.equal(readFormConfig(text).http.port, null)
  assert.ok(validateFormConfig('base', readFormConfig(text))['http.port'])
  for (const value of ['25', 0, -1, 65536, 1.5, null]) {
    assert.ok(validateFormConfig('imap', { imap: { tlsPort: value } })['imap.tlsPort'])
  }
  assert.ok(validateFormConfig('pop3', { imap: { tlsPort: 993 }, pop3: { port: 993, tlsPort: 995 } })['pop3.port'])
  assert.deepEqual(validateFormConfig('email', { policy: { smtpPort: 25, imapPort: 143, pop3Port: 110 } }), {})
})

test('域名校验兼容大小写和末尾点，拒绝重复项、空白和路径', () => {
  const valid = { database: { type: 'sqlite' }, domains: { primary: 'Example.com.', web: 'mail.example.com', accepted: ['example.com'] } }
  assert.deepEqual(validateFormConfig('base', valid), {})
  for (const accepted of [[], ['example.com', 'Example.com.'], [''], [' https://example.com'], ['other.example.com'], 'example.com', [null]]) {
    assert.ok(validateFormConfig('base', { ...valid, domains: { ...valid.domains, accepted } })['domains.accepted'])
  }
  for (const primary of ['https://example.com', 'example.com/path', 'bad..com', '-bad.com', '127.0.0.1', '例子.com']) {
    assert.ok(validateFormConfig('base', { ...valid, domains: { ...valid.domains, primary } })['domains.primary'])
  }
})

test('插件网络列表拒绝无效和重复目标，空设置可编辑且非标准设置不覆盖', () => {
  const make = network => ({ plugins: { telegram_push: { permissions: { network }, settings: null } } })
  const valid = { scheme: 'https', host: 'api.example.com', port: 443 }
  assert.deepEqual(validateFormConfig('plugins', make([valid])), {})
  for (const network of [{}, [null], [{ ...valid, host: '127.0.0.1' }], [{ ...valid, host: 'bad..com' }], [{ ...valid, scheme: 'http' }], [{ ...valid, port: null }], [valid, { ...valid, host: 'API.EXAMPLE.COM.' }]]) {
    assert.ok(validateFormConfig('plugins', make(network))['plugins.telegram_push.permissions.network'])
  }
  const text = JSON.stringify(make([]))
  const updated = readFormConfig(updateFormConfig(text, 'plugins', ['plugins', 'telegram_push', 'settings', 'timeout'], 30))
  assert.equal(updated.plugins.telegram_push.settings.timeout, 30)
  const legacy = JSON.stringify({ plugins: { telegram_push: { settings: ['preserve'] } } })
  assert.equal(updateFormConfig(legacy, 'plugins', ['plugins', 'telegram_push', 'settings', 'timeout'], 30), legacy)
})

test('不同插件超时字段使用各自实际单位，不把毫秒当作秒', () => {
  const groups = getFieldGroups('plugins', { plugins: { spam_block: { settings: { timeout: 10000 } }, telegram_push: { settings: { timeout: 30 } } } })
  const spamTimeout = groups.find(group => group.id === 'spam_block').fields.find(field => field.path.at(-1) === 'timeout')
  const telegramTimeout = groups.find(group => group.id === 'telegram_push').fields.find(field => field.path.at(-1) === 'timeout')
  assert.equal(spamTimeout.label, '请求超时（毫秒）')
  assert.equal(telegramTimeout.label, '请求超时（秒）')
  assert.deepEqual(validateFormConfig('plugins', { plugins: { spam_block: { settings: { timeout: 10000 } } } }), {})
})

test('证书路径始终受管且引用成对，切回默认来源清除自定义目录', () => {
  const tls = { mode: 'managed_upload', defaultDirectory: 'config/ssl', customDirectory: 'config/ssl/imported', certificateRef: 'config/ssl/imported/cert.pem', privateKeyRef: 'config/ssl/imported/key.pem' }
  assert.deepEqual(validateFormConfig('ssl', { tls }), {})
  for (const customDirectory of ['C:/certs', '/etc/ssl', '../certs', 'config/../certs', 'config\\ssl', 'config//ssl']) {
    assert.ok(validateFormConfig('ssl', { tls: { ...tls, customDirectory } })['tls.customDirectory'])
  }
  assert.ok(validateFormConfig('ssl', { tls: { ...tls, privateKeyRef: '' } })['tls.privateKeyRef'])
  assert.ok(validateFormConfig('ssl', { tls: { ...tls, certificateRef: 'config/ssl/outside.pem' } })['tls.certificateRef'])
  const updated = readFormConfig(updateFormConfig(JSON.stringify({ tls }), 'ssl', ['tls', 'mode'], 'managed_default'))
  assert.equal(updated.tls.customDirectory, '')
  assert.equal(updated.tls.privateKeyRef, tls.privateKeyRef)
})

test('SMTP 加密方式自动匹配端口，手动端口和读取过程不改变原配置', () => {
  for (const [security, expectedPort] of [['starttls', 587], ['implicit_tls', 465], ['tls', 465], ['plain', 25]]) {
    const config = { generation: 9, relay: { relayHost: 'localhost', relayPort: 2525, future: true } }
    let text = JSON.stringify(config)
    getFieldGroups('smtp', config)
    assert.equal(JSON.stringify(config), text)
    text = updateFormConfig(text, 'smtp', ['relay', 'security'], security)
    assert.deepEqual(readFormConfig(text), { ...config, relay: { ...config.relay, security, relayPort: expectedPort } })
    const fields = getFieldGroups('smtp', readFormConfig(text)).flatMap(group => group.fields)
    assert.equal(fields.find(field => field.path.at(-1) === 'relayPort').recommendedPort, expectedPort)
    text = updateFormConfig(text, 'smtp', ['relay', 'relayPort'], 2526)
    assert.equal(readFormConfig(text).relay.security, security)
    text = updateFormConfig(text, 'smtp', ['relay', 'security'], security)
    assert.equal(readFormConfig(text).relay.relayPort, 2526)
  }
  const fields = getFieldGroups('smtp', {}).flatMap(group => group.fields)
  assert.ok(fields.findIndex(field => field.path.at(-1) === 'security') < fields.findIndex(field => field.path.at(-1) === 'relayPort'))
  const unused = JSON.stringify({ relay: { relayPort: 2525, security: 'starttls' } })
  assert.equal(readFormConfig(updateFormConfig(unused, 'smtp', ['relay', 'security'], '')).relay.relayPort, 2525)
  for (const security of ['constructor', '__proto__', 'future']) {
    const updated = readFormConfig(updateFormConfig(unused, 'smtp', ['relay', 'security'], security))
    assert.equal(updated.relay.relayPort, 2525)
    const fields = getFieldGroups('smtp', updated).flatMap(group => group.fields)
    assert.equal(fields.find(field => field.path.at(-1) === 'relayPort').recommendedPort, undefined)
  }
})

test('内置 HTTPS 自动补全端口，反代禁用内置监听，已有自定义 HTTP 端口保留', () => {
  let text = updateFormConfig('{}', 'base', ['http', 'tlsMode'], 'direct')
  assert.deepEqual(readFormConfig(text).http, { tlsMode: 'direct', port: 80, httpsEnabled: true, httpsPort: 443 })
  text = updateFormConfig(text, 'base', ['http', 'httpsEnabled'], false)
  assert.equal(readFormConfig(text).http.httpsPort, 0)
  assert.ok(!getFieldGroups('base', readFormConfig(text)).flatMap(group => group.fields).some(field => field.path.at(-1) === 'httpsPort'))
  text = updateFormConfig(text, 'base', ['http', 'httpsEnabled'], true)
  assert.equal(readFormConfig(text).http.httpsPort, 443)
  text = updateFormConfig(text, 'base', ['http', 'port'], 9080)
  text = updateFormConfig(text, 'base', ['http', 'tlsMode'], 'proxy')
  assert.deepEqual(readFormConfig(text).http, { tlsMode: 'proxy', port: 9080, httpsEnabled: false, httpsPort: 0 })
  assert.equal(updateFormConfig(text, 'base', ['http', 'httpsPort'], 8443), text)
  text = updateFormConfig(text, 'base', ['http', 'tlsMode'], 'direct')
  assert.equal(readFormConfig(text).http.port, 9080)
  assert.equal(readFormConfig(text).http.httpsPort, 443)
  const custom = JSON.stringify({ http: { httpsEnabled: false, httpsPort: 8443, port: 9080 } })
  assert.equal(readFormConfig(updateFormConfig(custom, 'base', ['http', 'tlsMode'], 'direct')).http.httpsPort, 8443)
})

test('加密配置校验拦截监听冲突、远程明文和已启用但未选择加密的中继', () => {
  for (const port of [80, 0]) {
    assert.ok(validateFormConfig('base', { http: { port, httpsEnabled: true, httpsPort: 80 } })['http.httpsPort'])
  }
  for (const relayHost of ['localhost', 'LOCALHOST', '127.0.0.1', '127.1.2.3', '::1', '0:0:0:0:0:0:0:1']) {
    assert.deepEqual(validateFormConfig('smtp', { relay: { relayHost, security: 'plain', relayPort: 25 } }), {})
  }
  for (const relayHost of ['smtp.example.com', '127.0.0.999', '127.1', '127.0.0.1.evil.com', '192.168.1.1']) {
    assert.ok(validateFormConfig('smtp', { relay: { relayHost, security: 'plain', relayPort: 25 } })['relay.relayHost'])
  }
  const errors = validateFormConfig('smtp', { relay: { verificationEnabled: true, relayHost: 'smtp.example.com', relayPort: 0 } })
  assert.ok(errors['relay.security'])
  assert.ok(errors['relay.relayPort'])
  assert.deepEqual(validateFormConfig('smtp', { relay: {} }), {})
  assert.deepEqual(validateFormConfig('smtp', { relay: { security: 'starttls', relayPort: 2525, relayHost: 'smtp.example.com' } }), {})
})
