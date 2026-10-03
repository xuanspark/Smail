import { cp, lstat, mkdir, mkdtemp, readdir, readFile, rename } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../', import.meta.url))
const args = process.argv.slice(2)
if (args.some(arg => !['telegram', '--prepare'].includes(arg))) throw new Error('未知前端构建参数')
const plugin = args.includes('telegram')
const source = path.join(root, 'out', ...(plugin ? ['plugins', 'telegram-ui'] : ['frontend']))
const target = path.join(root, ...(plugin ? ['plugins', 'pmail_telegram_push', 'hook', 'dist'] : ['backend', 'listen', 'http_server', 'dist']))

async function validateDirectory(directory) {
  const relative = path.relative(root, path.resolve(directory))
  if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) {
    throw new Error('编译资源路径越过项目目录，已停止')
  }
  let current = root
  for (const part of relative.split(path.sep)) {
    current = path.join(current, part)
    let info
    try { info = await lstat(current) } catch (error) {
      if (error.code === 'ENOENT') return false
      throw error
    }
    if (!info.isDirectory() || info.isSymbolicLink()) throw new Error('编译资源目录无效或为链接，已停止')
  }
  return true
}

async function archiveDirectories(directories) {
  const existing = []
  for (const directory of directories) {
    if (await validateDirectory(directory)) existing.push(directory)
  }
  if (!existing.length) return
  const historyRoot = path.join(root, 'out', 'build-history')
  await validateDirectory(historyRoot)
  await mkdir(historyRoot, { recursive: true })
  const history = await mkdtemp(path.join(historyRoot, plugin ? 'telegram-ui-' : 'frontend-'))
  for (const [index, directory] of existing.entries()) {
    const destination = path.join(history, `${index}-${path.basename(directory)}`)
    await validateDirectory(destination)
    await rename(directory, destination)
  }
}

if (args.includes('--prepare')) {
  // 只移动项目内的生成目录，不删除文件，也不让旧散列资源进入新产物。
  await archiveDirectories([source, target])
  console.log('旧前端编译资源已隔离，本次将从空目录构建。')
} else {
  // Go 嵌入文件不能越过模块目录；这里仅同步构建中间文件，不保存运行数据。
  if (!await validateDirectory(source)) throw new Error('前端构建目录不存在')
  await readFile(path.join(source, 'index.html'))
  await archiveDirectories([target])
  await mkdir(target, { recursive: true })
  for (const entry of await readdir(source)) {
    await cp(path.join(source, entry), path.join(target, entry), { recursive: true })
  }
  console.log('前端产物已生成至 out，并同步 Go 嵌入目录。')
}
