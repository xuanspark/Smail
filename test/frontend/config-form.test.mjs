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

test('五个分区使用分组表单，控件和样式能编译且不再提供 JSON 编辑器', () => {
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
  for (const section of ['base', 'smtp', 'email', 'ssl', 'plugins']) {
    const groups = getFieldGroups(section, { plugins: { spam_block: { settings: null } } })
    assert.ok(groups.length)
    const keys = groups.flatMap(group => group.fields.map(field => field.path.join('.')))
    assert.equal(keys.length, new Set(keys).size)
    assert.ok(groups.every(group => /[\u4e00-\u9fff]/.test(group.title)))
    assert.ok(!keys.some(key => /generation|password|encryptionKey|capabilities/.test(key)))
  }
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

test('开关、数字、空数字和列表保持真实类型，代理 HTTPS 正确联动', () => {
  let text = JSON.stringify({ http: { tlsMode: 'direct', httpsEnabled: true, httpsPort: 443 } })
  text = updateFormConfig(text, 'base', ['http', 'tlsMode'], 'proxy')
  assert.deepEqual(readFormConfig(text).http, { tlsMode: 'proxy', httpsEnabled: false, httpsPort: 0 })
  assert.equal(updateFormConfig(text, 'base', ['http', 'httpsEnabled'], true), text)
  text = updateFormConfig(text, 'base', ['security', 'externalEmailEnabled'], true)
  assert.equal(readFormConfig(text).security.externalEmailEnabled, true)
  text = updateFormConfig(text, 'base', ['http', 'port'], 8080)
  assert.equal(readFormConfig(text).http.port, 8080)
  text = updateFormConfig(text, 'base', ['http', 'port'], undefined)
  assert.equal(readFormConfig(text).http.port, null)
  assert.ok(validateFormConfig('base', readFormConfig(text))['http.port'])
  for (const value of ['25', 0, -1, 65536, 1.5, null]) {
    assert.ok(validateFormConfig('email', { policy: { smtpPort: value, imapPort: 143, pop3Port: 110 } })['policy.smtpPort'])
  }
  assert.ok(validateFormConfig('email', { policy: { smtpPort: 143, imapPort: 143, pop3Port: 110 } })['policy.imapPort'])
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
