import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { lstat, mkdir, readFile, readdir, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { runtimeRoot, sourceRoot } from './runtime-config.mjs'

const receiptPath = path.join(sourceRoot, 'out', 'build-state.json')
const skipped = new Set(['node_modules', 'dist', 'dist-ssr', 'output', 'bundled', '__pycache__', '.git'])
const sourceDirectories = ['backend', 'frontend', 'plugins/pmail_telegram_push', 'deploy']

export async function sourceFingerprint(root = sourceRoot) {
  const hash = createHash('sha256')
  async function visit(relative) {
    const entries = await readdir(path.join(root, relative), { withFileTypes: true })
    entries.sort((left, right) => left.name.localeCompare(right.name, 'en'))
    for (const entry of entries) {
      if (skipped.has(entry.name) || entry.name.startsWith('.tmp-') || entry.name === 'auto-imports.d.ts' || entry.name === 'components.d.ts') continue
      const name = `${relative}/${entry.name}`
      if (entry.isDirectory()) {
        await visit(name)
      } else if (!/\.(exe|log|pid|db|tsbuildinfo)$/.test(entry.name)) {
        if (!entry.isFile()) throw new Error(`源码包含链接或特殊文件：${name}`)
        hash.update(name).update('\0').update(await readFile(path.join(root, name))).update('\0')
      }
    }
  }
  for (const directory of sourceDirectories) await visit(directory)
  return hash.digest('hex')
}

export async function fileDigest(filename) {
  const info = await lstat(filename)
  if (!info.isFile() || info.isSymbolicLink() || info.size === 0) throw new Error('编译产物为空或不是普通文件')
  return createHash('sha256').update(await readFile(filename)).digest('hex')
}

const artifacts = [
  ['out/backend/smail.exe', 'smail.exe'],
  ['out/backend/smail-admin.exe', 'smail-admin.exe'],
  ['out/plugins/spam_block.exe', null],
  ['out/plugins/pmail_telegram_push.exe', null],
  ['out/plugins/wechat_push.exe', null]
]

export async function sealBuild(expectedFingerprint) {
  const actual = await sourceFingerprint()
  if (!expectedFingerprint || actual !== expectedFingerprint) throw new Error('编译期间源码发生变化，请重新编译，拒绝部署不一致产物')
  const files = await Promise.all(artifacts.map(async ([source, target]) => ({
    source, target, sha256: await fileDigest(path.join(sourceRoot, source))
  })))
  const receipt = {
    sourceFingerprint: actual,
    gitHash: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: sourceRoot, encoding: 'utf8', windowsHide: true }).trim(),
    builtAt: new Date().toISOString(),
    files
  }
  await mkdir(path.dirname(receiptPath), { recursive: true })
  await writeFile(`${receiptPath}.tmp`, JSON.stringify(receipt, null, 2) + '\n')
  await rename(`${receiptPath}.tmp`, receiptPath)
  return receipt
}

export async function verifyBuild({ deployed = false } = {}) {
  const receipt = JSON.parse(await readFile(receiptPath, 'utf8'))
  if (receipt.sourceFingerprint !== await sourceFingerprint()) throw new Error('源码已变化，必须重新编译后才能启动')
  if (JSON.stringify(receipt.files.map(item => [item.source, item.target])) !== JSON.stringify(artifacts)) {
    throw new Error('编译清单不完整，请重新编译')
  }
  for (const item of receipt.files) {
    if (await fileDigest(path.join(sourceRoot, item.source)) !== item.sha256) throw new Error(`编译产物校验失败：${item.source}`)
    if (deployed && item.target && await fileDigest(path.join(runtimeRoot, item.target)) !== item.sha256) {
      throw new Error(`部署产物不是最新编译结果：${item.target}`)
    }
  }
  return receipt
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    switch (process.argv[2]) {
      case 'fingerprint': console.log(await sourceFingerprint()); break
      case 'seal': await sealBuild(process.argv[3]); break
      case 'verify': await verifyBuild({ deployed: process.argv.includes('--deployed') }); break
      default: throw new Error('用法：build-state.mjs fingerprint|seal|verify')
    }
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
