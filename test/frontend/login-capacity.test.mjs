import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileFunction } from 'node:vm'
import { reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/Login/Login.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['reactive', 'ref', 'ElMessage', 'useRouter', 'lang', 'http', 'useGlobalStatusStore', 'getSafeLoginRedirect']
)

test('登录繁忙保留输入，不修改会话，释放按钮后允许重试', async () => {
  const messages = []
  const updates = []
  let busy = true
  const page = createPage(reactive, ref, { error: message => messages.push(message) },
    () => ({ currentRoute: { value: { query: {} } }, replace: async path => updates.push(path) }), {},
    { post: async () => {
      if (busy) throw { response: { status: 503 } }
      return { errorNo: 0, data: { account: 'alice' } }
    } },
    () => ({ clearPendingLogin: () => updates.push('clear-pending'), setUser: () => updates.push('set-user') }),
    () => '/')
  Object.assign(page.form, { account: 'alice', password: 'OldPass123' })
  await page.onSubmit()
  assert.equal(page.form.password, 'OldPass123')
  assert.equal(page.loading.value, false)
  assert.deepEqual(updates, [])
  assert.match(messages[0], /繁忙.*重试/)
  busy = false
  await page.onSubmit()
  assert.deepEqual(updates, ['clear-pending', 'set-user', '/'])
})
