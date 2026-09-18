import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/LoginVerification/LoginVerification.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'onBeforeUnmount', 'onMounted', 'ref', 'ElMessage', 'useRouter', 'useGlobalStatusStore', 'http', 'getSafeLoginRedirect', 'window']
)

function fixture({ statusFails = false } = {}) {
  const calls = []
  const pending = reactive({ challengeId: 'pending-id', account: 'alice', methods: ['email', 'totp'], deliveryStatus: 'idle', expiresAt: new Date(Date.now() + 600000).toISOString() })
  const store = {
    pendingLogin: pending,
    setPendingLogin: data => Object.assign(pending, { deliveryStatus: data.delivery_status, retryAt: data.retry_at }),
    clearPendingLogin: () => { store.pendingLogin = null },
    setUser: () => calls.push('session')
  }
  let mount
  let tick
  const page = createPage(computed, () => {}, callback => { mount = callback }, ref,
    { error() {}, warning() {} }, () => ({ replace: async () => {} }), () => store,
    { post: async (url, input, options) => {
      calls.push({ url, input, options })
      if (url.endsWith('/status')) {
        if (statusFails) throw new Error('status unavailable')
        return { errorNo: 0, data: { pending: true, delivery_status: 'sent' } }
      }
      if (url.endsWith('/resend')) return { errorNo: 0, data: { pending: true, delivery_status: 'sending', retry_at: new Date(Date.now() + 60000).toISOString() } }
      return { errorNo: 0, data: { account: 'alice' } }
    } }, () => '/', { setInterval: callback => { tick = callback; return 1 }, clearInterval() {} })
  mount()
  return { page, calls, tick: async () => { tick(); await new Promise(resolve => setImmediate(resolve)) } }
}

test('email code remains submittable when delivery status polling fails', async () => {
  const { page, calls, tick } = fixture({ statusFails: true })
  page.chooseMethod('email')
  assert.equal(page.canSubmit.value, false)
  await page.resendCode()
  await tick()
  assert.ok(page.errorMessage.value)
  assert.equal(page.sending.value, true)
  page.code.value = '12345678'
  assert.equal(page.canSubmit.value, true)
  await page.verifyCode()
  assert.equal(calls.filter(call => call.url?.endsWith('/resend')).length, 1)
  assert.ok(calls.includes('session'))
  assert.equal(calls.find(call => call.url?.endsWith('/verify')).input.method, 'email')
})

test('polling advances email delivery to sent without sending again', async () => {
  const { page, calls, tick } = fixture()
  page.chooseMethod('email')
  await page.resendCode()
  await tick()
  assert.equal(page.sending.value, false)
  assert.equal(page.pending.value.deliveryStatus, 'sent')
  assert.equal(page.errorMessage.value, '')
  assert.equal(calls.filter(call => call.url?.endsWith('/resend')).length, 1)
})

test('进入选择页和切换验证方式均不发信，只有点击发送按钮才请求一次', async () => {
  const { page, calls } = fixture()
  assert.deepEqual(calls, [])
  page.chooseMethod('email')
  page.chooseMethod('totp')
  page.chooseMethod('email')
  assert.deepEqual(calls, [])
  await page.resendCode()
  await page.resendCode()
  assert.equal(calls.length, 1)
  assert.equal(calls[0].url, '/api/v1/security/login/resend')
  assert.equal(calls[0].options.timeout, 4500)
})

test('选择 TOTP 后直接验证六位动态码，全程不请求发送邮件', async () => {
  const { page, calls } = fixture()
  page.chooseMethod('totp')
  page.code.value = '123456'
  await page.verifyCode()
  assert.equal(calls[0].url, '/api/v1/security/login/verify')
  assert.equal(calls[0].input.method, 'totp')
  assert.equal(calls[0].options.timeout, 4500)
  assert.equal(calls.some(call => call.url?.endsWith('/resend')), false)
})
