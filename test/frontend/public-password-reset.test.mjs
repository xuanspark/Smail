import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const script = readFileSync(new URL('../../frontend/src/views/PublicPasswordReset/PublicPasswordReset.js', import.meta.url), 'utf8')
const template = readFileSync(new URL('../../frontend/src/views/PublicPasswordReset/PublicPasswordReset.vue', import.meta.url), 'utf8')

test('匿名恢复页仅在后端明确确认链接可用时显示密码表单', () => {
  assert.match(script, /!result\.ok \|\| result\.data\?\.available !== true/)
  assert.match(template, /v-else-if="available"/)
  assert.match(template, /恢复链接无效或已失效，请重新申请。/)
})
