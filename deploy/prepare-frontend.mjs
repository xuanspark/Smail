import { cp, mkdir, readdir, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../', import.meta.url))
const plugin = process.argv[2] === 'telegram'
const source = path.join(root, 'out', ...(plugin ? ['plugins', 'telegram-ui'] : ['frontend']))
const target = path.join(root, ...(plugin ? ['plugins', 'pmail_telegram_push', 'hook', 'dist'] : ['backend', 'listen', 'http_server', 'dist']))

// Go 嵌入文件不能越过模块目录；这里仅同步构建中间文件，不保存运行数据。
await readFile(path.join(source, 'index.html'))
await mkdir(target, { recursive: true })
for (const entry of await readdir(source)) {
  await cp(path.join(source, entry), path.join(target, entry), { recursive: true })
}
console.log('前端产物已生成至 out，并同步 Go 嵌入目录。')
