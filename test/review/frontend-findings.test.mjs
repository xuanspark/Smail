import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { readFormConfig, updateFormConfig, validateFormConfig } from '../../frontend/src/views/components/ConfigSettings/ConfigForm/ConfigForm.js'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const Vue = require('vue')
const { parse, compileScript } = require('@vue/compiler-sfc')
const read = name => readFileSync(new URL(`../../frontend/${name}`, import.meta.url), 'utf8')

test('邮件列表挂载后，表格选中项必须用于全选和批量操作', async () => {
  const source = read('src/views/List/List.js').replace(/^import .*$/gm, '')
    .replace('export function usePage', 'function usePage').replace(/export \{[^}]+\}\s*$/, '')
  const icons = ['CircleCheck', 'Switch', 'Delete', 'View', 'Folder', 'EpArrowDownBold', 'EditPen', 'Warning']
  const messages = []
  const requests = []
  const rows = [{ id: 1 }, { id: 2 }]
  const transport = {
    post: async url => {
      requests.push(url)
      return { errorNo: 0, data: { list: rows, total_page: 1 } }
    },
    get: async () => ({ errorNo: 0, data: [] })
  }
  const usePage = compileFunction(source + '\nreturn usePage', [
    'Vue', 'ref', 'watch', 'useRouter', 'useGroupStore', 'lang', 'http', 'ElMessage', 'ElMessageBox', ...icons
  ])(
    Vue, Vue.ref, Vue.watch, () => ({ push() {} }), () => Vue.reactive({ tag: '', name: '收件箱' }), {}, transport,
    { warning: value => messages.push(value), success() {}, error() {} }, { confirm: async () => true },
    ...icons.map(() => ({}))
  )
  let page
  let table
  let cleared = 0
  let toggled = 0
  const { descriptor, errors } = parse(read('src/views/List/List.vue'))
  assert.deepEqual(errors, [])
  const compiled = compileScript(descriptor, { id: 'audit-list', inlineTemplate: true })
  const script = compiled.content
    .replace(/import \{([^}]+)\} from ["']vue["']/g, (_, fields) => `const {${fields.replace(/\bas\b/g, ':')}} = Vue`)
    .replace(/^import .*$/gm, '').replace('export default', 'return')
  const component = compileFunction(script, ['Vue', 'usePage', ...icons])(
    Vue, () => { page = usePage(); return page }, ...icons.map(() => ({}))
  )
  const tableComponent = {
    props: ['data'],
    emits: ['selection-change'],
    setup(props, { expose, emit }) {
      let selection = []
      table = {
        getSelectionRows: () => selection,
        clearSelection: () => { cleared++; selection = []; emit('selection-change', selection) },
        toggleRowSelection: (row, selected) => {
          toggled++
          selection = selection.filter(item => item.id !== row.id)
          if (selected) selection.push(row)
          emit('selection-change', selection)
        },
        selectRow: row => { selection = [row]; emit('selection-change', selection) }
      }
      expose(table)
      return () => Vue.h('audit-table')
    }
  }
  const node = type => ({ type, children: [] })
  const renderer = Vue.createRenderer({
    createElement: node, createText: text => ({ ...node('text'), text }), createComment: node,
    setText: (item, text) => { item.text = text }, setElementText: (item, text) => { item.text = text },
    parentNode: item => item.parent, nextSibling: () => null, remove() {},
    insert: (item, parent) => { item.parent = parent; parent.children.push(item) },
    patchProp: (item, key, previous, value) => { item[key] = value }
  })
  const app = renderer.createApp(component)
  app.component('ElTable', tableComponent)
  app.config.warnHandler = () => {}
  app.mount(node('root'))
  try {
    await Vue.nextTick()
    assert.ok(table)
    table.selectRow(rows[0])
    assert.equal(page.selectedRows.value.length, 1)
    requests.length = 0
    assert.ok(page.taskTableDataRef.value, '真实模板必须连接组合函数返回的表格引用')
    page.selectAll()
    assert.equal(page.selectedRows.value.length, 2)
    page.invertSelection()
    assert.equal(page.selectedRows.value.length, 0)
    assert.equal(cleared, 1)
    assert.equal(toggled, 4)
    page.selectAll()
    page.markRead()
    page.move(0, '归档')
    page.del()
    await Promise.resolve()
    await Promise.resolve()
    const actual = { connected: !!page.taskTableDataRef.value, cleared, toggled, requests: [...requests], warnings: [...messages] }
    console.log(`list_actual=${JSON.stringify(actual)}`)
    assert.equal(cleared, 2)
    assert.equal(toggled, 6)
    for (const action of ['read', 'move', 'del']) assert.ok(requests.includes(`/api/v1/email/${action}`))
    assert.equal(actual.connected, true, '真实模板必须连接组合函数返回的表格引用')
    assert.equal(actual.cleared, 2)
    assert.equal(actual.requests.filter(url => !url.endsWith('/list')).length, 3)
  } finally {
    app.unmount()
  }
})

