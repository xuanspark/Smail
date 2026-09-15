import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const Vue = require('vue')
const { parse, compileScript } = require('@vue/compiler-sfc')
const read = extension => readFileSync(new URL(`../../frontend/src/views/components/ThirdPartyAuthorization/ThirdPartyAuthorization.${extension}`, import.meta.url), 'utf8')

test('客户端授权码页面的 Vue 模板和脚本可编译', () => {
  const { descriptor, errors } = parse(read('vue'))
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'audit-protocol', inlineTemplate: true }))
})

test('客户端授权码接口与界面能力保持一致', () => {
  const source = read('js')
  const template = read('vue')
  for (const path of [
    '/api/v1/security/credentials/clients',
    '/api/v1/security/credentials/clients/create',
    '/api/v1/security/credentials/clients/regenerate',
    '/api/v1/security/credentials/clients/revoke',
    '/api/v1/security/credentials/clients/reveal',
  ]) {
    assert.match(source, new RegExp(path.replaceAll('/', '\\/')))
  }
  assert.match(template, /安全验证查看明文/)
  assert.match(source, /startRevealCountdown\(60\)/)
  assert.match(template, /自动隐藏/)
  assert.match(template, /撤销删除/)
})
