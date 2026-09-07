import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/components/ConfigSettings/ConfigNavigation.js', import.meta.url), 'utf8')
const components = ['BaseConfig', 'EmailConfig', 'SslConfig', 'PluginsConfig', 'ConfigMaintenance']
const createNavigation = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function useNavigation', 'function useNavigation') + '\nreturn useNavigation()',
  ['computed', 'nextTick', 'ref', 'useGlobalStatusStore', ...components]
)

function fixture(capabilities = ['config.read']) {
  const allowed = reactive(new Set(capabilities))
  const page = createNavigation(computed, async () => {}, ref,
    () => ({ hasCapability: key => allowed.has(key) }), ...components.map(name => ({ name })))
  return { page, allowed }
}

test('配置菜单包含三个独立插件三级页面，无权限的维护入口不可打开', async () => {
  const { page, allowed } = fixture()
  assert.equal(new Set(page.sectionOptions.value.map(item => item.component)).size, 4)
  assert.deepEqual(page.sectionOptions.value.map(item => item.value), ['base', 'mail', 'ssl', 'plugins'])
  const children = page.sectionOptions.value.find(item => item.value === 'plugins').children
  assert.deepEqual(children.map(item => item.value), ['spam_block', 'telegram_push', 'wechat_push'])
  await page.selectSection('plugins')
  assert.equal(page.selectedSection.value, 'spam_block')
  assert.equal(page.activePage.value.label, '垃圾邮件拦截')
  await page.selectSection('telegram_push')
  assert.equal(page.activePage.value.label, 'Telegram 推送')
  await page.selectSection('plugins')
  assert.equal(page.selectedSection.value, 'telegram_push')
  await page.selectSection('base')
  assert.equal(page.maintenanceOptions.value.length, 0)
  await page.selectSection('audit')
  await page.selectSection('invalid')
  assert.equal(page.selectedSection.value, 'base')
  allowed.add('audit.read')
  assert.deepEqual(page.maintenanceOptions.value.map(item => item.value), ['audit'])
  await page.selectSection('audit')
  assert.equal(page.activePage.value.component.name, 'ConfigMaintenance')
  allowed.delete('config.read')
  assert.equal(page.activePage.value, undefined)
})

test('插件三级菜单切换保留离开确认及权限检查', async () => {
  const { page, allowed } = fixture()
  await page.selectSection('wechat_push')
  page.currentPage.value = { beforeLeave: async () => false }
  await page.selectSection('spam_block')
  assert.equal(page.selectedSection.value, 'wechat_push')
  page.currentPage.value = { beforeLeave: async () => { allowed.delete('config.read'); return true } }
  await page.selectSection('telegram_push')
  assert.equal(page.selectedSection.value, 'wechat_push')
  assert.equal(page.activePage.value, undefined)
})

test('切页等待离开确认，取消和快速重复点击不会更改页面', async () => {
  const { page } = fixture()
  let resolve
  let prompts = 0
  page.currentPage.value = { beforeLeave: () => { prompts++; return new Promise(done => { resolve = done }) } }
  const first = page.selectSection('mail')
  await page.selectSection('ssl')
  assert.equal(prompts, 1)
  assert.equal(page.selectedSection.value, 'base')
  resolve(false)
  await first
  assert.equal(page.selectedSection.value, 'base')
  const second = page.selectSection('mail')
  resolve(true)
  await second
  assert.equal(page.selectedSection.value, 'mail')
  assert.equal(page.activePage.value.component.name, 'EmailConfig')
})

test('退出确认期间收回权限，不得进入原先可见的维护页', async () => {
  const { page, allowed } = fixture(['config.read', 'config.rollback'])
  page.currentPage.value = { beforeLeave: async () => { allowed.delete('config.rollback'); return true } }
  await page.selectSection('backups')
  assert.equal(page.selectedSection.value, 'base')
})