// 后端约束由 TestAuditPendingConfigBlocksFurtherUpdates 使用真实 manager 单独验证。
for (const action of ['edit', 'refresh']) {
  test(`保存候选配置后 ${action} 不应让页面失去继续保存或应用的入口`, async () => {
    const source = read('src/views/components/ConfigSettings/ConfigSettings.js')
      .replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage')
    const initial = {
      listener: { port: 25, starttlsPort: 587, tlsPort: 465 },
      relay: {
        relayHost: '',
        relayPort: 0,
        security: '',
        from: '',
        fromDomain: '',
        verificationEnabled: true,
        loginPolicy: 'new_device'
      },
      imap: { tlsPort: 993 },
      pop3: { port: 110, tlsPort: 995 },
      policy: { maxMessageBytes: 10 << 20, spamFilterLevel: 2, externalRecoveryEnabled: true }
    }
    let pending = null
    let applied = false
    const errors = []
    const transport = {
      get: async url => ({ errorNo: 0, data: url.endsWith('/pending') ? pending : url.endsWith('/status') ? { activeGeneration: 1, setupState: 'ready' } : initial }),
      put: async (url, body) => {
        if (pending) return { errorNo: 409, errorMsg: '配置代次冲突' }
        pending = { generation: 2, candidateId: 'audit-candidate', sameSession: true, section: 'mail', payload: body.payload }
        return { errorNo: 0, data: pending }
      },
      post: async (url, body) => {
        assert.ok(url.endsWith('/apply'))
        assert.equal(body.candidateId, pending.candidateId)
        assert.equal(body.generation, pending.generation)
        applied = true
        pending = null
        return { errorNo: 0, data: { generation: 2 } }
      }
    }
    const page = compileFunction(source + '\nreturn usePage("mail")', [
      'computed', 'onMounted', 'onBeforeUnmount', 'reactive', 'ref', 'ElMessage', 'ElMessageBox',
      'http', 'useGlobalStatusStore', 'readFormConfig', 'updateFormConfig', 'validateFormConfig'
    ])(
      Vue.computed, () => {}, () => {}, Vue.reactive, Vue.ref,
      { success() {}, warning() {}, error: message => errors.push(message) }, { confirm: async () => true },
      transport, () => ({ hasCapability: () => true }), readFormConfig, updateFormConfig, validateFormConfig
    )
    await page.loadAll()
    page.updateField(['policy', 'maxMessageBytes'], 8 << 20)
    await page.saveConfig()
    assert.equal(page.savedGeneration.value, 2)
    assert.deepEqual(errors, [])
    if (action === 'edit') page.updateField(['policy', 'maxMessageBytes'], 16 << 20)
    else await page.refreshPage()
    await page.continuePending()
    console.log(`action=${action} server_pending=${pending} saved_generation=${page.savedGeneration.value} apply_requested=${applied} errors=${JSON.stringify(errors)}`)
    assert.equal(applied, true, '页面必须保留恢复或取消候选配置的可用路径，而不是保存冲突且应用不发请求')
  })
}

