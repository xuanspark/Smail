import { spawnSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../../', import.meta.url))
const directory = path.join(root, 'test-log', `review-windows-${Date.now()}`)
mkdirSync(directory, { recursive: true })
// Windows PowerShell 5 使用显式 UTF-8 读取，避免依赖系统代码页解释测试源码。
const command = `
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$source = [IO.File]::ReadAllText('test/review/windows-install-findings.ps1', [Text.Encoding]::UTF8)
& ([scriptblock]::Create($source)) -WorkspaceRoot (Get-Location).Path
`
const result = spawnSync('powershell.exe', ['-NoProfile', '-Command', command], {
  cwd: root, encoding: 'utf8', windowsHide: true
})
writeFileSync(path.join(directory, 'windows.log'), (result.stdout || '') + (result.stderr || ''))
process.stdout.write(result.stdout || '')
process.stderr.write(result.stderr || '')
if (result.error) console.error(result.error)
console.log(`测试记录：${path.relative(root, directory)}`)
process.exitCode = result.status ?? 1
