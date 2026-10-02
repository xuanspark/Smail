import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, reactive, ref } from './vue.mjs'

const root = new URL('../../frontend/src/views/Setup/', import.meta.url)
const source = readFileSync(new URL('Setup.js', root), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replaceAll('import.meta.env.DEV', 'false')
    .replace('export function usePage', 'function usePage') + '\nreturn usePage()',
  ['computed', 'onUnmounted', 'reactive', 'ref', 'ElMessage', 'lang', 'axios', 'Plus', 'http', 'buildSetupFinalURL', 'probeService', 'window']
)

function fixture(tlsMode, type = '2') {
  const requests = []
  const messages = []
  let scheduledTimer = null
  const window = {
    location: { href: 'https://mail.example.test/setup' },
    setTimeout: callback => {
      scheduledTimer = callback
      return 1
    },
    clearTimeout: () => {
      scheduledTimer = null
    }
  }
  const page = createPage(computed, () => {}, reactive, ref,
    { error: message => messages.push(message) }, {}, {}, {}, {
      post: async (path, payload) => {
        requests.push({ path, payload })
        return { errorNo: 0, data: { tlsMode, type, port: 8080 } }
      }
    }, () => 'https://mail.example.test/', () => true, window)
  return {
    page,
    requests,
    messages,
    window,
    runScheduledTimer: async () => {
      scheduledTimer = null
      await page.checkStatus()
    }
  }
}

test('反代初始化强制导入外部证书，即使旧配置仍是自动 DNS 类型', async () => {
  const { page, requests, messages, window, runScheduledTimer } = fixture('proxy')
  await page.getSSLConfig()
  assert.equal(page.proxyHTTPS.value, true)
  assert.equal(page.sslSettings.type, '1')
  page.sslSettings.type = '0'
  page.handleSSLTypeChange()
  assert.equal(page.sslSettings.type, '1')
  await page.handleSSLChallengeChange('dns')
  assert.equal(requests.length, 1)
  assert.equal(page.dnsChecking.value, false)
  // 绕过控件强行修改内存也不能让提交回到自动签发分支。
  page.sslSettings.type = '0'
  page.sslSettings.challenge = 'dns'
  page.sslSettings.key_path = '/external/key.pem'
  page.sslSettings.crt_path = '/external/cert.pem'
  await page.setSSLConfig()
  assert.deepEqual(requests.at(-1), {
    path: '/api/v1/setup', payload: {
      action: 'set', step: 'ssl', ssl_type: '1',
      key_path: '/external/key.pem', crt_path: '/external/cert.pem'
    }
  })
  assert.equal(requests.length, 2)
  await runScheduledTimer()
  assert.equal(window.location.href, 'https://mail.example.test/')
  assert.deepEqual(messages, [])
})

test('直接 HTTPS 初始化保留自动 HTTP 和 DNS 选项', async () => {
  for (const type of ['0', '2']) {
    const { page, messages } = fixture('direct', type)
    await page.getSSLConfig()
    assert.equal(page.proxyHTTPS.value, false)
    assert.equal(page.sslSettings.type, '0')
    assert.equal(page.sslSettings.challenge, type === '2' ? 'dns' : 'http')
    assert.deepEqual(messages, [])
  }
})

test('初始化模板能编译，实际渲染按反代策略隐藏自动选项并保留导入路径', async () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript } = require('@vue/compiler-sfc')
  const { createSSRApp, compile, h } = require('vue')
  const { renderToString } = require('vue/server-renderer')
  const { descriptor, errors } = parse(readFileSync(new URL('Setup.vue', root), 'utf8'))
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'setup-proxy', inlineTemplate: true }))
  const render = compile(descriptor.template.content)
  for (const tlsMode of ['proxy', 'direct']) {
    const { page } = fixture(tlsMode, '0')
    await page.getSSLConfig()
    page.active.value = 5
    page.lang.ssl_auto = '自动申请证书'
    page.lang.ssl_manuallyf = '导入外部证书'
    const app = createSSRApp({ setup: () => page, render })
    for (const name of new Set(descriptor.template.content.match(/\bel-[a-z-]+/g))) {
      app.component(name, {
        props: ['label', 'value'],
        setup: (props, { slots }) => () => h('div', { 'data-label': props.label, 'data-value': props.value }, slots.default?.())
      })
    }
    app.directive('loading', { getSSRProps: () => ({}) })
    const html = await renderToString(app)
    assert.match(html, /data-label="导入外部证书"/)
    if (tlsMode === 'proxy') {
      assert.doesNotMatch(html, /data-label="自动申请证书"|ssl-challenge-select-row|ssl-storage-card|ssl-port-warning/)
      assert.match(html, /外部续期后也需重新导入并应用/)
      assert.match(html, /modelvalue="\.\/data\/ssl\/private\.key"/)
      assert.match(html, /modelvalue="\.\/data\/ssl\/public\.crt"/)
    } else {
      assert.match(html, /data-label="自动申请证书"/)
      assert.match(html, /ssl-challenge-select-row|ssl-storage-card/)
    }
  }
})