// 使用真实响应拦截器和页面函数；仅隔离网络、消息提示以及下载所需的浏览器对象。
test('外部邮箱验证码输入错误后必须能够再次输入，而不是重新绑定同一地址', async () => {
  const source = read('src/views/components/SecuritySettings/SecuritySettings.js')
    .replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage')
  let bound = false
  let prompts = 0
  let verifies = 0
  const notifications = []
  const page = compileFunction(source + '\nreturn usePage()', [
    'computed', 'onMounted', 'reactive', 'ref', 'watch', 'ElMessageBox', 'ElNotification',
    'lang', 'http', 'useGlobalStatusStore', 'webAuthnSupported'
  ])(
    Vue.computed, () => {}, Vue.reactive, Vue.ref, () => {},
    { prompt: async () => { prompts++; return { value: prompts === 1 ? 'incorrect' : 'correct' } } },
    notification => notifications.push(notification.message), {},
    { get: async () => ({ errorNo: 0, data: [] }), post: async url => {
      if (url.endsWith('/bind')) {
        if (bound) return { errorNo: 400, errorMsg: '外部邮箱绑定失败' }
        bound = true
        return { errorNo: 0, data: { id: 1, challenge_id: 'audit-binding', email: 'b***@example.com' } }
      }
      assert.ok(url.endsWith('/verify'))
      verifies++
      return verifies === 1 ? { errorNo: 400, errorMsg: '验证失败' } : { errorNo: 0 }
    } },
    () => ({ role: 'user', userInfos: {}, securityActiveSection: 'external-email' }), () => false
  )
  page.bindForm.email = 'binding@example.com'
  await page.bindExternalEmail()
  assert.equal(prompts, 1)
  assert.equal(verifies, 1)
  await page.bindExternalEmail()
  console.log(`binding_pending=${bound} prompt_count=${prompts} verify_count=${verifies} notifications=${JSON.stringify(notifications)}`)
  assert.equal(prompts, 2, '验证码首次输入错误后应允许重试验证现有绑定')
})

// 使用真实响应拦截器和页面函数；仅隔离网络、消息提示以及下载所需的浏览器对象。
test('审计导出必须保存服务端返回的 Blob 内容', async () => {
  let interceptor
  const transport = { interceptors: { response: { use: success => { interceptor = success } } } }
  const axiosSource = read('src/utils/axios.js')
    .replace(/^import .*$/gm, '')
    .replace('import.meta.env.VITE_APP_URL', 'undefined')
    .replace('export {http};', '')
  compileFunction(axiosSource, ['axios', 'lang', 'router', 'createLoginRedirectHandler'])(
    { create: () => transport }, { lang: 'zhCn' }, {}, () => () => {}
  )
  assert.equal(typeof interceptor, 'function')
  const source = read('src/views/components/ConfigSettings/ConfigSettings.js')
    .replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage')
  const results = []
  for (const format of ['csv', 'json']) {
    const payload = format === 'csv' ? 'id,operation\n1,update\n' : '[{"id":1,"operation":"update"}]'
    let downloaded
    let clicked = false
    const errors = []
    const page = compileFunction(source + '\nreturn usePage("audit")', [
      'computed', 'onMounted', 'onBeforeUnmount', 'reactive', 'ref', 'ElMessage', 'ElMessageBox',
      'http', 'useGlobalStatusStore', 'readFormConfig', 'updateFormConfig', 'validateFormConfig', 'URL', 'document'
    ])(
      Vue.computed, () => {}, () => {}, Vue.reactive, Vue.ref,
      { success() {}, error: message => errors.push(message) }, {},
      { post: async (url, body, config) => {
        assert.equal(url, '/api/v1/audits/config')
        assert.equal(config.responseType, 'blob')
        assert.equal(body.format, format)
        return interceptor({ data: new Blob([payload]), config: { url } })
      } },
      () => ({ hasCapability: () => true }), () => ({}), () => '', () => ({}),
      { createObjectURL: blob => { downloaded = blob; return 'blob:audit' }, revokeObjectURL() {} },
      { createElement: () => ({ click: () => { clicked = true } }) }
    )
    await page.exportAudit(format)
    assert.deepEqual(errors, [])
    assert.equal(clicked, true)
    assert.ok(downloaded instanceof Blob)
    const actual = await downloaded.text()
    console.log(`format=${format} downloaded=${JSON.stringify(actual)}`)
    results.push({ format, actual, payload })
  }
  for (const { actual, payload } of results) assert.equal(actual, payload, '下载文件必须保留真实审计记录，而不是 undefined')
})

