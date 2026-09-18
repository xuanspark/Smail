import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const script = readFileSync(new URL('../../frontend/src/views/PublicPasswordReset/useRecoveryFlow.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  script.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'reactive', 'ref', 'watch', 'onBeforeUnmount', 'isValidPasswordLength', 'postPublicRecovery', 'clearInterval', 'setInterval']
)

function fixture(t) {
  let respond = async () => ({ ok: true, data: { role: 'user', token: 'flow', email: 'a***@example.com', methods: ['email', 'security_code'] } })
  const requests = []
  const page = createPage(computed, reactive, ref, () => {}, fn => t.after(fn), value => value.length >= 8 && value.length <= 12,
    async (endpoint, payload) => { requests.push({ endpoint, payload }); return respond(endpoint, payload) }, () => {}, () => 1)
  return { page, requests, respond: fn => { respond = fn } }
}

test('识别账号不会发送邮件，邮件验证通过后才可提交密码', async t => {
  const { page, requests, respond } = fixture(t)
  page.form.identifier = 'alice'
  await page.requestReset()
  assert.equal(page.page.value, 'verify')
  assert.deepEqual(requests.map(item => item.endpoint), ['begin'])
  respond(async () => ({ ok: true, data: { sent: true } }))
  await page.sendCode()
  assert.equal(page.countdown.value, 60)
  respond(async () => ({ ok: false, status: 400, data: { message: '验证码错误' } }))
  page.form.code = '12345678'
  await page.verifyIdentity()
  assert.equal(page.page.value, 'verify')
  respond(async () => ({ ok: true, data: { token: 'verified' } }))
  await page.verifyIdentity()
  assert.equal(page.page.value, 'reset')
  assert.equal(page.form.code, '')
  Object.assign(page.form, { newPassword: 'NewPass123', confirmPassword: 'NewPass123' })
  respond(async () => ({ ok: false, status: 503, data: { message: '稍后重试' } }))
  await page.completeReset()
  assert.equal(page.form.newPassword, 'NewPass123')
  assert.equal(page.page.value, 'reset')
  respond(async () => ({ ok: true, data: { reset: true } }))
  await page.completeReset()
  assert.equal(requests.at(-1).payload.token, 'verified')
  assert.equal(page.page.value, 'success')
  assert.equal(page.form.newPassword, '')
})

test('超级管理员进入禁止页面，发送失败不显示发送成功', async t => {
  const { page, respond } = fixture(t)
  page.form.identifier = 'admin'
  respond(async () => ({ ok: true, data: { role: 'super_admin', methods: [] } }))
  await page.requestReset()
  assert.equal(page.page.value, 'blocked')
  page.goRequest()
  respond(async () => ({ ok: false, status: 503, data: { message: '服务不可用' } }))
  await page.requestReset()
  assert.equal(page.page.value, 'request')
  assert.equal(page.notice.value, '')
  assert.match(page.errorMessage.value, /服务不可用/)
})

test('更换账号后迟到的响应不会推进重置步骤', async t => {
  const { page, respond } = fixture(t)
  let finish
  respond(() => new Promise(resolve => { finish = resolve }))
  page.form.identifier = 'alice'
  const pending = page.requestReset()
  page.goRequest()
  finish({ ok: true, data: { role: 'user', token: 'old', methods: ['email'] } })
  await pending
  assert.equal(page.page.value, 'request')
  assert.equal(page.identity.value, null)
})
