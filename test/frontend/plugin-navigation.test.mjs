import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const root = new URL('../../frontend/src/views/components/PluginSettings/', import.meta.url)
const source = readFileSync(new URL('PluginSettings.js', root), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'nextTick', 'onMounted', 'onBeforeUnmount', 'ref', 'http', 'useGlobalStatusStore']
)

function fixture(capabilities = ['config.read', 'plugin.read_global', 'plugin.read', 'plugin.write_own'], get = async () => ({ errorNo: 0, data: [] }), role = 'super_admin') {
  const allowed = reactive(new Set(capabilities))
  const store = reactive({ role, hasCapability: value => allowed.has(value) })
  const requests = []
  let mount, dispose
  const page = createPage(computed, async () => {}, fn => { mount = fn }, fn => { dispose = fn }, ref,
    { get: async url => { requests.push(url); return get(url) } },
    () => store)
  return { page, allowed, requests, mount, dispose, store }
}

for (const role of ['super_admin', 'admin', 'user']) {
  test(`${role} 保留本人通知入口，只有超级管理员可进入系统公众号和机器人配置`, async () => {
    const capabilities = role === 'user'
      ? ['plugin.read', 'plugin.write_own']
      : ['plugin.read_global', 'plugin.read', 'plugin.write_own']
    const { page } = fixture(capabilities, undefined, role)
    assert.deepEqual(page.sectionOptions.value.map(item => item.value), role === 'super_admin'
      ? ['spam_block', 'telegram_push', 'wechat_push']
      : role === 'admin' ? ['spam_block'] : [])
    assert.deepEqual(page.accountOptions.value.map(item => item.value), ['telegram-user', 'wechat-user'])
    await page.selectSection('telegram-user')
    assert.equal(page.activePage.value.kind, 'account')
    await page.selectSection('wechat-user')
    assert.equal(page.activePage.value.kind, 'account')
    assert.equal(page.activePage.value.value, 'wechat-user')
    await page.selectSection('wechat_push')
    assert.equal(page.selectedSection.value, role === 'super_admin' ? 'wechat_push' : 'wechat-user')
  })
}

test('超级管理员降级后隐藏系统通知配置但保留本人通知入口', async () => {
  const { page, store } = fixture()
  await page.selectSection('telegram_push')
  store.role = 'admin'
  assert.equal(page.activePage.value, undefined)
  assert.deepEqual(page.sectionOptions.value.map(item => item.value), ['spam_block'])
  await page.selectSection('telegram-user')
  assert.equal(page.activePage.value.kind, 'account')
})

test('独立插件配置包含三个运行配置页，停用插件不依赖运行列表也能配置', async () => {
  const { page, mount } = fixture()
  assert.deepEqual(page.sectionOptions.value.map(item => item.value), ['spam_block', 'telegram_push', 'wechat_push'])
  assert.equal(page.activePage.value.label, '垃圾邮件拦截')
  await mount()
  for (const key of ['telegram_push', 'wechat_push', 'spam_block']) {
    await page.selectSection(key)
    assert.equal(page.activePage.value.kind, 'config')
    assert.equal(page.activePage.value.value, key)
  }
  assert.deepEqual(page.accountOptions.value.map(item => item.value), ['telegram-user', 'wechat-user'])
})

test('普通账号只保留本人通知设置，不读取或进入全局配置和管理员插件列表', async () => {
  const { page, mount, requests, allowed } = fixture(['plugin.read', 'plugin.write_own'])
  await mount()
  assert.equal(requests.length, 0)
  assert.deepEqual(page.sectionOptions.value, [])
  assert.equal(page.activePage.value.kind, 'account')
  await page.selectSection('spam_block')
  assert.equal(page.selectedSection.value, 'telegram-user')
  allowed.delete('plugin.write_own')
  assert.equal(page.activePage.value, undefined)
})

test('只有插件读取权限的账号仍可进入插件配置，但不能读取无权限的插件列表', async () => {
  const { page, mount, requests } = fixture(['plugin.read_global'])
  await mount()
  assert.equal(requests.length, 0)
  assert.equal(page.sectionOptions.value.length, 3)
  assert.equal(page.accountOptions.value.length, 0)
})

test('插件切换等待离开确认，取消、并发点击及权限收回不会丢弃当前页', async () => {
  const { page, allowed } = fixture()
  let resolve
  let prompts = 0
  page.currentPage.value = { beforeLeave: () => { prompts++; return new Promise(done => { resolve = done }) } }
  const attempt = page.selectSection('telegram_push')
  await page.selectSection('wechat_push')
  assert.equal(prompts, 1)
  resolve(false)
  await attempt
  assert.equal(page.selectedSection.value, 'spam_block')
  const second = page.selectSection('wechat_push')
  resolve(true)
  await second
  assert.equal(page.selectedSection.value, 'wechat_push')
  page.currentPage.value = { beforeLeave: async () => { allowed.delete('plugin.read_global'); return true } }
  await page.selectSection('telegram_push')
  assert.equal(page.selectedSection.value, 'wechat_push')
  assert.equal(page.activePage.value, undefined)
})

test('未知插件只使用安全名称生成隔离页面，内置插件别名不得重复出现', async () => {
  const { page, mount, requests } = fixture(undefined, async () => ({ errorNo: 0, data: [
    'spam_block', 'telegram_push', 'wechat_push', 'SpamBlock', 'TelegramPush', 'WechatPush',
    'WeChatPushHook', 'ExtraPlugin', 'ExtraPlugin', '../bad', '<script>', null
  ] }))
  await mount()
  assert.deepEqual(requests, ['/api/v1/plugin/list'])
  assert.deepEqual(page.externalOptions.value.map(item => item.value), ['external:ExtraPlugin'])
  await page.selectSection('external:ExtraPlugin')
  assert.equal(page.activePage.value.src, '/api/v1/plugin/settings/ExtraPlugin/index.html')
  const template = readFileSync(new URL('PluginSettings.vue', root), 'utf8')
  assert.match(template, /sandbox="allow-scripts"/)
  assert.doesNotMatch(template, /allow-same-origin|v-for=".*pluginEntries/)
  assert.match(template, /:key="selectedSection"[\s\S]*:page-key="selectedSection"/)
  assert.match(template, /defineExpose\(\{ beforeLeave \}\)/)
})

test('插件列表故障不隐藏三个运行配置页，支持重试并丢弃卸载后的响应', async () => {
  let fail = true
  const { page, mount } = fixture(undefined, async () => {
    if (fail) throw new Error('读取失败')
    return { errorNo: 0, data: [] }
  })
  await mount()
  assert.equal(page.errorMessage.value, '读取失败')
  assert.equal(page.sectionOptions.value.length, 3)
  fail = false
  await page.load()
  assert.equal(page.errorMessage.value, '')
  let resolve
  const late = fixture(undefined, () => new Promise(done => { resolve = done }))
  const loading = late.mount()
  late.dispose()
  resolve({ errorNo: 0, data: ['ExtraPlugin'] })
  await loading
  assert.deepEqual(late.page.externalOptions.value, [])
})
