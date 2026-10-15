import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref } from './vue.mjs'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
const root = new URL('../../frontend/src/views/components/', import.meta.url)
const read = path => readFileSync(new URL(path, root), 'utf8')
const useSubmenu = compileFunction(
  read('SettingsSubmenu/SettingsSubmenu.js').replaceAll('export ', '') + '\nreturn useSubmenu'
)()

test('二级菜单只发送可见且尚未选中的选项，不提前修改父组件状态', () => {
  const props = reactive({
    modelValue: 'question',
    items: [{ value: 'question' }, { value: 'external-email' }]
  })
  const events = []
  const { select } = useSubmenu(props, (...event) => events.push(event))
  select('password')
  select('question')
  select('external-email')
  assert.deepEqual(events, [['select', 'external-email']])
  assert.equal(props.modelValue, 'question')
  props.modelValue = 'external-email'
  select('external-email')
  props.items = [{ value: 'external-email' }]
  select('question')
  assert.equal(events.length, 1)
})

test('一级菜单保留能力过滤和当前分类标题联动', () => {
  const source = read('HomeHeader/HomeHeader.js')
  const names = [
    'EpMenu', 'Lock', 'Connection', 'Setting', 'FolderOpened', 'Filter', 'User', 'Grid', 'Document', 'Checked',
    'SettingsPageStatus', 'BrandLogo'
  ]
  const createPage = compileFunction(
    source.replace(/^\uFEFF?import .*$/gm, '').replace('export function usePage', 'function usePage')
      .replace(/export \{[\s\S]*?\}\s*$/, '') + '\nreturn usePage()',
    ['computed', 'ref', 'onMounted', 'onUnmounted', 'window', 'useGlobalStatusStore', 'lang', 'defineAsyncComponent', ...names]
  )
  const capabilities = reactive(new Set(['password.change_own', 'user.read']))
  const store = reactive({
    settingsActiveTab: 'security',
    hasCapability: value => capabilities.has(value)
  })
  const page = createPage(computed, ref, () => {}, () => {}, { innerWidth: 1440 }, () => store,
    { security: '安全', plugin_settings: '插件配置' }, () => ({}), ...names.map(name => ({ name })))
  assert.deepEqual(page.settingsMenu.value.map(item => item.key), ['security', 'third-party', 'system-notification', 'group', 'rule', 'sender-filter'])
  assert.equal(page.settingsMenu.value.find(item => item.key === 'third-party').label, '邮件客户端')
  assert.equal(page.activeSettings.value.label, '安全')
  capabilities.add('config.read')
  capabilities.add('plugin.read_global')
  store.settingsActiveTab = 'config'
  assert.equal(page.activeSettings.value.label, '配置管理')
  assert.equal(page.settingsMenu.value.length, 8)
  assert.equal(page.settingsMenu.value.find(item => item.key === 'plugins').label, '插件配置')
  assert.ok(page.settingsMenu.value.every(item => item.icon && item.hint))
  capabilities.delete('config.read')
  capabilities.delete('plugin.read_global')
  assert.equal(page.activeSettings.value, undefined)
  assert.equal(page.settingsMenu.value.some(item => item.key === 'plugins'), false)
  capabilities.add('plugin.read')
  assert.equal(page.settingsMenu.value.some(item => item.key === 'plugins'), true)
  capabilities.add('audit.read')
  for (const role of ['user', 'admin']) {
    store.role = role
    assert.equal(page.settingsMenu.value.some(item => item.key === 'logs'), true)
  }
  store.role = 'super_admin'
  assert.equal(page.settingsMenu.value.at(-1).key, 'logs')
})

test('三级菜单只发送当前分组的可见子项，选中状态仍由父页面确认', () => {
  const props = reactive({ modelValue: 'spam_block', items: [{ value: 'plugins', children: [{ value: 'spam_block' }, { value: 'telegram_push' }] }] })
  const events = []
  const { select } = useSubmenu(props, (...event) => events.push(event))
  select('unknown')
  select('spam_block')
  select('telegram_push')
  assert.deepEqual(events, [['select', 'telegram_push']])
  assert.equal(props.modelValue, 'spam_block')
  props.items = []
  select('telegram_push')
  assert.equal(events.length, 1)
  assert.match(read('SettingsSubmenu/SettingsSubmenu.vue'), /settings-tertiary-list/)
  assert.match(read('SettingsSubmenu/SettingsSubmenu.css'), /\.settings-tertiary-item:focus-visible/)
})

