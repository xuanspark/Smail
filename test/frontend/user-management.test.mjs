import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileFunction } from 'node:vm'
import { roleMeta, assignableRoles, canManageUser } from '../../frontend/src/utils/userRoles.mjs'

const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
const { parse, compileScript, compileStyle } = require('@vue/compiler-sfc')
const root = new URL('../../frontend/src/views/components/UserManagement/', import.meta.url)
const read = path => readFileSync(new URL(path, root), 'utf8')

function loadStatusHelpers() {
  const source = read('UserManagement.js')
    .replace(/^import .*$/gm, '')
    .replace(/export \{[\s\S]*?\}\s*$/, '')
    .replaceAll('export ', '')
  return compileFunction(`${source}\nreturn { roleMeta, recoveryCodeMeta, externalEmailMeta, accountStatusMeta, isSameUser, canManageRecoveryCode }`, ['roleMeta'])(roleMeta)
}

function loadExternalEmailHelpers() {
  const source = read('UserExternalEmailDialog/UserExternalEmailDialog.js')
  const helper = source.match(/export function externalEmailEndpoints\(selfManaged\) \{[\s\S]*?\n\}/)?.[0]
  assert.ok(helper)
  return compileFunction(`${helper.replace('export ', '')}\nreturn { externalEmailEndpoints }`)()
}

test('用户管理状态映射使用明确的中文文案', () => {
  const { roleMeta, recoveryCodeMeta, externalEmailMeta, accountStatusMeta } = loadStatusHelpers()
  assert.equal(roleMeta('super_admin').label, '超级管理员')
  assert.equal(roleMeta('admin').label, '管理员')
  assert.equal(roleMeta('user').label, '普通用户')
  assert.equal(roleMeta('').label, '未知权限')
  assert.equal(recoveryCodeMeta(true).label, '已开启')
  assert.equal(recoveryCodeMeta(false).label, '未开启')
  assert.equal(externalEmailMeta(true).label, '已绑定')
  assert.equal(externalEmailMeta(false).label, '未绑定')
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
  assert.doesNotMatch(source, /ExternalEmailEnabled/)
  assert.match(source, /scope\.row\.ExternalEmailConfigured/)
  assert.match(source, /selectedUser\?\.ExternalEmailConfigured/)
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

test('打开安全码或外部邮箱编辑弹窗前必须确认当前登录密码', () => {
  const source = read('UserManagement.js')
  assert.match(source, /confirmRecentAuthentication/)
  assert.match(source, /confirmSecurityDialogAccess\('recovery-code'\)/)
  assert.match(source, /confirmSecurityDialogAccess\('external-email'\)/)
  assert.match(source, /if \(!await confirmSecurityDialogAccess\('recovery-code'\)\) return/)
  assert.match(source, /if \(!await confirmSecurityDialogAccess\('external-email'\)\) return/)

  const template = read('UserManagement.vue')
  assert.match(template, /securityDialogOpening === 'recovery-code'/)
  assert.match(template, /securityDialogOpening === 'external-email'/)
})

test('当前用户判断优先使用用户 ID 并保留账号兼容回退', () => {
  const { isSameUser } = loadStatusHelpers()
  assert.equal(isSameUser({ ID: 7, Account: 'same' }, { id: 7, account: 'other' }), true)
  assert.equal(isSameUser({ ID: 7, Account: 'same' }, { id: 8, account: 'same' }), false)
  assert.equal(isSameUser({ Account: 'same' }, { account: 'same' }), true)
  assert.equal(isSameUser({ Account: 'same' }, { account: 'other' }), false)
})

test('超级管理员安全码只能在部署端管理，普通用户仍使用独立弹窗', () => {
  const { canManageRecoveryCode } = loadStatusHelpers()
  assert.equal(canManageRecoveryCode({ ID: 1, Role: 'super_admin' }), false)
  assert.equal(canManageRecoveryCode({ ID: 2, Role: 'admin' }), true)
  assert.equal(canManageRecoveryCode({ ID: 3, Role: 'user' }), true)
  assert.equal(canManageRecoveryCode(null), false)

  const source = read('UserManagement.vue')
  assert.match(source, /部署端管理/)
  assert.match(source, /仅支持本机管理/)
  assert.match(source, /:disabled="!recoveryCodeWebManaged \|\| submitting"/)
  assert.match(source, /:self-managed="editingCurrentUser"/)
})

test('编辑外部邮箱按本人和代管场景使用不同的验证码接口', () => {
  const { externalEmailEndpoints } = loadExternalEmailHelpers()
  assert.deepEqual(externalEmailEndpoints(true), {
    list: '/api/v1/security/external-email/list',
    change: '/api/v1/security/external-email/change',
    verify: '/api/v1/security/external-email/change/verify',
  })
  assert.deepEqual(externalEmailEndpoints(false), {
    list: '/api/v1/user/security/external-email/list',
    change: '/api/v1/user/security/external-email/change',
    verify: '/api/v1/user/security/external-email/verify',
  })

  const dialogSource = read('UserExternalEmailDialog/UserExternalEmailDialog.vue')
  assert.match(dialogSource, /编辑我的外部邮箱/)
  assert.match(dialogSource, /props\.selfManaged/)
  assert.match(dialogSource, /append-to-body/)
  assert.match(dialogSource, /destroy-on-close/)
})

test('普通用户编辑请求不直接携带安全码或外部邮箱', () => {
  const source = read('UserManagement.js')
  const submitStart = source.indexOf('const submit = async function')
  const editRequestEnd = source.indexOf('if (!res || res.errorNo', submitStart)
  const submitSource = source.slice(submitStart, editRequestEnd)
  assert.match(submitSource, /\/api\/v1\/user\/edit/)
  assert.doesNotMatch(submitSource, /recovery_code|recoveryCode|external_email|externalEmail/)
})

test('用户级别选项遵守账户管理边界，不提供超级管理员授权入口', () => {
  assert.deepEqual(assignableRoles('super_admin').map(item => item.value), ['user', 'admin'])
  assert.deepEqual(assignableRoles('admin').map(item => item.value), ['user'])
  assert.deepEqual(assignableRoles('user'), [])
  assert.deepEqual(assignableRoles('unknown'), [])
  assert.equal(canManageUser('super_admin', 'super_admin'), false)
  assert.equal(canManageUser('super_admin', 'admin'), true)
  assert.equal(canManageUser('admin', 'admin'), false)
  assert.equal(canManageUser('admin', 'user'), true)
  assert.match(read('UserManagement.vue'), /v-model="editUserInfo\.role"/)
  assert.match(read('UserManagement.js'), /requestWithRecentAuthentication/)
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
