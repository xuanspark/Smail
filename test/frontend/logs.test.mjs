import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, ref } from './vue.mjs'
import { formatFileSize, parseRuntimeLine, summarizeRuntime, timeParts } from '../../frontend/src/views/components/LogSettings/RuntimeLog.js'

const root = new URL('../../frontend/src/views/components/LogSettings/', import.meta.url)
const read = name => readFileSync(new URL(name, root), 'utf8')
const create = compileFunction(
  read('LogSettings.js').replace(/^import .*$/gm, '').replace('export function', 'function') + '\nreturn useLogs()',
  ['computed', 'onBeforeUnmount', 'onMounted', 'ref', 'watch', 'http', 'formatFileSize', 'parseRuntimeLine', 'summarizeRuntime', 'timeParts', 'useGlobalStatusStore']
)

function fixture(get, role = 'super_admin') {
  let dispose
  const page = create(computed, callback => { dispose = callback }, () => {}, ref, () => {}, { get }, formatFileSize, parseRuntimeLine, summarizeRuntime, timeParts, () => ({ role }))
  return { page, dispose: () => dispose() }
}

test('系统收发信筛选仅超管可用，切换时不携带冲突的操作者编号', async () => {
  for (const role of ['super_admin', 'admin', 'user']) {
    const requests = []
    const { page } = fixture(async (_, { params }) => {
      requests.push(params)
      return { errorNo: 0, data: { items: [] } }
    }, role)
    page.category.value = 'mail'
    page.actorId.value = '42'
    page.mailScope.value = 'system'
    await page.search()
    assert.equal(page.canFilterSystemMail.value, role === 'super_admin')
    assert.equal(requests[0].systemOnly, role === 'super_admin' ? true : undefined)
    assert.equal(requests[0].actorId, role === 'admin' ? '42' : undefined)
    page.mailScope.value = 'all'
    await page.search()
    assert.equal(requests[1].systemOnly, undefined)
    assert.equal(requests[1].actorId, role === 'user' ? undefined : '42')
    assert.equal(page.displayActor({ actorId: '0', payload: { actorName: 'xuans' } }), '系统')
  }
  assert.match(read('LogSettings.vue'), /v-if="canFilterSystemMail"/)
})

test('普通用户不发送操作者筛选，管理员保留范围内筛选', async () => {
  for (const role of ['user', 'admin']) {
    const requests = []
    const { page } = fixture(async (_, { params }) => {
      requests.push(params)
      return { errorNo: 0, data: { items: [] } }
    }, role)
    page.actorId.value = '1'
    await page.search()
    assert.equal(page.canFilterActor.value, role === 'admin')
    assert.equal(requests[0].actorId, role === 'admin' ? '1' : undefined)
  }
})

test('日志页面使用共享 HTTP 客户端的命名导出', () => {
  const transport = readFileSync(new URL('../../frontend/src/utils/axios.js', import.meta.url), 'utf8')
  assert.match(transport, /export\s*\{\s*http\s*\}/)
  assert.match(read('LogSettings.js'), /import\s*\{\s*http\s*\}\s*from\s*['"]@\/utils\/axios['"]/)
})

test('日志分类移除配置审计，保留操作、收发信与运行日志', () => {
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }))
  assert.deepEqual(page.categories.map(item => item.value), ['operation', 'mail', 'runtime'])
  assert.equal(page.category.value, 'operation')
  assert.doesNotMatch(read('LogSettings.vue'), /category === 'config'|changes\.length/)
})

test('日志页面模板和样式能编译，详情不使用原始 HTML 渲染', () => {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  const { descriptor, errors } = parse(read('LogSettings.vue'))
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'logs', inlineTemplate: true }))
  assert.deepEqual(compileStyle({ source: read('LogSettings.css'), id: 'logs', scoped: true }).errors, [])
  assert.doesNotMatch(read('LogSettings.vue'), /v-html/)
})

test('日志查询传递时间与操作者筛选，并将业务失败显示为错误', async () => {
  const requests = []
  const { page } = fixture(async (_, options) => {
    requests.push(options.params)
    return requests.length === 1
      ? { errorNo: 0, data: { items: [{ id: 'safe' }], hasMore: false } }
      : { errorNo: 500, errorMsg: '日志暂不可用' }
  })
  page.actorId.value = ' 42 '
  page.dateRange.value = ['2026-09-01T00:00:00Z', '2026-09-08T00:00:00Z']
  await page.search()
  assert.equal(requests[0].actorId, '42')
  assert.equal(requests[0].from, '2026-09-01T00:00:00.000Z')
  assert.equal(page.items.value.length, 1)
  await page.search()
  assert.equal(page.error.value, '日志暂不可用')
  assert.equal(page.items.value.length, 0)
  assert.equal(page.displayActor({ actorId: '0' }), '系统')
})

