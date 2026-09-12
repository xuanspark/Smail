import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { reactive, ref } from './vue.mjs'

// 将已有真实 Vue 开关事件回归纳入默认前端测试，避免只覆盖调用函数的模拟。
import '../review/protocol-frontend.test.mjs'

const root = new URL('../../frontend/src/views/components/ThirdPartyAuthorization/', import.meta.url)
const read = extension => readFileSync(new URL(`ThirdPartyAuthorization.${extension}`, root), 'utf8')

for (const succeeded of [false, true]) {
  test(`关闭协议时只在保存成功后清除本次明文：成功=${succeeded}`, async () => {
    const usePage = compileFunction(
      read('js').replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage',
      ['onMounted', 'reactive', 'ref', 'ElMessageBox', 'ElNotification', 'http'],
    )(() => {}, reactive, ref, {}, () => {}, {
      put: async () => ({ errorNo: succeeded ? 0 : 1 }),
    })
    const page = usePage()
    const item = { protocol: 'imap', enabled: true, configured: true }
    page.generatedProtocol.value = 'imap'
    page.generatedProtocolCode.value = 'temporary-test-only'
    await page.setProtocolEnabled(item, false)
    assert.equal(item.enabled, !succeeded)
    assert.equal(page.generatedProtocolCode.value, succeeded ? '' : 'temporary-test-only')
    assert.equal(page.generatedProtocol.value, succeeded ? '' : 'imap')
    assert.equal(item.configured, true)
    assert.equal(page.protocolLoading.value, false)
  })
}

test('邮件客户端文案明确区分本地服务、账号权限和可恢复的关闭状态', () => {
  const template = read('vue')
  assert.match(template, /邮件客户端协议/)
  assert.match(template, /为 IMAP、POP3、SMTP 配置专属授权码/)
  assert.match(template, /当前账号客户端访问/)
  assert.match(template, /:aria-label=/)
  assert.doesNotMatch(template, /第三方邮件|&#x[0-9a-f]+;/i)
})
