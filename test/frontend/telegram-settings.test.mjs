import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, reactive, ref } from './vue.mjs'

const root = new URL('../../frontend/src/views/components/PluginSettings/', import.meta.url)
const source = readFileSync(new URL('TelegramSettings/TelegramSettings.js', root), 'utf8')
const create = compileFunction(source.replace(/^import .*$/gm, '').replace('export function ', 'function ') + '\nreturn useTelegramSettings()',
  ['computed', 'onMounted', 'onBeforeUnmount', 'reactive', 'ref', 'ElMessage', 'ElMessageBox', 'QRCode', 'http', 'isRecentAuthenticationCancelled', 'requestWithRecentAuthentication'])
const createPage = (http, confirm = async () => {}) => create(
  computed,
  () => {},
  () => {},
  reactive,
  ref,
  { success() {} },
  { confirm },
  { toDataURL: async () => '' },
  http,
  () => false,
  request => request()
)
const userSettings = (overrides = {}) => ({
  version: 7,
  bound: true,
  displayName: 'smail_test_user',
  needsRebind: false,
  available: true,
  message: '',
  enabled: false,
  showContent: false,
  spoilerContent: false,
  sendAttachments: false,
  disableLinkPreview: true,
  ...overrides
})

test('Telegram 原生设置保留版本、限制用户字段，不依赖插件 iframe', async () => {
  const requests = []
  let conflict = false
  const page = createPage({
    get: async () => ({ errorNo: 0, data: userSettings() }),
    put: async (url, body) => {
      requests.push({ url, body })
      return conflict
        ? { errorNo: 400, errorMsg: '配置已被其他操作更新，请刷新后重试' }
        : { errorNo: 0, data: userSettings({ ...body, version: 8, synced: true, secret: '不应返回' }) }
    },
    post: async (url, body) => {
      requests.push({ url, body })
      return { errorNo: 0, data: {} }
    }
  })
  await page.load()
  assert.equal(page.ready.value, true)
  assert.equal(page.status.bound, true)
  assert.equal(page.status.displayName, 'smail_test_user')
  page.settings.sendAttachments = true
  await page.submit('save')
  assert.equal(requests[0].body.version, 7)
  assert.deepEqual(Object.keys(requests[0].body).sort(), [
    'disableLinkPreview', 'enabled', 'sendAttachments', 'showContent', 'spoilerContent', 'version'
  ])
  assert.equal(page.settings.secret, undefined)
  await page.submit('test')
  assert.equal(requests[1].url, '/api/v1/plugin/settings/telegram_push/test')
  assert.equal(requests[1].body, undefined)
  conflict = true
  await page.submit('save')
  assert.notEqual(page.errorMessage.value, '')
})

test('Telegram 首次读取失败禁止覆盖存量设置，机器人故障不影响停用', async () => {
  let fail = true
  const page = createPage({
    get: async () => {
      if (fail) throw new Error('模拟离线')
      return { errorNo: 0, data: userSettings({ version: 0, bound: false, available: false, displayName: '' }) }
    },
    put: async () => assert.fail('读取失败后不能保存')
  })
  await page.load()
  await page.submit('save')
  assert.equal(page.ready.value, false)
  fail = false
  await page.load()
  assert.equal(page.ready.value, true)
  assert.equal(page.status.bound, false)
})

test('Telegram 模板独立编译，其他插件继续保持 sandbox 隔离', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  const parent = readFileSync(new URL('PluginSettings.vue', root), 'utf8')
  assert.match(parent, /TelegramSettings\s+v-else-if="activePage\?\.value === 'telegram-user'"/)
  assert.match(parent, /sandbox="allow-scripts"/)
  assert.doesNotMatch(parent, /allow-same-origin/)
  const { descriptor, errors } = parse(readFileSync(new URL('TelegramSettings/TelegramSettings.vue', root), 'utf8'))
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'telegram-settings', inlineTemplate: true }))
  assert.deepEqual(compileStyle({ source: readFileSync(new URL('TelegramSettings/TelegramSettings.css', root), 'utf8'), id: 'telegram-settings', scoped: true }).errors, [])
  assert.doesNotMatch(source, /window\.parent|parent\.document|postMessage|localStorage/)
})

test('Telegram 通知编辑在切页和重新加载前确认，取消保留输入，保存后解除保护', async () => {
  let cancel = true
  let reads = 0
  let prompts = 0
  const page = createPage({
    get: async () => {
      reads++
      return { errorNo: 0, data: userSettings({ version: 1, showContent: false }) }
    },
    put: async (url, body) => ({ errorNo: 0, data: userSettings({ ...body, version: 2, synced: true }) })
  }, { confirm: async () => { prompts++; if (cancel) throw new Error('取消') } })
  await page.load()
  assert.equal(await page.beforeLeave(), true)
  page.settings.showContent = true
  assert.equal(await page.beforeLeave(), false)
  await page.refresh()
  assert.equal(reads, 1)
  assert.equal(page.settings.showContent, true)
  cancel = false
  await page.refresh()
  assert.equal(reads, 2)
  assert.equal(page.settings.showContent, false)
  await page.submit('save')
  assert.equal(page.hasChanges.value, false)
  const count = prompts
  assert.equal(await page.beforeLeave(), true)
  assert.equal(prompts, count)
  page.working.value = 'save'
  assert.equal(await page.beforeLeave(), false)
})