import { createServer } from 'node:http'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../../', import.meta.url))
const directory = path.join(root, 'test-log', `review-plugin-browser-${Date.now()}`)
await mkdir(directory, { recursive: true })
const page = await readFile(path.join(root, 'plugins/pmail_telegram_push/src/App.vue'), 'utf8')
const container = await readFile(path.join(root, 'frontend/src/views/components/PluginSettings/PluginSettings.vue'), 'utf8')
const headers = await readFile(path.join(root, 'backend/listen/http_server/https_server.go'), 'utf8')
const mounted = page.match(/onMounted\(\(\) => \{([\s\S]*?)\n\}\)/)?.[1]
const sandbox = container.match(/sandbox="([^"]+)"/)?.[1]
const policy = headers.match(/"Content-Security-Policy", "([^"]*connect-src 'none'[^"]*)"/)?.[1]
if (!mounted || !sandbox || !policy) throw new Error('未找到被审查的真实模板、安全策略或挂载函数')

// 浏览器隔离夹具：使用源码中的挂载函数、sandbox 和 CSP，不访问业务服务或数据库。
const events = []
const send = (response, type, body) => {
  response.setHeader('Content-Type', type)
  response.setHeader('Cache-Control', 'no-store')
  response.end(body)
}
const server = createServer((request, response) => {
  if (request.url === '/health') return send(response, 'application/json', JSON.stringify({ ready: true, events }))
  if (request.url === '/api/audit') {
    events.push('api-request')
    return send(response, 'application/json', '{"errorNo":0}')
  }
  if (request.url === '/frame') {
    response.setHeader('Content-Security-Policy', policy)
    return send(response, 'text/html; charset=utf-8', `<!doctype html><html><head><meta charset="utf-8"></head><body><script>
      const report = value => parent.postMessage({audit: true, ...value}, '*');
      const tourOpen = { value: false };
      const resizeIframeHeight = () => report({stage: 'resize'});
      const fetchBotInfo = () => report({stage: 'bot-info'});
      const fetchSetting = () => report({stage: 'settings'});
      addEventListener('securitypolicyviolation', event => report({stage: 'csp', directive: event.effectiveDirective}));
      try { (() => { ${mounted} })(); report({stage: 'mounted-complete'}); }
      catch (error) { report({stage: 'mounted-error', name: error.name, message: error.message}); }
      fetch('/api/audit').then(() => report({stage: 'request-ok'})).catch(error => report({stage: 'request-error', message: error.message}));
    </script></body></html>`)
  }
  send(response, 'text/html; charset=utf-8', `<!doctype html><html><head><meta charset="utf-8"><title>Smail 插件隔离审查</title></head><body>
    <h1>Smail 插件隔离审查</h1><p>不连接业务数据库，不发送第三方消息。</p>
    <pre id="evidence"></pre><script>window.auditEvents=[];addEventListener('message',event=>{if(event.data?.audit){auditEvents.push(event.data);document.querySelector('#evidence').textContent=JSON.stringify(auditEvents,null,2);}});</script>
    <iframe sandbox="${sandbox}" src="/frame" title="插件隔离夹具"></iframe></body></html>`)
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const state = { url: `http://127.0.0.1:${server.address().port}`, sandbox, policy }
await writeFile(path.join(directory, 'state.json'), JSON.stringify(state, null, 2) + '\n')
console.log(JSON.stringify({ ...state, directory: path.relative(root, directory) }))
const stop = () => { server.close(); server.closeAllConnections() }
process.once('SIGINT', stop)
process.once('SIGTERM', stop)
