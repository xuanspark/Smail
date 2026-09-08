import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { once } from 'node:events'
import { mkdtemp, readFile, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const root = fileURLToPath(new URL('../../', import.meta.url))
const lockScript = path.join(root, 'deploy', 'workspace-lock.ps1')
const shellArguments = ['-NoProfile', '-ExecutionPolicy', 'Bypass']
const windowsOnly = { skip: process.platform !== 'win32', timeout: 30000 }
const quote = value => `'${value.replaceAll("'", "''")}'`

function acquire(directory) {
  return `$ErrorActionPreference = 'Stop'; . ${quote(lockScript)}; $lock = Enter-SmailWorkspaceLock -Root ${quote(directory)};`
}

function probe(directory) {
  return spawnSync('powershell.exe', [...shellArguments, '-Command',
    `${acquire(directory)} try { Write-Output 'acquired' } finally { $lock.ReleaseMutex(); $lock.Dispose() }`,
  ], { encoding: 'utf8', windowsHide: true, timeout: 10000 })
}

async function hold(directory, context) {
  const child = spawn('powershell.exe', [...shellArguments, '-Command',
    `${acquire(directory)} try { Write-Output 'ready'; [Console]::In.ReadLine() | Out-Null } finally { $lock.ReleaseMutex(); $lock.Dispose() }`,
  ], { windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] })
  context.after(() => {
    if (child.exitCode === null) child.kill()
  })
  await new Promise((resolve, reject) => {
    let output = ''
    let errors = ''
    child.stderr.on('data', data => { errors += data })
    child.stdout.on('data', data => {
      output += data
      if (output.includes('ready')) resolve()
    })
    child.once('error', reject)
    child.once('exit', code => reject(new Error(`锁持有进程提前退出：${code}；${errors}`)))
  })
  return child
}

async function release(child) {
  const exited = once(child, 'exit')
  child.stdin.end('\n')
  await exited
}

test('构建入口在计算指纹和移动资源之前获取互斥量', async () => {
  const source = await readFile(path.join(root, 'deploy', 'build.ps1'), 'utf8')
  const acquireIndex = source.indexOf('$workspaceLock = Enter-SmailWorkspaceLock')
  assert.ok(acquireIndex > 0)
  assert.ok(acquireIndex < source.indexOf('fingerprint'))
  assert.ok(acquireIndex < source.indexOf('Move-Item'))
  assert.match(source, /finally\s*\{\s*\$workspaceLock\.ReleaseMutex\(\)/)
})

test('同工作区互斥，不同工作区可以并行，正常退出后可重试', windowsOnly, async context => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'smail-lock-'))
  const other = await mkdtemp(path.join(os.tmpdir(), 'smail-lock-other-'))
  const child = await hold(directory, context)
  assert.notEqual(probe(directory).status, 0)
  assert.equal(probe(other).status, 0)
  await release(child)
  assert.equal(probe(directory).status, 0)
})

test('持有进程异常退出后无需删除文件即可重新获取锁', windowsOnly, async context => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'smail-lock-crash-'))
  const child = await hold(directory, context)
  const exited = once(child, 'exit')
  child.kill()
  await exited
  assert.equal(probe(directory).status, 0)
})

test('Go 入口与构建共用工作区锁，并保留 Go 退出码', windowsOnly, async context => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'smail-go-lock-'))
  const invocation = path.join(directory, 'invocation.json')
  const run = () => spawnSync('powershell.exe', [...shellArguments, '-File',
    path.join(root, 'test', 'deployment', 'run-go.ps1'), '-InvocationFile', invocation,
  ], { encoding: 'utf8', windowsHide: true, timeout: 10000 })
  await writeFile(invocation, JSON.stringify({ arguments: ['version'] }))
  const child = await hold(root, context)
  assert.notEqual(run().status, 0)
  await release(child)
  const success = run()
  assert.equal(success.status, 0, success.stderr)
  assert.match(success.stdout, /go version/)
  await writeFile(invocation, JSON.stringify({ arguments: ['smail-invalid-test-command'] }))
  assert.notEqual(run().status, 0)
})
