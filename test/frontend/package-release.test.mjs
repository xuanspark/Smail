import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { execFile } from 'node:child_process'
import { mkdir, mkdtemp, readFile, readdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'
import test from 'node:test'
import { root, targets } from '../../deploy/build-plugins.mjs'
import { packagePlan, packageRelease } from '../../deploy/package.mjs'

const execute = promisify(execFile)

async function fixture(selectedTargets) {
  const directory = path.join(root, 'test-log', 'package-tests')
  await mkdir(directory, { recursive: true })
  const workspace = await mkdtemp(path.join(directory, 'release-'))
  for (const target of selectedTargets) {
    for (const item of packagePlan(target, workspace)) {
      await mkdir(path.dirname(item.source), { recursive: true })
      await writeFile(item.source, `fixture:${target}:${path.basename(item.source)}`)
    }
  }
  return workspace
}

test('六个平台发布包只包含三个程序和四个空目录，解压后内容与回执一致', async () => {
  const workspace = await fixture(targets)
  const releases = await packageRelease(targets, { workspace })
  for (const release of releases) {
    const extension = release.target.startsWith('windows_') ? '.exe' : ''
    const programs = ['smail', 'smail-admin', 'smail-config'].map(name => name + extension)
    const directories = ['config', 'data', 'log', 'ssl']
    const { stdout } = await execute('tar', ['-tf', release.archive], { windowsHide: true })
    assert.deepEqual(stdout.trim().split(/\r?\n/).sort(), [
      'smail/', ...programs.map(name => `smail/${name}`), ...directories.map(name => `smail/${name}/`)
    ].sort())
    const extracted = path.join(workspace, 'extracted', release.target)
    await mkdir(extracted, { recursive: true })
    await execute('tar', ['-xf', release.archive, '-C', extracted], { windowsHide: true })
    const receipt = JSON.parse(await readFile(release.receipt, 'utf8'))
    assert.equal(receipt.files.length, 3)
    assert.equal(receipt.archive.name, path.basename(release.archive))
    const archiveBytes = await readFile(release.archive)
    assert.equal(receipt.archive.bytes, archiveBytes.length)
    assert.equal(receipt.archive.sha256, createHash('sha256').update(archiveBytes).digest('hex'))
    for (const name of directories) assert.deepEqual(await readdir(path.join(extracted, 'smail', name)), [])
    for (const item of packagePlan(release.target, workspace)) {
      const name = path.basename(item.target)
      const bytes = await readFile(path.join(extracted, 'smail', name))
      assert.deepEqual(bytes, await readFile(item.source))
      const record = receipt.files.find(file => file.name === name)
      assert.equal(record.bytes, bytes.length)
      assert.equal(record.sha256, createHash('sha256').update(bytes).digest('hex'))
    }
    if (!extension) {
      const { stdout: details } = await execute('tar', ['-tvf', release.archive], { windowsHide: true })
      for (const name of programs) {
        const line = details.split(/\r?\n/).find(value => value.endsWith(`smail/${name}`))
        assert.match(line, /^-rwxr-xr-x/)
      }
    }
  }
})

test('重复打包保留旧数据但不会将其带入新发布包', async () => {
  const target = 'windows_amd64'
  const workspace = await fixture([target])
  const [previous] = await packageRelease([target], { workspace })
  const database = path.join(previous.directory, 'smail', 'data', 'private.db')
  await writeFile(database, 'private fixture')
  await writeFile(path.join(previous.directory, 'smail', 'release.json'), 'old metadata')
  await packageRelease([target], { workspace })
  const { stdout } = await execute('tar', ['-tf', previous.archive], { windowsHide: true })
  assert.doesNotMatch(stdout, /private\.db|release\.json/)
  const historyRoot = path.join(workspace, 'out', 'build-history')
  const history = await readdir(historyRoot)
  assert.equal(history.length, 1)
  assert.equal(await readFile(path.join(historyRoot, history[0], `Smail_${target}`, 'smail', 'data', 'private.db'), 'utf8'), 'private fixture')
})

test('缺失或空的输入程序阻止打包，不能产生部分发布目录', async () => {
  const workspace = await fixture(['windows_amd64'])
  await assert.rejects(packageRelease(['windows_amd64', 'linux_amd64'], { workspace }), { code: 'ENOENT' })
  await assert.rejects(readdir(path.join(workspace, 'out', 'package')), { code: 'ENOENT' })
  await writeFile(packagePlan('windows_amd64', workspace)[2].source, '')
  await assert.rejects(packageRelease(['windows_amd64'], { workspace }), /发布产物无效/)
  await assert.rejects(readdir(path.join(workspace, 'out', 'package')), { code: 'ENOENT' })
})
