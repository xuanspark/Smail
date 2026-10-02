import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, symlink, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { assertDeploymentLayout, readDevelopmentRuntime, resolveRuntimeFile, runtimeRoot, sourceRoot } from '../../deploy/runtime-config.mjs'
import { fileDigest, sourceFingerprint } from '../../deploy/build-state.mjs'

test('开发配置只从模拟部署目录读取且不返回凭据字段', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'smail-runtime-config-'))
  await mkdir(path.join(root, 'config'))
  await mkdir(path.join(root, 'ssl'))
  await writeFile(path.join(root, 'config/config.json'), JSON.stringify({
    generation: 9, domains: { web: 'mail.example.test' }, database: { password: 'not-exported' }
  }))
  await writeFile(path.join(root, 'config/ssl.json'), JSON.stringify({
    meta: { generation: 9 },
    tls: { certificateRef: 'ssl/public.crt', privateKeyRef: 'ssl/private.key' }
  }))
  await writeFile(path.join(root, 'ssl/public.crt'), 'certificate')
  await writeFile(path.join(root, 'ssl/private.key'), 'key', { mode: 0o600 })
  const runtime = readDevelopmentRuntime(root)
  assert.equal(runtime.origin, 'https://mail.example.test')
  assert.equal(runtime.generation, 9)
  assert.equal(runtime.privateKeyPath, path.join(root, 'ssl/private.key'))
  assert.equal(JSON.stringify(runtime).includes('not-exported'), false)
  assert.equal(runtimeRoot, path.join(sourceRoot, 'yunx-test'))
  assert.throws(() => resolveRuntimeFile(root, '../outside.key'))
  assert.throws(() => resolveRuntimeFile(root, path.join(root, 'ssl/private.key')))
  await writeFile(path.join(root, 'config/ssl.json'), JSON.stringify({ meta: { generation: 10 }, tls: {} }))
  assert.throws(() => readDevelopmentRuntime(root), /事务尚未完成/)
})

test('开发恢复接口与页面统一使用 Web 域名', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'smail-runtime-recovery-domain-'))
  await mkdir(path.join(root, 'config'))
  await mkdir(path.join(root, 'ssl'))
  await writeFile(path.join(root, 'config/config.json'), JSON.stringify({
    generation: 5,
    domains: { web: 'web.example.test', primary: 'mail.example.test' }
  }))
  await writeFile(path.join(root, 'config/ssl.json'), JSON.stringify({
    meta: { generation: 5 },
    tls: { certificateRef: 'ssl/public.crt', privateKeyRef: 'ssl/private.key' }
  }))
  await writeFile(path.join(root, 'ssl/public.crt'), 'certificate')
  await writeFile(path.join(root, 'ssl/private.key'), 'key', { mode: 0o600 })

  const runtime = readDevelopmentRuntime(root)
  assert.equal(runtime.origin, 'https://web.example.test')
  assert.equal('mailOrigin' in runtime, false)
})

test('源码指纹检测编辑且忽略编译嵌入目录', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'smail-build-state-'))
  for (const name of ['backend', 'frontend', 'plugins/pmail_telegram_push', 'deploy']) {
    await mkdir(path.join(root, name), { recursive: true })
  }
  await writeFile(path.join(root, 'backend/main.go'), 'package main')
  const before = await sourceFingerprint(root)
  await mkdir(path.join(root, 'backend/dist'))
  await writeFile(path.join(root, 'backend/dist/index.html'), 'generated')
  assert.equal(await sourceFingerprint(root), before)
  await writeFile(path.join(root, 'backend/main.go'), 'package main\n')
  assert.notEqual(await sourceFingerprint(root), before)
  assert.equal((await fileDigest(path.join(root, 'backend/main.go'))).length, 64)
  await writeFile(path.join(root, 'empty.exe'), '')
  await assert.rejects(fileDigest(path.join(root, 'empty.exe')))
})

test('运行文件拒绝通过父目录链接访问另一个业务目录', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'smail-runtime-link-'))
  await mkdir(path.join(root, 'ssl'))
  await mkdir(path.join(root, 'data'))
  await writeFile(path.join(root, 'data/private.key'), 'fixture')
  await symlink(path.join(root, 'data'), path.join(root, 'ssl/linked'), process.platform === 'win32' ? 'junction' : 'dir')
  assert.throws(() => resolveRuntimeFile(root, 'ssl/linked/private.key'), /链接/)
})

test('部署和启动必须完整构建、验证产物且不覆盖运行配置', async () => {
  const read = name => readFile(path.join(sourceRoot, name), 'utf8')
  const deploy = await read('deploy/deploy-runtime.ps1')
  const start = await read('deploy/start-dev.ps1')
  assert.ok(deploy.indexOf("'build.ps1'") < deploy.indexOf('Copy-Item'))
  assert.match(deploy, /verify --deployed/)
  assert.doesNotMatch(deploy, /deployment-history/)
  assert.match(deploy, /Remove-Item -LiteralPath \$item\.FullName -Recurse -Force/)
  assert.doesNotMatch(deploy, /Copy-Item[^\n]*(config|data|ssl)/)
  assert.doesNotMatch(start, /SkipBuild|SkipFrontend/)
  assert.match(start, /\$env:SMAIL_ROOT = \$runtime/)
  assert.match(start, /\$workingDirectory = \$runtime/)
  assert.match(start, /build-state\.mjs'\) verify --deployed/)
  assert.match(await read('.gitignore'), /^\/yunx-test\/$/m)
})

test('部署启动检查兼容自动归档前后的密钥且拒绝双份冲突', async () => {
  for (const grouped of [false, true]) {
    const root = await mkdtemp(path.join(os.tmpdir(), 'smail-key-layout-'))
    for (const name of ['config', 'ssl', 'data', 'log']) await mkdir(path.join(root, name))
    await writeFile(path.join(root, 'config/config.json'), JSON.stringify({ setupState: 'ready', database: { type: 'mysql' } }))
    for (const name of ['smtp', 'email', 'plugins']) await writeFile(path.join(root, `config/${name}.json`), '{}')
    await writeFile(path.join(root, 'config/ssl.json'), JSON.stringify({ tls: { certificateRef: 'ssl/public.crt', privateKeyRef: 'ssl/private.key' } }))
    await writeFile(path.join(root, 'ssl/public.crt'), 'certificate')
    await writeFile(path.join(root, 'ssl/private.key'), 'key', { mode: 0o600 })
    for (const [folder, name] of [
      ['credentials', 'credential.key'],
      ['external-email-encryption', 'external-email-encryption.key'],
      ['external-email-fingerprint', 'external-email-fingerprint.key']
    ]) {
      if (grouped) await mkdir(path.join(root, 'ssl', folder))
      await writeFile(path.join(root, 'ssl', ...(grouped ? [folder] : []), name), Buffer.alloc(32, 1))
    }
    assert.equal(assertDeploymentLayout(root).setupState, 'ready')
    if (grouped) {
      await writeFile(path.join(root, 'ssl/credential.key'), Buffer.alloc(32, 2))
      assert.throws(() => assertDeploymentLayout(root), /冲突/)
    }
  }
})
