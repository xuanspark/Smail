import { cp, lstat, mkdir, readdir, rename } from 'node:fs/promises'
import path from 'node:path'
import { buildPlan, root } from './build-plugins.mjs'

const plan = buildPlan({ target: process.argv[2] })
const destination = path.join(root, 'backend/cmd/smail/bundled')
for (const item of plan) {
  const info = await lstat(item.output)
  if (!info.isFile() || info.isSymbolicLink()) throw new Error('待嵌入插件不是普通文件')
}
try {
  await readdir(destination)
  const history = path.join(root, 'out/build-history', `plugins-${Date.now()}`)
  await mkdir(path.dirname(history), { recursive: true })
  const info = await lstat(destination)
  if (!info.isDirectory() || info.isSymbolicLink()) throw new Error('嵌入目录无效')
  await rename(destination, history)
} catch (error) {
  if (error.code !== 'ENOENT') throw error
}
await mkdir(destination, { recursive: true })
for (const item of plan) await cp(item.output, path.join(destination, path.basename(item.output)), { force: false })
console.log('三个插件已准备嵌入主程序。')
