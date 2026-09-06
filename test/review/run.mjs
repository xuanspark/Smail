import { spawn } from 'node:child_process'
import { mkdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../../', import.meta.url))
const stamp = new Date().toISOString().replace(/[:.]/g, '-')
const directory = path.join(root, 'test-log', `audit-${stamp}-${process.pid}`)
await mkdir(directory, { recursive: true })
const replacements = {
  'backend/res_init/audit_findings_test.go': 'test/review/lifecycle_findings_test.go',
  'backend/hooks/wechat_push/audit_findings_test.go': 'test/review/wechat_findings_test.go',
  'backend/hooks/framework/audit_findings_test.go': 'test/review/plugin_framework_findings_test.go',
  'backend/utils/async/audit_findings_test.go': 'test/review/async_findings_test.go',
  'backend/utils/address/audit_findings_test.go': 'test/review/address_findings_test.go',
  'backend/services/totp/audit_findings_test.go': 'test/review/totp_findings_test.go',
  'backend/services/setup/ssl/audit_findings_test.go': 'test/review/ssl_findings_test.go',
  'backend/services/setup/ssl/audit_proxy_test.go': 'test/review/ssl_proxy_test.go',
  'backend/listen/cron_server/audit_ssl_test.go': 'test/review/cron_ssl_test.go',
  'backend/services/setup/audit_findings_test.go': 'test/review/setup_findings_test.go',
  'backend/config/audit_findings_test.go': 'test/review/config_findings_test.go',
  'backend/services/dbmigration/audit_findings_test.go': 'test/review/dbmigration_findings_test.go',
  'backend/listen/http_server/audit_findings_test.go': 'test/review/http_findings_test.go',
  'backend/listen/http_server/audit_external_email_findings_test.go': 'test/review/external_email_http_findings_test.go',
  'backend/listen/http_server/audit_attachment_findings_test.go': 'test/review/attachment_http_findings_test.go',
  'backend/services/list/audit_findings_test.go': 'test/review/list_findings_test.go',
  'backend/services/recovery/audit_findings_test.go': 'test/review/recovery_findings_test.go',
  'backend/listen/imap_server/audit_findings_test.go': 'test/review/imap_findings_test.go',
  'backend/listen/imap_server/audit_wire_findings_test.go': 'test/review/imap_wire_findings_test.go',
  'backend/controllers/audit_findings_test.go': 'test/review/runtime_findings_test.go',
  'backend/controllers/audit_controller_findings_test.go': 'test/review/controller_findings_test.go',
  'backend/controllers/audit_proxy_test.go': 'test/review/proxy_controller_test.go',
  'backend/internal/gopop/audit_findings_test.go': 'test/review/pop3_parser_findings_test.go',
  'backend/listen/pop3_server/audit_findings_test.go': 'test/review/pop3_action_findings_test.go',
  'backend/dto/parsemail/audit_findings_test.go': 'test/review/message_findings_test.go',
  'backend/listen/smtp_server/audit_findings_test.go': 'test/review/smtp_findings_test.go'
}
const Replace = Object.fromEntries(
  Object.entries(replacements).map(([target, source]) => [path.join(root, target), path.join(root, source)])
)
const overlay = path.join(directory, 'overlay.json')
await writeFile(overlay, JSON.stringify({ Replace }, null, 2) + '\n')

// Go overlay 注入独立审查测试，不改动后端源码和默认测试入口。
const pop3Only = process.argv.includes('--pop3-only')
const messageOnly = process.argv.includes('--message-only')
const migrationOnly = process.argv.includes('--migration-only')
const configOnly = process.argv.includes('--config-only')
const controllersOnly = process.argv.includes('--controllers-only')
const httpOnly = process.argv.includes('--http-only')
const setupOnly = process.argv.includes('--setup-only')
const sslOnly = process.argv.includes('--ssl-only')
const totpOnly = process.argv.includes('--totp-only')
const frameworkOnly = process.argv.includes('--framework-only')
const asyncOnly = process.argv.includes('--async-only')
const addressOnly = process.argv.includes('--address-only')
const packages = frameworkOnly ? ['./hooks/framework'] : asyncOnly ? ['./utils/async'] : addressOnly ? ['./utils/address'] : totpOnly ? ['./services/totp'] : sslOnly ? ['./services/setup/ssl', './listen/cron_server'] : setupOnly ? ['./services/setup'] : httpOnly ? ['./listen/http_server'] : controllersOnly ? ['./controllers'] : configOnly ? ['./config'] : migrationOnly ? ['./services/dbmigration'] : pop3Only ? ['./internal/gopop', './listen/pop3_server'] : messageOnly ? [
  './dto/parsemail', './listen/smtp_server'
] : [
  './res_init',
  './listen/cron_server',
  './hooks/wechat_push',
  './services/dbmigration', './listen/http_server', './services/list', './services/recovery',
  './listen/imap_server', './controllers', './internal/gopop', './listen/pop3_server',
  './dto/parsemail', './listen/smtp_server', './config', './services/setup', './services/setup/ssl', './services/totp', './hooks/framework'
]
const extra = process.argv.slice(2).filter(argument => !['--pop3-only', '--message-only', '--migration-only', '--config-only', '--controllers-only', '--http-only', '--setup-only', '--ssl-only', '--totp-only', '--framework-only', '--async-only', '--address-only'].includes(argument))
const selection = extra.some(argument => argument === '-run' || argument.startsWith('-run=')) ? [] : ['-run=^TestAudit']
const child = spawn(process.execPath, [
  path.join(root, 'test/run.mjs'), 'backend', `-overlay=${overlay}`,
  ...extra, '-count=1', '-v', ...selection, ...packages
], { cwd: root, windowsHide: true, stdio: 'inherit' })
child.once('error', error => {
  console.error(error)
  process.exitCode = 1
})
child.once('close', code => { process.exitCode = code ?? 1 })
