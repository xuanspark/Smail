import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { createRequire } from 'node:module'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const { computed, ref, watch, nextTick, effectScope } = require('vue')
const root = new URL('../../frontend/src/views/components/ConfigSettings/', import.meta.url)
const paginationSource = readFileSync(new URL('ConfigPagination/ConfigPagination.js', root), 'utf8')
const usePagination = compileFunction(
  paginationSource.replace(/^import .*$/gm, '').replace('export function', 'function') + '\nreturn usePagination',
  ['computed', 'ref', 'watch']
)(computed, ref, watch)

test('列表分页保留源数据、绝对索引及最后一页边界', () => {
  const scope = effectScope()
  scope.run(() => {
    const items = ref(Array.from({ length: 14 }, (_, index) => ({ id: index })))
    const page = usePagination(items, 6)
    assert.equal(page.pageCount.value, 3)
    page.currentPage.value = 2.8
    assert.equal(page.currentPage.value, 2)
    assert.deepEqual(page.pageItems.value.map(item => item.index), [6, 7, 8, 9, 10, 11])
    assert.equal(page.pageItems.value[0].entry, items.value[6])
    page.currentPage.value = 999
    assert.equal(page.pageItems.value.length, 2)
    items.value.splice(12)
    assert.equal(page.currentPage.value, 2)
    items.value.push({ id: 14 })
    assert.equal(page.currentPage.value, 2)
    page.currentPage.value = -1
    assert.equal(page.currentPage.value, 1)
    assert.equal(items.value.length, 13)
    items.value = []
    assert.equal(page.pageCount.value, 1)
    assert.deepEqual(page.pageItems.value, [])
  })
  scope.stop()
})

test('网络和文本列表跨页编辑、添加、删除仅更新目标条目', async () => {
  const fieldSource = readFileSync(new URL('ConfigField/ConfigField.js', root), 'utf8')
  const useField = compileFunction(
    fieldSource.replace(/^import .*$/gm, '').replace(/^export \{.*$/gm, '').replace('export function', 'function') + '\nreturn useField',
    ['computed', 'nextTick', 'fieldKey', 'usePagination']
  )(computed, nextTick, field => field.path.join('.'), usePagination)
  for (const type of ['list', 'network']) {
    const scope = effectScope()
    const value = ref(type === 'list' ? ['a.example', 'b.example', 'c.example'] : [
      { scheme: 'https', host: 'a.example', port: 443 },
      { scheme: 'https', host: 'b.example', port: 443 },
      { scheme: 'https', host: 'c.example', port: 443 }
    ])
    const props = { field: { path: ['targets'], type }, get value() { return value.value } }
    const page = scope.run(() => useField(props, (_event, _path, updated) => { value.value = updated }))
    const first = structuredClone(type === 'list' ? value.value[0] : { ...value.value[0] })
    page.currentPage.value = 2
    const index = page.pageItems.value[0].index
    if (type === 'list') page.updateEntry(index, 'changed.example')
    else page.updateTarget(index, 'host', 'changed.example')
    assert.equal(type === 'list' ? value.value[2] : value.value[2].host, 'changed.example')
    assert.deepEqual(type === 'list' ? value.value[0] : { ...value.value[0] }, first)
    await page.addEntry()
    assert.equal(page.currentPage.value, page.pageCount.value)
    assert.equal(value.value.length, 4)
    page.removeEntry(3)
    page.removeEntry(2)
    assert.equal(page.currentPage.value, 1)
    assert.equal(value.value.length, 2)
    scope.stop()
  }
})

test('基础分区保留完整表单，分页和展开菜单模板均能编译', () => {
  const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
  for (const name of ['ConfigPagination', 'ConfigForm', 'ConfigField', 'ConfigMaintenance']) {
    const text = readFileSync(new URL(`${name}/${name}.vue`, root), 'utf8')
    const { descriptor, errors } = parse(text)
    assert.deepEqual(errors, [])
    assert.doesNotThrow(() => compileScript(descriptor, { id: name, inlineTemplate: true }))
    assert.doesNotMatch(text, /&#x[0-9a-f]+;/i)
  }
  const form = readFileSync(new URL('ConfigForm/ConfigForm.vue', root), 'utf8')
  assert.match(form, /v-show="section !== 'base' \|\| activeGroup === group.id"/)
  assert.match(form, /watch\(\(\) => props.errors/)
  const css = readFileSync(new URL('ConfigForm/ConfigForm.css', root), 'utf8')
  assert.deepEqual(compileStyle({ source: css, id: 'data-v-form', scoped: true }).errors, [])
  const menu = readFileSync(new URL('../SettingsSubmenu/SettingsSubmenu.vue', root), 'utf8')
  assert.match(menu, /v-if="item.children\?\.length"\s+class="settings-tertiary-list"/)
  assert.doesNotMatch(menu, /v-if="item.children\?\.some/)
})
