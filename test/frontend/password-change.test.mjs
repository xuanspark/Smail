import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/components/SecuritySettings/SecuritySettings.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import[\s\S]*?from .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'reactive', 'ref', 'watch', 'onMounted', 'onUnmounted', 'http', 'useGlobalStatusStore', 'webAuthnSupported', 'lang', 'ElNotification', 'TextEncoder', 'location', 'setInterval', 'clearInterval']
)

function fixture(role = 'super_admin', post) {
  const calls = [], messages = [], redirects = []
  const store = reactive({ role, userInfos: { account: 'root' }, securityActiveSection: 'password', hasCapability: () => false, clear() { this.userInfos = {} } })
  const page = createPage(computed, reactive, ref, () => {}, () => {}, () => {}, {
    post: async (url, body) => {
      calls.push({ url, body })
      return post ? post(url, body) : { errorNo: 0, data: { id: 'challenge', email: 'o***@example.com' } }
    },
  }, () => store, () => false, {}, value => messages.push(value), TextEncoder,
  { assign: value => redirects.push(value) }, () => 1, () => {})
  page.externalEmails.value = [{ id: 1, type: 'primary', status: 'verified', email: 'o***@example.com' }]
  Object.assign(page.ruleForm, { new_pwd: 'NewPass123', new_pwd2: 'NewPass123' })
  return { page, calls, messages, redirects, store }
}

test('超级管理员显示修改密码入口，验证码申请不提交脱敏地址，倒计时阻止重复发送', async () => {
  const { page, calls } = fixture()
  assert.ok(page.sectionOptions.value.some(item => item.value === 'password'))
  await page.sendPasswordCode()
  await page.sendPasswordCode()
  assert.equal(calls.length, 1)
  assert.equal(calls[0].body.purpose, 'password_change')
  assert.equal(calls[0].body.account, 'root')
  assert.equal(calls[0].body.email, undefined)
  assert.equal(page.passwordCountdown.value, 60)
})

test('超级管理员凭邮箱验证码修改密码，成功后清除身份并重新登录', async () => {
  const { page, calls, redirects, store } = fixture()
  await page.sendPasswordCode()
  page.ruleForm.code = '12345678'
  await page.submit()
  assert.equal(calls[1].body.challenge_id, 'challenge')
  assert.equal(calls[1].body.current_password, undefined)
  assert.equal(calls[1].body.code, '12345678')
  assert.deepEqual(redirects, ['/login'])
  assert.equal(Object.keys(store.userInfos).length, 0)
})

test('普通用户和管理员维持当前密码验证', async () => {
  for (const role of ['user', 'admin']) {
    const { page, calls } = fixture(role)
    page.ruleForm.current_pwd = 'OldPass123'
    await page.submit()
    assert.equal(calls[0].body.current_password, 'OldPass123')
    assert.equal(calls[0].body.code, undefined)
  }
})

test('无已验证安全邮箱、未获取验证码、密码不一致时不能提交', async () => {
  const { page, calls } = fixture()
  await page.submit()
  page.externalEmails.value = []
  await page.sendPasswordCode()
  page.ruleForm.new_pwd2 = 'OtherPass1'
  await page.submit()
  assert.equal(calls.length, 0)
})

test('安全邮箱优先主要邮箱，没有主要邮箱时允许已验证备用邮箱', async () => {
  const { page, calls } = fixture()
  const backup = { id: 2, type: 'backup', status: 'verified', email: 'b***@example.com' }
  const primary = page.externalEmails.value[0]
  page.externalEmails.value = [backup, primary]
  assert.equal(page.passwordEmail.value.id, 1)
  page.externalEmails.value = [backup, { ...primary, status: 'disabled' }]
  assert.equal(page.passwordEmail.value.id, 2)
  await page.sendPasswordCode()
  assert.equal(calls.length, 1)
  page.externalEmails.value = [{ ...backup, status: 'pending' }]
  assert.equal(page.passwordEmail.value, undefined)
})

test('请求失败不虚报成功、不登出且释放提交状态', async () => {
  const { page, messages, redirects, store } = fixture('admin', async () => { throw new Error('服务暂不可用') })
  page.ruleForm.current_pwd = 'OldPass123'
  await page.submit()
  assert.equal(messages.at(-1).type, 'error')
  assert.equal(page.passwordSubmitting.value, false)
  assert.equal(redirects.length, 0)
  assert.equal(store.userInfos.account, 'root')
})
