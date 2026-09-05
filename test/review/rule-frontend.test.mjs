import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const Vue = require('vue')
const { parse, compileScript } = require('@vue/compiler-sfc')
const read = extension => readFileSync(new URL(`../../frontend/src/views/components/RuleSettings/RuleSettings.${extension}`, import.meta.url), 'utf8')

test('规则编辑取消后，新建按钮必须创建独立规则且不能污染原列表', async () => {
  const source = read('js').replace(/^import .*$/gm, '')
    .replace('export function usePage', 'function usePage').replace(/export \{[^}]+\}\s*$/, '')
  const original = {
    id: 7, name: '原有规则', sort: 0, action: 1, params: '',
    rules: [{ field: 'Subject', type: 'contains', rule: 'original' }]
  }
  const requests = []
  const transport = {
    post: async (url, body) => {
      if (url.endsWith('/get')) return { errorNo: 0, data: [structuredClone(original)] }
      requests.push({ url, body: JSON.parse(JSON.stringify(body)) })
      return { errorNo: 0, data: '' }
    },
    get: async () => ({ errorNo: 0, data: [] })
  }
  const lang = { cancel: '取消', submit: '提交', new_rule: '新建规则' }
  const usePage = compileFunction(source + '\nreturn usePage', [
    'reactive', 'ref', 'lang', 'http', 'ElNotification', 'Delete', 'Edit', 'InfoFilled', 'Plus'
  ])(Vue.reactive, Vue.ref, lang, transport, () => {}, {}, {}, {}, {})
  const { descriptor, errors } = parse(read('vue'))
  assert.deepEqual(errors, [])
  const compiled = compileScript(descriptor, { id: 'audit-rules', inlineTemplate: true })
  const script = compiled.content
    .replace(/import \{([^}]+)\} from ["']vue["']/g, (_, fields) => `const {${fields.replace(/\bas\b/g, ':')}} = Vue`)
    .replace(/^import .*$/gm, '').replace('export default', 'return')
  let page
  const component = compileFunction(script, ['Vue', 'usePage', 'Plus'])(
    Vue, () => { page = usePage(); return page }, {}
  )
  const node = tag => ({ tag, children: [] })
  const renderer = Vue.createRenderer({
    createElement: node, createText: text => ({ ...node('text'), text }), createComment: node,
    setText: (item, text) => { item.text = text }, setElementText: (item, text) => { item.text = text },
    parentNode: item => item.parent, nextSibling: () => null, remove() {},
    insert: (item, parent) => { item.parent = parent; parent.children.push(item) },
    patchProp: (item, key, previous, value) => { item[key] = value }
  })
  const app = renderer.createApp(component)
  app.component('ElTable', { setup: () => () => Vue.h('audit-table') })
  // 保留真实页面模板和点击处理器，只用简单容器呈现弹窗插槽。
  app.component('ElDialog', {
    setup(props, { slots }) {
      return () => Vue.h('audit-dialog', [...(slots.default?.() || []), ...(slots.footer?.() || [])])
    }
  })
  app.config.warnHandler = () => {}
  const root = node('root')
  app.mount(root)
  const flatten = item => [item, ...item.children.flatMap(flatten)]
  const textOf = item => (item.text || '') + item.children.map(textOf).join('')
  try {
    await Vue.nextTick()
    await Vue.nextTick()
    assert.equal(page.data.value.length, 1)
    page.editRule(page.data.value[0])
    page.addRuleForm.rules[0].rule = 'unsaved'
    await Vue.nextTick()
    const buttons = flatten(root).filter(item => item.tag === 'el-button')
    const cancel = buttons.find(item => textOf(item).trim() === lang.cancel)
    const create = buttons.find(item => item.class === 'add-btn')
    console.log(`rule_buttons=${JSON.stringify(buttons.map(item => ({ text: textOf(item), class: item.class, click: typeof item.onClick })))}`)
    assert.equal(typeof cancel?.onClick, 'function')
    assert.equal(typeof create?.onClick, 'function')
    cancel.onClick()
    await Vue.nextTick()
    assert.equal(page.dialogVisible.value, false)
    const cancelledListRule = page.data.value[0].rules[0].rule
    create.onClick()
    await Vue.nextTick()
    assert.equal(page.dialogVisible.value, true)
    const newRuleID = page.addRuleForm.id
    page.addRuleForm.name = '用户想新建的规则'
    buttons.find(item => textOf(item).trim() === lang.submit).onClick()
    await Vue.nextTick()
    assert.equal(requests.length, 1)
    console.log(`rule_actual=${JSON.stringify({ cancelledListRule, newRuleID, request: requests[0] })}`)
    assert.deepEqual({ cancelledListRule, newRuleID, url: requests[0].url }, {
      cancelledListRule: 'original', newRuleID: 0, url: '/api/v1/rule/add'
    }, '取消必须丢弃编辑草稿，新建必须重置旧规则 ID')
  } finally {
    app.unmount()
  }
})
