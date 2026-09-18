import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/components/SecuritySetupPrompt/SecuritySetupPrompt.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'ref', 'http', 'useGlobalStatusStore']
)

function fixture(post = async () => ({ errorNo: 0 })) {
  const store = reactive({
    isLogin: true,
    role: 'super_admin',
    userInfos: { account: 'root', showSecuritySetupPrompt: true, must_change_password: 0, webAuthnEnabled: false },
    settingsDrawerVisible: false,
    dismissRecoveryPrompt() { this.recoveryPromptDismissed = true }
  })
  return { store, page: createPage(computed, ref, { post }, () => store) }
}

test('超级管理员只提醒邮箱，强制改密时延后', () => {
  const { store, page } = fixture()
  assert.equal(page.visible.value, true)
  assert.deepEqual(Array.from(page.options.value, item => item.key), ['external-email'])
  store.userInfos.must_change_password = 1
  assert.equal(page.visible.value, false)
  store.userInfos.must_change_password = 0
  assert.equal(page.visible.value, true)
})

test('确认仅关闭本次提醒，下次登录未完成设置仍提醒', async () => {
  const requests = []
  const { store, page } = fixture(async path => { requests.push(path); return { errorNo: 0 } })
  await page.acknowledge('external-email')
  assert.equal(page.visible.value, false)
  assert.equal(store.settingsActiveTab, 'security')
  assert.equal(store.securityActiveSection, 'external-email')
  assert.equal(store.settingsDrawerVisible, true)
  assert.deepEqual(requests, ['/api/v1/security/setup/acknowledge'])
  store.userInfos = { account: 'root', showSecuritySetupPrompt: false }
  assert.equal(page.visible.value, false)
  store.settingsDrawerVisible = false
  store.userInfos = { account: 'root', showSecuritySetupPrompt: true }
  assert.equal(page.visible.value, true)
})

test('管理员和普通用户只显示尚未完成的安全码与邮箱', () => {
  for (const role of ['admin', 'user']) {
    const { store, page } = fixture()
    store.role = role
    assert.deepEqual(Array.from(page.options.value, item => item.key), ['recovery', 'external-email'])
    store.userInfos.recoveryCodesConfigured = true
    assert.deepEqual(Array.from(page.options.value, item => item.key), ['external-email'])
    store.userInfos.externalEmailConfigured = true
    store.userInfos.recoveryCodesConfigured = false
    assert.deepEqual(Array.from(page.options.value, item => item.key), ['recovery'])
    store.userInfos.recoveryCodesConfigured = true
    store.userInfos.showSecuritySetupPrompt = false
    assert.deepEqual(Array.from(page.options.value, item => item.key), [])
    assert.equal(page.visible.value, false)
  }
})

test('我知道了只关闭弹窗，去设置打开第一个未完成项', async () => {
  const { store, page } = fixture()
  store.role = 'user'
  await page.acknowledge()
  assert.equal(page.visible.value, false)
  assert.equal(store.settingsDrawerVisible, false)
  store.userInfos.showSecuritySetupPrompt = true
  await page.acknowledge(page.options.value[0].key)
  assert.equal(store.securityActiveSection, 'recovery')
  assert.equal(store.settingsDrawerVisible, true)
})

test('保存失败保留引导，可重试且不将查看状态误记为设置完成', async () => {
  const { store, page } = fixture(async () => { throw new Error('请求失败') })
  await page.acknowledge()
  assert.equal(page.visible.value, true)
  assert.equal(page.error.value, '请求失败')
  assert.equal(page.saving.value, false)
  assert.equal(store.settingsDrawerVisible, false)
  assert.equal(store.userInfos.recoveryCodesConfigured, undefined)
})

test('并发点击只提交一次，切换账号后不修改新账号状态', async () => {
  let finish
  let calls = 0
  const { store, page } = fixture(() => { calls++; return new Promise(resolve => { finish = resolve }) })
  const pending = page.acknowledge('recovery')
  await page.acknowledge('external-email')
  assert.equal(calls, 1)
  store.userInfos = { account: 'alice', showSecuritySetupPrompt: true }
  finish({ errorNo: 0 })
  await pending
  assert.equal(page.visible.value, true)
  assert.equal(store.settingsDrawerVisible, false)
})
