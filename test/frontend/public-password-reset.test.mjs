import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const script = readFileSync(new URL('../../frontend/src/views/PublicPasswordReset/PublicPasswordReset.js', import.meta.url), 'utf8')
const template = readFileSync(new URL('../../frontend/src/views/PublicPasswordReset/PublicPasswordReset.vue', import.meta.url), 'utf8')

test('旧链接模块保留兼容代码，公开页面改用分步验证入口', () => {
  assert.match(script, /!result\.ok \|\| result\.data\?\.available !== true/)
  assert.match(template, /useRecoveryFlow\.js/)
  assert.match(template, /page === 'verify'/)
})

const createPage = compileFunction(
  script.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'reactive', 'ref', 'onMounted', 'onBeforeUnmount', 'isValidPasswordLength', 'postPublicRecovery', 'window']
)

async function fixture(t, requirements = { ok: true, status: 200, data: { available: true, requires_recovery_code: true } }) {
  let mounted
  let response = async () => ({ ok: true, status: 200, data: { reset: true } })
  const requests = []
  const page = createPage(computed, reactive, ref, callback => { mounted = callback }, callback => t.after(callback), () => true,
    async (endpoint, payload) => {
      requests.push({ endpoint, payload })
      return endpoint === 'requirements' ? requirements : response()
    }, {
      location: { hash: '#/reset-password/retry-token' },
      addEventListener() {},
      removeEventListener() {}
    })
  mounted()
  await new Promise(resolve => setImmediate(resolve))
  Object.assign(page.form, { newPassword: 'NewPass123', confirmPassword: 'NewPass123', recoveryCode: 'offline-code' })
  return { page, requests, respond: value => { response = value } }
}

test('匿名恢复繁忙保留链接和输入，成功重试后清空秘密', async t => {
  const { page, requests, respond } = await fixture(t)
  respond(async () => ({ ok: false, status: 503, data: { reset: false } }))
  for (let index = 0; index < 12; index++) {
    await page.completeReset()
    assert.equal(page.page.value, 'reset')
    assert.equal(page.form.newPassword, 'NewPass123')
    assert.equal(page.form.recoveryCode, 'offline-code')
    assert.equal(page.loading.value, false)
    assert.match(page.errorMessage.value, /稍后重试/)
    assert.doesNotMatch(page.errorMessage.value, /已失效/)
  }
  respond(async () => ({ ok: true, status: 200, data: { reset: true } }))
  await page.completeReset()
  assert.equal(requests.at(-1).payload.token, 'retry-token')
  assert.equal(page.page.value, 'success')
  assert.equal(page.form.newPassword, '')
  assert.equal(page.form.recoveryCode, '')
})

test('匿名恢复服务临时不可用不误报链接失效，也不显示密码表单', async t => {
  const { page } = await fixture(t, { ok: false, status: 503, data: {} })
  assert.equal(page.available.value, false)
  assert.match(page.errorMessage.value, /稍后刷新重试/)
  assert.doesNotMatch(page.errorMessage.value, /已失效/)
})

test('匿名恢复网络错误不声称已重置，并保留重试输入', async t => {
  const { page, respond } = await fixture(t)
  respond(async () => { throw new Error('network') })
  await page.completeReset()
  assert.equal(page.page.value, 'reset')
  assert.equal(page.form.newPassword, 'NewPass123')
  assert.match(page.errorMessage.value, /无法确认/)
})

test('匿名恢复请求保留繁忙状态码，不携带会话或启用缓存', async () => {
  const transport = readFileSync(new URL('../../frontend/src/utils/publicRecoveryHttp.js', import.meta.url), 'utf8')
  const createTransport = compileFunction(
    transport.replace('export async function', 'async function') + '\nreturn postPublicRecovery', ['fetch']
  )
  const post = createTransport(async (url, options) => {
    assert.equal(url, '/rest/api/recovery/complete')
    assert.equal(options.credentials, 'omit')
    assert.equal(options.cache, 'no-store')
    assert.equal(options.referrerPolicy, 'no-referrer')
    return { ok: false, status: 503, json: async () => ({ reset: false }) }
  })
  const result = await post('complete', { token: 'test-token' })
  assert.equal(result.ok, false)
  assert.equal(result.status, 503)
  assert.equal(result.data.reset, false)
})
