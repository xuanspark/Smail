import { spawn } from 'node:child_process'
import { createWriteStream } from 'node:fs'
import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../../', import.meta.url))
const directory = path.join(root, 'test-log', `review-telegram-${new Date().toISOString().replace(/[:.]/g, '-')}`)
const temporary = path.join(directory, 'tmp')
await mkdir(temporary, { recursive: true })
const overlay = path.join(directory, 'overlay.json')
await writeFile(overlay, JSON.stringify({ Replace: {
  [path.join(root, 'plugins/pmail_telegram_push/hook/audit_findings_test.go')]:
    path.join(root, 'test/review/telegram_findings_test.go')
} }, null, 2) + '\n')

// 仅使用内存 HTTP 客户端；临时根和日志与开发配置隔离。
const log = createWriteStream(path.join(directory, 'telegram.log'))
const env = { ...process.env, SMAIL_ROOT: temporary, GOTMPDIR: temporary, TMPDIR: temporary, TEMP: temporary, TMP: temporary }
const extra = process.argv.slice(2)
const child = spawn('go', ['test', '-mod=readonly', `-overlay=${overlay}`, '-count=1', '-v', '-run=^TestAudit', ...extra, './hook'], {
  cwd: path.join(root, 'plugins/pmail_telegram_push'), env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe']
})
child.stdout.on('data', data => { process.stdout.write(data); log.write(data) })
child.stderr.on('data', data => { process.stderr.write(data); log.write(data) })
const code = await new Promise(resolve => {
  child.once('error', error => { console.error(error); resolve(1) })
  child.once('close', value => resolve(value ?? 1))
})
await new Promise(resolve => log.end(resolve))
console.log(`测试记录：${path.relative(root, directory)}`)
process.exitCode = code