test('跨文件运行日志短页正确返回上一页，翻页固定时间和筛选条件', async () => {
  const offsets = []
  const ranges = []
  const { page } = fixture(async (_, { params }) => {
    assert.equal(params.limit, 10)
    assert.equal(params.file, undefined)
    assert.equal(params.actorId, undefined)
    offsets.push(params.offset)
    ranges.push([params.from, params.to])
    return { errorNo: 0, data: { lines: Array(params.offset === 0 ? 8 : 10).fill('日志'), hasMore: true } }
  })
  page.category.value = 'runtime'
  page.actorId.value = '42'
  await page.search()
  page.dateRange.value = ['2026-09-01T00:00:00Z', '2026-09-02T00:00:00Z']
  await page.next()
  await page.next()
  await page.previous()
  assert.deepEqual(offsets, [0, 8, 18, 8])
  for (const range of ranges) assert.deepEqual(range, ranges[0])
  await page.search()
  assert.equal(page.offset.value, 0)
  assert.equal(offsets.at(-1), 0)
  assert.equal(ranges.at(-1)[0], '2026-09-01T00:00:00.000Z')
})

test('默认最近二十四小时和十条分页，清空时恢复默认，非法范围不请求', async () => {
  const requests = []
  const { page } = fixture(async (_, { params }) => {
    requests.push(params)
    return { errorNo: 0, data: { items: Array(10).fill({ id: 'entry' }), hasMore: true } }
  })
  await page.search()
  assert.equal(requests[0].limit, 10)
  assert.equal(new Date(requests[0].to) - new Date(requests[0].from), 24 * 60 * 60 * 1000)
  await page.next()
  assert.equal(requests[1].offset, 10)
  page.dateRange.value = null
  await page.search()
  assert.equal(requests[2].offset, 0)
  assert.equal(page.dateRange.value.length, 2)
  page.dateRange.value = ['invalid', 'invalid']
  await page.search()
  assert.equal(requests.length, 3)
  assert.equal(page.error.value, '请选择有效的起止时间')
  assert.equal(page.items.value.length, 0)
  assert.equal(page.hasMore.value, false)
  page.dateRange.value = ['2026-09-02', '2026-09-01']
  await page.search()
  assert.equal(requests.length, 3)
  await page.recent()
  assert.equal(requests.length, 4)
})

test('慢响应及卸载后的响应不得覆盖最新日志', async () => {
  let complete
  let count = 0
  const { page, dispose } = fixture(async () => {
    if (++count === 1) return new Promise(resolve => { complete = resolve })
    return { errorNo: 0, data: { items: [{ id: 'new' }] } }
  })
  const first = page.search()
  await page.search()
  complete({ errorNo: 0, data: { items: [{ id: 'old' }] } })
  await first
  assert.equal(page.items.value[0].id, 'new')
  const last = page.search()
  dispose()
  await last
  assert.equal(page.items.value[0].id, 'new')
})

test('运行日志拆分时间级别来源及请求标识，同时保留完整原文', () => {
  const raw = '[2026-09-08T14:55:23.2188949Z] [warning][2026-09-08 22:55:23][request-42][git.pcux.cn/xuans/Smail/res_init/init.go:423]开发模式：已停用恢复通道'
  const row = parseRuntimeLine(raw, 8)
  assert.equal(row.sequence, 8)
  assert.equal(row.level, 'warning')
  assert.equal(row.label, '警告')
  assert.equal(row.type, 'warning')
  assert.equal(row.requestId, 'request-42')
  assert.equal(row.sourceShort, 'res_init/init.go:423')
  assert.equal(row.message, '开发模式：已停用恢复通道')
  assert.equal(row.raw, raw)
  assert.equal(row.time, timeParts(row.createdAt).time)
})

test('无请求标识、Windows 代码路径与原文方括号均能安全展示', () => {
  const row = parseRuntimeLine('[2026-09-08T14:55:23Z] [info][2026-09-08 22:55:23][D:\\source\\app.go:39][业务]启动成功\\n下一行', 1)
  assert.equal(row.source, 'D:\\source\\app.go:39')
  assert.equal(row.requestId, '')
  assert.equal(row.message, '[业务]启动成功\\n下一行')
  assert.equal(row.level, 'info')
})