for (const format of ['csv', 'json']) {
  test(`审计 ${format} 导出的错误 Blob 不应下载，失败后允许重试`, async () => {
    const source = read('src/views/components/ConfigSettings/ConfigSettings.js')
      .replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage')
    const errors = []
    const downloads = []
    let clicked = 0
    let requests = 0
    const payload = format === 'csv' ? 'id,operation\n1,update\n' : '[{"id":1}]'
    const page = compileFunction(source + '\nreturn usePage("audit")', [
      'computed', 'onMounted', 'onBeforeUnmount', 'reactive', 'ref', 'ElMessage', 'ElMessageBox',
      'http', 'useGlobalStatusStore', 'readFormConfig', 'updateFormConfig', 'validateFormConfig', 'URL', 'document'
    ])(
      Vue.computed, () => {}, () => {}, Vue.reactive, Vue.ref,
      { success() {}, error: message => errors.push(message) }, {},
      { post: async () => ++requests === 1
        ? new Blob([JSON.stringify({ errorNo: 403, errorMsg: '无权导出审计记录' })], { type: 'application/json' })
        : new Blob([payload], { type: format === 'csv' ? 'text/csv' : 'application/json' }) },
      () => ({ hasCapability: () => true }), () => ({}), () => '', () => ({}),
      { createObjectURL: blob => { downloads.push(blob); return 'blob:audit' }, revokeObjectURL() {} },
      { createElement: () => ({ click: () => { clicked++ } }) }
    )
    await page.exportAudit(format)
    assert.deepEqual(errors, ['无权导出审计记录'])
    assert.equal(downloads.length, 0)
    assert.equal(clicked, 0)
    assert.equal(page.working.value, '')
    await page.exportAudit(format)
    assert.equal(requests, 2)
    assert.equal(downloads.length, 1)
    assert.equal(clicked, 1)
    assert.equal(await downloads[0].text(), payload)
    assert.equal(page.working.value, '')
  })
}

// 挂载真实 SFC 的编译结果和真实组合函数，使用 Vue 自定义渲染器代替浏览器 DOM。
test('写信页挂载后，附件按钮必须连接到文件输入框', async () => {
  const source = read('src/views/Editer/Editer.js').replace(/^import .*$/gm, '')
    .replace('export function usePage', 'function usePage').replace(/export \{[^}]+\}\s*$/, '')
  const names = ['onBeforeUnmount', 'reactive', 'ref', 'shallowRef', 'ElMessage', 'Close', 'Paperclip',
    'Position', 'ArrowDown', 'Document', 'lang', 'Editor', 'Toolbar', 'i18nChangeLanguage', 'useRouter',
    'http', 'useGroupStore', 'useGlobalStatusStore', 'MouseEvent']
  const values = [Vue.onBeforeUnmount, Vue.reactive, Vue.ref, Vue.shallowRef, {}, {}, {}, {}, {}, {},
    { lang: 'zhCn' }, {}, {}, () => {}, () => ({}), {}, () => ({}),
    () => ({ userInfos: { account: 'audit', name: 'audit', domains: ['example.test'], is_admin: false } }),
    class { constructor(type) { this.type = type } }]
  const usePage = compileFunction(source + '\nreturn usePage', names)(...values)
  let page
  const { descriptor, errors } = parse(read('src/views/Editer/Editer.vue'))
  assert.deepEqual(errors, [])
  const compiled = compileScript(descriptor, { id: 'audit-editor', inlineTemplate: true })
  const script = compiled.content
    .replace(/import \{([^}]+)\} from ["']vue["']/g, (_, fields) => `const {${fields.replace(/\bas\b/g, ':')}} = Vue`)
    .replace(/^import .*$/gm, '').replace('export default', 'return')
  const component = compileFunction(script, ['Vue', 'usePage', 'Paperclip', 'Position', 'ArrowDown', 'Toolbar', 'Editor', 'Document', 'Close'])(
    Vue, () => { page = usePage(); return page }, {}, {}, {}, {}, {}, {}, {}
  )
  const node = type => ({ type, style: {}, children: [], dispatchEvent(event) { this.lastEvent = event.type; return true } })
  const renderer = Vue.createRenderer({
    createElement: node, createText: text => ({ ...node('text'), text }), createComment: node,
    setText: (item, text) => { item.text = text }, setElementText: (item, text) => { item.text = text },
    parentNode: item => item.parent, nextSibling: () => null, remove() {},
    insert: (item, parent) => { item.parent = parent; parent.children.push(item) },
    patchProp: (item, key, previous, value) => { item[key] = value }
  })
  const app = renderer.createApp(component)
  app.config.warnHandler = () => {}
  app.mount(node('root'))
  try {
    await Vue.nextTick()
    console.log(`template_file_ref_connected=${!!page.fileRef.value}`)
    assert.doesNotThrow(() => page.upload(), '挂载后点击添加附件不应抛出异常')
    assert.equal(page.fileRef.value?.lastEvent, 'click')
  } finally {
    app.unmount()
  }
})
