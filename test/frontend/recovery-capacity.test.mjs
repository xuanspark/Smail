import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref, watch } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/PasswordRecovery/PasswordRecovery.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'reactive', 'ref', 'watch', 'onBeforeUnmount', 'ElMessage', 'http', 'isValidPasswordLength']
)

async function fixture(t) {
  let response = async () => ({ errorNo: 0, data: { ticket: 'retry-ticket' } })
  const requests = []
  const page = createPage(computed, reactive, ref, watch, callback => t.after(callback),
    { success() {}, error() {}, warning() {}, info() {} },
    { post: async (path, payload) => {
      requests.push({ path, payload })
      if (requests.length === 1) {
        return { errorNo: 0, data: { requires_recovery_code: true, continuation: 'retry-continuation', expires_in: 300 } }
      }
      return response()
    } }, () => true)
  Object.assign(page.recovery, { identifier: 'alice', email: 'alice@example.com' })
  Object.assign(page.recovery, { challengeId: 'challenge', code: '123456' })
  await page.handleStepOneNext()
  assert.equal(page.adminRecoveryVisible.value, true)
  page.recovery.recoveryCode = 'offline-code'
  return { page, requests, respond: value => { response = value } }
}

for (const status of [408, 429, 500, 502, 503, 504, null]) {
  test(`附加恢复验证临时错误 ${status ?? '网络'} 不消耗尝试或清空凭证`, async t => {
    const { page, requests, respond } = await fixture(t)
    respond(async () => { throw Object.assign(new Error('temporary'), {
      isAxiosError: true, response: status === null ? undefined : { status }
    }) })
    for (let index = 0; index < 12; index++) {
      await page.confirmAdminVerification()
      assert.equal(page.adminRecoveryVisible.value, true)
      assert.equal(page.recovery.recoveryCode, 'offline-code')
      assert.equal(page.loading.value, false)
      assert.match(page.adminRecoveryError.value, /稍后重试/)
    }
    respond(async () => ({ errorNo: 0, data: { ticket: 'retry-ticket' } }))
    await page.confirmAdminVerification()
    assert.equal(requests.at(-1).payload.continuation, 'retry-continuation')
    assert.equal(page.currentStep.value, 2)
  })
}

test('真实恢复码错误仍受五次前端尝试限制', async t => {
  const { page, respond } = await fixture(t)
  respond(async () => ({ errorNo: 400 }))
  for (let index = 0; index < 5; index++) {
    page.recovery.recoveryCode = 'wrong-code'
    await page.confirmAdminVerification()
    assert.equal(page.recovery.recoveryCode, '')
    assert.equal(page.adminRecoveryVisible.value, index < 4)
  }
  assert.match(page.verificationState.value, /重新获取/)
})

test('设置新密码繁忙保留票据，成功后才清空密码', async t => {
  const { page, requests, respond } = await fixture(t)
  await page.confirmAdminVerification()
  Object.assign(page.recovery, { newPassword: 'NewPass123', confirmPassword: 'NewPass123' })
  respond(async () => { throw Object.assign(new Error('繁忙'), { response: { status: 503 } }) })
  await page.handleStepTwoNext()
  assert.equal(page.currentStep.value, 2)
  assert.equal(page.recovery.newPassword, 'NewPass123')
  respond(async () => ({ errorNo: 0, data: { reset: true } }))
  await page.handleStepTwoNext()
  assert.equal(requests.at(-1).payload.ticket, 'retry-ticket')
  assert.equal(page.currentStep.value, 3)
  assert.equal(page.recovery.newPassword, '')
})
