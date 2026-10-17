import { createHash } from 'node:crypto'
import { execFile } from 'node:child_process'
import { chmod, copyFile, lstat, mkdir, mkdtemp, readFile, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'
import { buildPlan, root, targets } from './build-plugins.mjs'

const execute = promisify(execFile)
const runtimeDirectories = ['config', 'data', 'ssl', 'log']

export async function resolveAppVersion(workspace = root) {
  if (process.env.SMAIL_VERSION) return process.env.SMAIL_VERSION
  try {
    const vueFile = path.join(workspace, 'frontend', 'src', 'views', 'components', 'HomeAside', 'HomeAside.vue')
    const content = await readFile(vueFile, 'utf8')
    const match = content.match(/class="app-version-badge">\s*(V\d+\.\d+\.\d+)\s*<\/span>/)
    if (match) return match[1]
  } catch (e) {}
  return 'dev'
}

export function packagePlan(target, workspace = root) {
  buildPlan({ target })
  const [goos, goarch] = target.split('_')
  const extension = goos === 'windows' ? '.exe' : ''
  const suffix = `${goos === 'darwin' ? 'mac' : goos}_${goarch}${extension}`
  const directory = path.join(workspace, 'out', 'package', `Smail_${target}`, 'smail')
  return [
    ...['smail', 'smail-admin', 'smail-config'].map(name => ({
      source: path.join(workspace, 'out', 'backend', `${name}_${suffix}`),
      target: path.join(directory, `${name}${extension}`)
    }))
  ]
}

async function digestFile(filename) {
  const info = await lstat(filename)
  if (!info.isFile() || info.size === 0) throw new Error(`发布产物无效：${filename}`)
  const bytes = await readFile(filename)
  return { bytes: info.size, sha256: createHash('sha256').update(bytes).digest('hex') }
}

async function createArchive(target, staging, filename) {
  const options = { cwd: staging, windowsHide: true }
  filename = path.basename(filename)
  if (target.startsWith('windows_')) {
    if (process.platform === 'win32') {
      await execute('tar', ['-a', '-cf', filename, 'smail'], options)
    } else {
      await execute('zip', ['-q', '-r', filename, 'smail'], options)
    }
    return
  }
  let tar = process.env.SMAIL_TAR || 'tar'
  const args = ['-czf', filename]
  if (process.platform === 'win32') {
    if (!process.env.SMAIL_TAR) {
      const { stdout } = await execute('git', ['--exec-path'], { windowsHide: true })
      tar = path.resolve(stdout.trim(), '../../../usr/bin/tar.exe')
    }
    const { stdout } = await execute(tar, ['--version'], { windowsHide: true })
    if (!stdout.includes('GNU tar')) throw new Error('Windows 打包 Linux/macOS 时，请通过 SMAIL_TAR 指定 GNU tar，以保留可执行权限')
    const environment = Object.entries(process.env)
    const searchPath = environment.find(([name]) => name.toLowerCase() === 'path')?.[1] || ''
    options.env = {
      ...Object.fromEntries(environment.filter(([name]) => name.toLowerCase() !== 'path')),
      PATH: `${path.dirname(tar)}${path.delimiter}${searchPath}`
    }
    args.push('--mode=755')
  }
  await execute(tar, [...args, 'smail'], options)
}

async function preservePrevious(outputRoot, destinations) {
  let history
  for (const destination of destinations) {
    const relative = path.relative(outputRoot, destination)
    if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) throw new Error('归档路径必须位于构建输出目录内')
    let info
    try { info = await lstat(destination) } catch (error) {
      if (error.code === 'ENOENT') continue
      throw error
    }
    if (info.isSymbolicLink()) throw new Error(`不能归档符号链接：${destination}`)
    if (!history) {
      const historyRoot = path.join(outputRoot, 'build-history')
      await mkdir(historyRoot, { recursive: true })
      history = await mkdtemp(path.join(historyRoot, 'release-'))
    }
    await rename(destination, path.join(history, path.basename(destination)))
  }
}

export async function packageRelease(selectedTargets = targets, { workspace = root } = {}) {
  if (!selectedTargets.length || new Set(selectedTargets).size !== selectedTargets.length) throw new Error('打包平台不能为空或重复')
  const plan = selectedTargets.flatMap(target => packagePlan(target, workspace))
  // 先校验全部输入，再从空目录打包；旧包及其中的数据只归档，不混入新包。
  for (const item of plan) await digestFile(item.source)
  const outputRoot = path.resolve(workspace, 'out')
  const packageRoot = path.join(outputRoot, 'package')
  const releaseRoot = path.join(outputRoot, 'release')
  const receiptRoot = path.join(outputRoot, 'release-receipts')
  for (const directory of [packageRoot, releaseRoot, receiptRoot]) await mkdir(directory, { recursive: true })
  const releases = []
  for (const target of selectedTargets) {
    const name = `Smail_${target}`
    const directory = path.join(packageRoot, name)
    const staging = await mkdtemp(path.join(packageRoot, '.staging-'))
    const runtime = path.join(staging, 'smail')
    await mkdir(runtime, { mode: 0o755 })
    for (const name of runtimeDirectories) await mkdir(path.join(runtime, name), { mode: 0o755 })
    const files = []
    for (const item of packagePlan(target, workspace)) {
      const name = path.basename(item.target)
      const destination = path.join(runtime, name)
      await copyFile(item.source, destination)
      await chmod(destination, 0o755)
      files.push({ name, ...await digestFile(destination) })
    }
    const version = await resolveAppVersion(workspace)
    const baseExt = target.startsWith('windows_') ? 'zip' : 'tar.gz'
    const archiveName = version && version !== 'dev' ? `Smail_${version}_${target}.${baseExt}` : `${name}.${baseExt}`
    const legacyArchiveName = `${name}.${baseExt}`
    const stagedArchive = path.join(staging, archiveName)
    await createArchive(target, staging, stagedArchive)
    const archive = path.join(releaseRoot, archiveName)
    const receipt = path.join(receiptRoot, `${name}.json`)
    const stagedReceipt = path.join(staging, `${name}.json`)
    await writeFile(stagedReceipt, JSON.stringify({
      version: version || process.env.SMAIL_VERSION || 'dev', gitHash: process.env.GIT_HASH || '',
      protocolVersion: 1, target, plugins: buildPlan({ target }).map(item => item.id), files,
      archive: { name: archiveName, ...await digestFile(stagedArchive) },
      createdAt: new Date().toISOString()
    }, null, 2) + '\n')
    await preservePrevious(outputRoot, [directory, archive, receipt])
    await rename(stagedArchive, archive)
    if (archiveName !== legacyArchiveName) {
      const legacyArchive = path.join(releaseRoot, legacyArchiveName)
      try { await copyFile(archive, legacyArchive) } catch (e) {}
    }
    await rename(stagedReceipt, receipt)
    await rename(staging, directory)
    releases.push({ target, directory, archive, receipt })
  }
  console.log(`发布包已生成：${releases.map(item => item.archive).join('、')}`)
  return releases
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  packageRelease(process.argv.length > 2 ? process.argv.slice(2) : targets).catch(error => {
    console.error(`发布打包失败：${error.message}`)
    process.exitCode = 1
  })
}