test('菜单模板可编译并保留按钮、当前项语义及原有选区处理器', () => {
  for (const name of ['HomeHeader', 'SettingsSubmenu', 'SecuritySettings', 'ConfigSettings', 'PluginSettings']) {
    const source = read(`${name}/${name}.vue`)
    const { descriptor, errors } = parse(source)
    assert.deepEqual(errors, [])
    assert.doesNotThrow(() => compileScript(descriptor, { id: 'settings-test', inlineTemplate: true }))
  }
  const submenu = read('SettingsSubmenu/SettingsSubmenu.vue')
  assert.match(submenu, /<button[\s\S]*type="button"/)
  assert.match(submenu, /:aria-current="modelValue === item.value \? 'page' : undefined"/)
  for (const name of ['SecuritySettings', 'ConfigSettings', 'PluginSettings']) {
    assert.match(read(`${name}/${name}.vue`), /<SettingsSubmenu[\s\S]*@select="selectSection"/)
  }
  assert.match(read('HomeHeader/HomeHeader.vue'), /globalStatus.settingsDrawerVisible && globalStatus.settingsActiveTab === item.key/)
})

test('一级标签样式不会穿透到插件内部，抽屉样式可命中传送后的节点', () => {
  const { code, errors } = compileStyle({
    source: read('HomeHeader/HomeHeader.css'), id: 'data-v-settings-test', scoped: true
  })
  assert.deepEqual(errors, [])
  assert.match(code, /\.settings-tabs\[data-v-settings-test\]\s*>\s*\.el-tabs__header/)
  assert.doesNotMatch(code, /\.settings-tabs\[data-v-settings-test\]\s+\.el-tabs__header/)
  assert.match(code, /\.settings-drawer > \.el-drawer__body\s*\{/)
  assert.match(code, /container: settings-content \/ inline-size/)
})

test('两级菜单分别提供选中、非选中悬停和键盘焦点样式', () => {
  for (const path of [
    'HomeHeader/HomeHeader.css',
    'SettingsSubmenu/SettingsSubmenu.css'
  ]) {
    const source = read(path)
    assert.match(source, /:hover:not\(/)
    assert.match(source, /:focus-visible/)
    assert.match(source, /(?:\.is-active|\.active)/)
  }
  for (const name of ['SecuritySettings', 'ConfigSettings', 'SettingsSubmenu']) {
    assert.match(read(`${name}/${name}.css`), /@container settings-content \(max-width: 560px\)/)
  }
})

test('配置管理只在真实移动端将内部导航移到内容顶部', () => {
  const header = read('HomeHeader/HomeHeader.vue')
  const config = read('ConfigSettings/ConfigSettings.css')
  const containerStart = config.indexOf('@container settings-content (max-width: 560px)')
  const desktopStart = config.indexOf('@media (min-width: 769px)', containerStart)
  const containerRule = config.slice(containerStart, desktopStart)
  assert.match(header, /:size="isMobile \? '100%' : '50%'"/)
  assert.match(read('HomeHeader/HomeHeader.css'), /\.settings-drawer\.el-drawer[\s\S]*right: 0[\s\S]*inset-block: 0[\s\S]*border-radius: 0;/)
  assert.match(read('HomeHeader/HomeHeader.css'), /\.settings-category[\s\S]*flex-wrap: nowrap;/)
  assert.match(read('HomeHeader/HomeHeader.css'), /\.settings-category-copy[\s\S]*flex: 1 1 auto;[\s\S]*overflow: hidden;/)
  assert.match(read('HomeHeader/HomeHeader.css'), /@media \(max-width: 768px\)[\s\S]*\.settings-drawer\.el-drawer[\s\S]*right: 0;/)
  assert.match(config, /grid-template-areas: "main submenu"/)
  assert.match(config, /@media \(min-width: 769px\)[\s\S]*grid-template-columns: minmax\(0, 1fr\)/)
  assert.match(config, /@media \(max-width: 768px\)[\s\S]*"submenu"[\s\S]*"main"/)
  assert.ok(containerStart >= 0 && desktopStart > containerStart)
  assert.doesNotMatch(containerRule, /config-workspace|config-navigation/)
})
