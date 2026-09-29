import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

// 将真实 Vue 客户端授权码页面回归纳入默认前端测试，避免只校验静态文案。
import '../review/protocol-frontend.test.mjs'

const root = new URL('../../frontend/src/views/components/ThirdPartyAuthorization/', import.meta.url)
const read = extension => readFileSync(new URL(`ThirdPartyAuthorization.${extension}`, root), 'utf8')

test('客户端账号信息严格使用后端允许的 POST 方法读取', () => {
  const source = read('js')
  assert.match(source, /http\.post\('\/api\/v1\/user\/info', \{\}\)/)
  assert.doesNotMatch(source, /http\.get\('\/api\/v1\/user\/info'\)/)
})

test('邮件客户端文案明确区分专属授权码与完整邮箱登录', () => {
  const template = read('vue')
  assert.match(template, /客户端专属授权码/)
  assert.match(template, /请使用完整邮箱地址登录，密码统一使用上方专属授权码/)
  assert.match(template, /IMAP[\s\S]*?\u6536\u4fe1\u670d\u52a1/)
  assert.match(template, /POP3[\s\S]*?\u6536\u4fe1\u670d\u52a1/)
  assert.match(template, /SMTP[\s\S]*?\u53d1\u4fe1\u670d\u52a1/)
  assert.doesNotMatch(template, /第三方邮件|&#x[0-9a-f]+;/i)
})
