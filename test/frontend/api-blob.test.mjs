import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import test from 'node:test'
import { decodeBlobAPIError } from '../../frontend/src/utils/apiResponse.mjs'

const source = readFileSync(new URL('../../frontend/src/utils/axios.js', import.meta.url), 'utf8')
const createInterceptors = compileFunction(
  source.replace(/^import .*$/gm, '')
    .replace('import.meta.env.VITE_APP_URL', 'undefined')
    .replace('export {http};', ''),
  ['axios', 'lang', 'router', 'createLoginRedirectHandler', 'decodeBlobAPIError', 'useGlobalStatusStore']
)

function fixture() {
  let success
  let failure
  let redirects = 0
  const http = { interceptors: { response: { use: (left, right) => { success = left; failure = right } } } }
  createInterceptors({ create: () => http }, { lang: 'zh' }, {}, () => async () => { redirects++ }, decodeBlobAPIError, () => ({ clear() {} }))
  return { success, failure, redirects: () => redirects }
}

function response(body, status = 403) {
  return {
    data: new Blob([JSON.stringify(body)], { type: 'application/json; charset=utf-8' }),
    status,
    config: { url: '/api/v1/audits/config', responseType: 'blob' }
  }
}

test('HTTP 错误 Blob 保留最近认证错误码及中文提示，不跳转登录', async () => {
  const api = fixture()
  const res = response({ errorNo: 405, errorMsg: '请确认当前密码', data: { code: 'RECENT_AUTH_REQUIRED', requestId: 'test-request' } })
  const error = Object.assign(new Error('HTTP 403'), { response: res, config: res.config })
  await assert.rejects(api.failure(error), result => {
    assert.equal(result, error)
    assert.equal(result.apiErrorCode, 'RECENT_AUTH_REQUIRED')
    assert.equal(result.apiRequestId, 'test-request')
    assert.equal(result.message, '请确认当前密码')
    return true
  })
  assert.equal(api.redirects(), 0)
})

test('HTTP 200 包装的业务错误 Blob 同样能触发最近认证', async () => {
  const api = fixture()
  const result = await api.success(response({ errorNo: 405, data: { code: 'RECENT_AUTH_REQUIRED' } }, 200))
  assert.equal(result.data.code, 'RECENT_AUTH_REQUIRED')
  assert.equal(api.redirects(), 0)
})

test('真实 CSV 和 JSON 导出、损坏内容及过大内容不会被替换', async () => {
  const values = [
    new Blob(['a,b\n1,2'], { type: 'text/csv' }),
    new Blob(['[{"action":"update"}]'], { type: 'application/json' }),
    new Blob(['{"errorNo":0,"data":[]}'], { type: 'application/json' }),
    new Blob(['broken'], { type: 'application/json' }),
    new Blob([' '.repeat(1024 * 1024 + 1)], { type: 'application/json' })
  ]
  for (const value of values) {
    const res = { data: value }
    await decodeBlobAPIError(res)
    assert.equal(res.data, value)
  }
})

test('只有真实登录失效的 Blob 错误才跳转登录', async () => {
  const api = fixture()
  const res = response({ errorNo: 403, errorMsg: '请重新登录' })
  await assert.rejects(api.failure({ response: res, config: res.config }))
  assert.equal(api.redirects(), 1)
})

test('认证繁忙保留错误码和中文提示，不跳转或清除登录', async () => {
  const api = fixture()
  for (const path of ['/api/v1/login', '/api/v1/user/info', '/api/v1/security/recovery/verify']) {
    const res = response({ errorNo: 503, errorMsg: '认证服务暂时繁忙，请稍后重试', data: { code: 'AUTH_BUSY' } }, 503)
    res.config.url = path
    await assert.rejects(api.failure({ response: res, config: res.config }), result => {
      assert.equal(result.apiErrorCode, 'AUTH_BUSY')
      assert.match(result.message, /稍后重试/)
      return true
    })
    assert.equal(api.redirects(), 0)
  }
})
