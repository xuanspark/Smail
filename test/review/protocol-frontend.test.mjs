import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const Vue = require('vue')
const { parse, compileScript } = require('@vue/compiler-sfc')
const read = extension => readFileSync(new URL(`../../frontend/src/views/components/ThirdPartyAuthorization/ThirdPartyAuthorization.${extension}`, import.meta.url), 'utf8')

for (const succeeded of [false, true]) {
  test(`关闭协议后界面状态必须与服务端一致：请求成功=${succeeded}`, async () => {
    const source = read('js').replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage')
    let serverEnabled = true
    const requests = []
    const notices = []
    const transport = {
      get: async () => ({ errorNo: 0, data: [{ protocol: 'imap', enabled: serverEnabled, configured: true }] }),
      put: async (url, data) => {
        requests.push({ url, ...data })
        if (!succeeded) throw new Error('隔离模拟网络失败')
        serverEnabled = data.enabled
        return { errorNo: 0 }
      }
    }
    const usePage = compileFunction(source + '\nreturn usePage', ['onMounted', 'ref', 'ElMessageBox', 'ElNotification', 'http'])(
      Vue.onMounted, Vue.ref, {}, value => notices.push(value), transport
    )
    const { descriptor, errors } = parse(read('vue'))
    assert.deepEqual(errors, [])
    const compiled = compileScript(descriptor, { id: 'audit-protocol', inlineTemplate: true })
    const script = compiled.content
      .replace(/import \{([^}]+)\} from ["']vue["']/g, (_, fields) => `const {${fields.replace(/\bas\b/g, ':')}} = Vue`)
      .replace(/^import .*$/gm, '').replace('export default', 'return')
    let page
    let toggle
    const component = compileFunction(script, ['Vue', 'usePage'])(Vue, () => { page = usePage(); return page })
    const node = tag => ({ tag, children: [] })
    const renderer = Vue.createRenderer({
      createElement: node, createText: text => ({ ...node('text'), text }), createComment: node,
      setText: (item, text) => { item.text = text }, setElementText: (item, text) => { item.text = text },
      parentNode: item => item.parent, nextSibling: () => null, remove() {},
      insert: (item, parent) => { item.parent = parent; parent.children.push(item) },
      patchProp: (item, key, previous, value) => { item[key] = value }
    })
    const app = renderer.createApp(component)
    app.component('ElSwitch', {
      props: ['modelValue'], emits: ['update:modelValue', 'change'],
      setup(props, { emit }) {
        // 与本机 Element Plus 源码一致：先更新 v-model，再触发 change。
        toggle = value => { emit('update:modelValue', value); emit('change', value) }
        return () => Vue.h('audit-switch', { enabled: props.modelValue })
      }
    })
    app.config.warnHandler = () => {}
    app.mount(node('root'))
    try {
      await Vue.nextTick()
      await Vue.nextTick()
      assert.equal(page.protocolCredentials.value[0].enabled, true)
      assert.equal(typeof toggle, 'function')
      toggle(false)
      await Vue.nextTick()
      await Vue.nextTick()
      assert.equal(requests.length, 1)
      assert.equal(notices[0].type, succeeded ? 'success' : 'error')
      const uiEnabled = page.protocolCredentials.value[0].enabled
      console.log(JSON.stringify({ succeeded, serverEnabled, uiEnabled, request: requests[0] }))
      assert.equal(uiEnabled, serverEnabled, '失败时应恢复仍然开启的服务端状态，不能误显示已关闭')
    } finally {
      app.unmount()
    }
  })
}
