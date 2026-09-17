import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const root = fileURLToPath(new URL('../../', import.meta.url))

test('Linux 部署资源策略与只读预检反例', { skip: process.platform !== 'linux' }, () => {
  const result = spawnSync('python3', [path.join(root, 'test/deployment/resource_policy_test.py')], {
    cwd: root,
    encoding: 'utf8',
    timeout: 30000,
    env: { ...process.env, PYTHONDONTWRITEBYTECODE: '1' },
  })
  assert.ifError(result.error)
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.match(result.stderr, /Ran [1-9][0-9]* tests/)
  process.stdout.write(result.stderr)
})