test('旧启动信息只精简摘要，不改原始记录或其他业务消息', () => {
  const header = '[2026-09-08T14:55:23Z] [info][2026-09-08 22:55:23]'
  for (const source of ['git.pcux.cn/xuans/Smail/internal/app/app.go', 'D:\\source\\internal\\app\\app.go']) {
    for (const suffix of [' ', ' \\n', '\\r\\n']) {
      const message = `***\tServer Start Success${suffix}`
      const raw = `${header}[${source}:40]${message}`
      const row = parseRuntimeLine(raw, 1)
      assert.equal(row.summary, '服务启动')
      assert.equal(row.message, message)
      assert.equal(row.raw, raw)
    }
  }
  for (const raw of [
    `${header}[other/app.go:40]***\tServer Start Success`,
    `${header}[internal/app/app.go:40]***\tServer Start Success 后续内容`,
    `${header.replace('[info]', '[warning]')}[internal/app/app.go:40]***\tServer Start Success`
  ]) {
    const row = parseRuntimeLine(raw, 1)
    assert.equal(row.summary, row.message.trim())
    assert.equal(row.raw, raw)
  }
})

test('未知格式和非法时间不丢失消息，分隔线只改摘要不改原文', () => {
  for (const raw of ['[2026-99-99T00:00:00Z] 错误', '<img src=x onerror=alert(1)>', '[业务]消息', '']) {
    const row = parseRuntimeLine(raw, 1)
    assert.equal(row.raw, raw)
    assert.equal(row.message, raw)
    assert.equal(row.level, 'unknown')
    assert.equal(row.time, '—')
  }
  const row = parseRuntimeLine('[2026-09-08T14:55:23Z] [info][2026-09-08 22:55:23][app.go:39]********', 1)
  assert.equal(row.summary, '启动分隔线')
  assert.equal(row.message, '********')
  assert.equal(parseRuntimeLine('[2026-09-08T14:55:23Z] [业务]保留内容', 2).message, '[业务]保留内容')
})

test('本页级别统计准确，压缩文件大小和空页区间明确', () => {
  const rows = ['info', 'warn', 'error', 'fatal', 'panic', 'debug'].map(level => (
    parseRuntimeLine(`[2026-09-08T14:55:23Z] [${level}][2026-09-08 22:55:23][app.go:39]内容`, 1)
  ))
  assert.deepEqual(summarizeRuntime(rows), { total: 6, info: 1, warning: 1, error: 3 })
  assert.equal(formatFileSize(2048), '2.0 KB')
  assert.equal(formatFileSize(0), '0 B')
  assert.equal(formatFileSize(undefined), '—')
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }))
  assert.equal(page.pageRange.value, '暂无记录')
  page.category.value = 'runtime'
  page.lines.value = rows.map(row => row.raw)
  page.offset.value = 8
  assert.equal(page.runtimeRows.value[0].sequence, 9)
  assert.equal(page.pageRange.value, '第 9–14 条')
  assert.deepEqual(page.runtimeSummary.value, summarizeRuntime(rows))
  assert.match(read('LogSettings.vue'), /本页日志统计/)
  assert.doesNotMatch(read('LogSettings.vue'), /lines\.join/)
})

test('运行日志明确展示结构化操作和结果，不把级别或开始事件当作成功', () => {
  const header = '[2026-09-08T14:55:23Z] [warning][2026-09-08 22:55:23][app.go:39]'
  const data = { logSchema: 1, action: '启动服务', result: 'started', object: 'HTTP' }
  const raw = `${header}开始启动 @event ${JSON.stringify(data)}`
  const row = parseRuntimeLine(raw, 1)
  assert.equal(row.action, '启动服务')
  assert.equal(row.result, 'started')
  assert.equal(row.outcome, '开始启动')
  assert.equal(row.object, 'HTTP')
  assert.equal(row.raw, raw)
  for (const message of ['旧启动成功', '正文 {"result":"success"}', '截断 @event {"logSchema":1', '内容 @event null']) {
    const old = parseRuntimeLine(header + message, 1)
    assert.equal(old.result, 'unknown')
    assert.equal(old.outcome, message)
  }
  const { page } = fixture(async () => ({ errorNo: 0, data: {} }))
  assert.equal(page.resultLabel('pending'), '待验证')
  assert.equal(page.resultLabel('accepted'), '对端接收')
  assert.equal(page.resultLabel('partial'), '部分接收')
  assert.equal(page.displayOutcome({ payload: {} }), '历史记录未提供结果说明')
  assert.equal(page.displayObject({ payload: { stage: '解析邮件' } }), '解析邮件')
  assert.match(read('LogSettings.vue'), /runtimeDetail\.action/)
  assert.match(read('LogSettings.vue'), /runtimeDetail\.result/)
})
