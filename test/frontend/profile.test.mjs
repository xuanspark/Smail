import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/components/SecuritySettings/ProfileSettings/ProfileSettings.js', import.meta.url), 'utf8')
const create = compileFunction(source.replace(/^import .*$/gm, '').replace('export function', 'function') + '\nreturn useProfile()', ['ref', 'ElMessage', 'http', 'useGlobalStatusStore', 'roleMeta'])

test('本人资料仅提交显示名称，成功更新当前账号，失败不虚报成功', async () => {
  const requests = [], messages = []
  const store = { userInfos: { name: '原名称', id: 4, role: 'user' } }
  let fail = false
  const page = create(ref, { success: text => messages.push(text), error: text => messages.push(text) }, {
    post: async (url, body) => {
      requests.push({ url, body })
      return fail ? { errorNo: 1, errorMsg: '保存失败' } : { errorNo: 0, data: { name: body.name } }
    }
  }, () => store, () => ({}))
  page.name.value = '  新名称  '
  await page.save()
  assert.deepEqual(requests, [{ url: '/api/v1/user/profile', body: { name: '新名称' } }])
  assert.equal(store.userInfos.name, '新名称')
  assert.equal(store.userInfos.role, 'user')
  fail = true
  page.name.value = '未保存名称'
  await page.save()
  assert.equal(store.userInfos.name, '新名称')
  assert.equal(messages.at(-1), '保存失败')
  assert.equal(page.saving.value, false)
})
