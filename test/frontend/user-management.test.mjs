import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileFunction } from 'node:vm'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
const root = new URL('../../frontend/src/views/components/UserManagement/', import.meta.url)
const read = path => readFileSync(new URL(path, root), 'utf8')

function loadStatusHelpers() {
  const source = read('UserManagement.js')
    .replace(/^import .*$/gm, '')
    .replace(/export \{[\s\S]*?\}\s*$/, '')
    .replaceAll('export ', '')
  return compileFunction(`${source}\nreturn { roleMeta, recoveryCodeMeta, externalEmailMeta, accountStatusMeta }`)()
}

test('用户管理状态映射使用明确的中文文案', () => {
  const { roleMeta, recoveryCodeMeta, externalEmailMeta, accountStatusMeta } = loadStatusHelpers()
  assert.equal(roleMeta('super_admin').label, '超级管理员')
  assert.equal(roleMeta('admin').label, '管理员')
  assert.equal(roleMeta('user').label, '普通用户')
  assert.equal(roleMeta('').label, '未知权限')
  assert.equal(recoveryCodeMeta(true).label, '已开启')
  assert.equal(recoveryCodeMeta(false).label, '未开启')
  assert.equal(externalEmailMeta(true, true).label, '已绑定')
  assert.equal(externalEmailMeta(true, false).label, '未绑定')
  assert.equal(externalEmailMeta(false, true).label, '已关闭')
  assert.equal(accountStatusMeta(0).label, '正常')
  assert.equal(accountStatusMeta(1).label, '已禁用')
})

test('用户管理页面可编译并展示权限与安全状态列', () => {
  const source = read('UserManagement.vue')
  const { descriptor, errors } = parse(source)
  assert.deepEqual(errors, [])
  assert.doesNotThrow(() => compileScript(descriptor, { id: 'user-management-test', inlineTemplate: true }))
  for (const label of ['权限级别', '安全码', '外部邮箱', '账号状态']) {
    assert.match(source, new RegExp(`label="${label}"`))
  }
  assert.match(source, /scope\.row\.Role/)
  assert.match(source, /scope\.row\.RecoveryCodeConfigured/)
  assert.match(source, /scope\.row\.ExternalEmailEnabled/)
  assert.match(source, /scope\.row\.ExternalEmailConfigured/)
	assert.match(source, /管理安全码/)
	assert.match(source, /编辑外部邮箱/)
	assert.match(source, /RecoveryCodeDialog/)
	assert.match(source, /UserExternalEmailDialog/)
})

test('用户安全信息使用独立弹窗并调用固定管理接口', () => {
	for (const component of [
		'RecoveryCodeDialog/RecoveryCodeDialog.vue',
		'UserExternalEmailDialog/UserExternalEmailDialog.vue',
	]) {
		const { descriptor, errors } = parse(read(component))
		assert.deepEqual(errors, [])
		assert.doesNotThrow(() => compileScript(descriptor, { id: component, inlineTemplate: true }))
	}
	const recoverySource = read('RecoveryCodeDialog/RecoveryCodeDialog.js')
	assert.match(recoverySource, /\/api\/v1\/user\/security\/recovery-codes\/generate/)
	assert.match(recoverySource, /shown_once|codes/)
	const externalSource = read('UserExternalEmailDialog/UserExternalEmailDialog.js')
	assert.match(externalSource, /\/api\/v1\/user\/security\/external-email\/change/)
	assert.match(externalSource, /\/api\/v1\/user\/security\/external-email\/verify/)
	assert.doesNotMatch(externalSource, /\/api\/v1\/security\/external-email\/bind/)
})

test('用户管理样式可编译并保持紧凑表格布局', () => {
  const { code, errors } = compileStyle({
    source: read('UserManagement.css'),
    id: 'data-v-user-management-test',
    scoped: true
  })
  assert.deepEqual(errors, [])
  assert.match(code, /\.status-tag/)
  assert.match(code, /\.role-tag/)
  assert.match(code, /white-space:\s*nowrap/)
  assert.match(code, /@container settings-content \(max-width: 760px\)/)
})
