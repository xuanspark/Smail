import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'
import { computed, ref } from './vue.mjs'

const root = new URL('../../frontend/src/views/components/LogSettings/', import.meta.url)
const read = name => readFileSync(new URL(name, root), 'utf8')
const create = compileFunction(
  read('LogSettings.js').replace(/^import .*$/gm, '').replace('export function', 'function') + '\nreturn useLogs()',
  ['computed', 'onBeforeUnmount', 'onMounted', 'ref', 'watch', 'http']
)

function fixture(get) {
  let dispose
  const page = create(computed, callback => { dispose = callback }, () => {}, ref, () => {}, { get })
  return { page, dispose: () => dispose() }
}

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

test('压缩日志短页正确返回上一页，文件过期后重置偏移', async () => {
  let filename = 'runtime-2026-08-15.log.gz'
  const offsets = []
  const { page } = fixture(async (_, { params }) => {
    if (!params.file) return { errorNo: 0, data: { files: [{ name: filename, compressed: true }] } }
    offsets.push(params.offset)
    return { errorNo: 0, data: { lines: Array(params.offset === 0 ? 8 : 12).fill('日志'), hasMore: true } }
  })
  page.category.value = 'runtime'
  await page.search()
  await page.next()
  await page.next()
  await page.previous()
  assert.deepEqual(offsets, [0, 8, 20, 8])
  filename = 'runtime-2026-08-16.log.gz'
  await page.next()
  assert.equal(page.offset.value, 0)
  assert.equal(offsets.at(-1), 0)
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
