import { spawn } from 'node:child_process'
import { createWriteStream } from 'node:fs'
import { mkdir, readdir, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../', import.meta.url))
const [suite = 'all', ...extra] = process.argv.slice(2)
if (!['all', 'frontend', 'backend', 'deployment'].includes(suite)) {
  console.error('用法：node test/run.mjs [all|frontend|backend|deployment] [Go 测试参数]')
  process.exit(2)
}
const stamp = new Date().toISOString().replace(/[:.]/g, '-')
const directory = path.join(root, 'test-log', `run-${stamp}-${process.pid}`)
const temporary = path.join(directory, 'tmp')
await mkdir(temporary, { recursive: true })

async function run(name, command, args, cwd) {
  const log = createWriteStream(path.join(directory, `${name}.log`))
  // 测试使用独立临时根目录，不能继承开发服务的真实配置根目录。
  const env = { ...process.env, SMAIL_ROOT: temporary, GOTMPDIR: temporary, TMPDIR: temporary, TEMP: temporary, TMP: temporary }
  const child = spawn(command, args, { cwd, env, windowsHide: true, stdio: ['inherit', 'pipe', 'pipe'] })
  child.stdout.on('data', data => { process.stdout.write(data); log.write(data) })
  child.stderr.on('data', data => { process.stderr.write(data); log.write(data) })
  const code = await new Promise(resolve => {
    child.once('error', error => { console.error(`测试启动失败：${error.message}`); resolve(1) })
    child.once('close', value => resolve(value ?? 1))
  })
  await new Promise(resolve => log.end(resolve))
  return code
}

let result = 0
if (suite === 'all' || suite === 'frontend') {
  const tests = (await readdir(path.join(root, 'test', 'frontend')))
    .filter(name => name.endsWith('.test.mjs'))
    .map(name => path.join(root, 'test', 'frontend', name))
  if (!tests.length) throw new Error('没有找到前端测试，拒绝空测试通过')
  result = await run('frontend', process.execPath, ['--test', ...tests], path.join(root, 'frontend'))
}
if ((suite === 'all' || suite === 'deployment') && result === 0) {
  const tests = (await readdir(path.join(root, 'test', 'deployment')))
    .filter(name => name.endsWith('.test.mjs'))
    .map(name => path.join(root, 'test', 'deployment', name))
  if (!tests.length) throw new Error('没有找到部署测试，拒绝空测试通过')
  result = await run('deployment', process.execPath, ['--test', ...tests], root)
}
if ((suite === 'all' || suite === 'backend') && result === 0) {
  const args = extra[0] === '--' ? extra.slice(1) : extra
  // 独立回归源码集中放在 test/backend，通过 overlay 保留同包测试语义。
  const replacements = {
    'backend/config/deployment_state_test.go': 'test/backend/deployment_state_test.go',
    'backend/utils/runtimepath/layout_test.go': 'test/backend/runtime_layout_test.go',
    'backend/config/data_paths_test.go': 'test/backend/data_paths_test.go',
    'backend/config/ssl_custom_regression_test.go': 'test/backend/ssl_custom_test.go',
    'backend/config/webauthn_defaults_test.go': 'test/backend/webauthn_config_test.go',
    'backend/res_init/webauthn_runtime_test.go': 'test/backend/webauthn_runtime_test.go',
    'backend/services/credentials/client_regression_test.go': 'test/backend/protocol_credentials_test.go',
    'backend/controllers/client_regression_test.go': 'test/backend/protocol_access_test.go',
    'backend/res_init/client_listeners_test.go': 'test/backend/mail_listeners_test.go',
  }
  const overlay = path.join(directory, 'overlay.json')
  const Replace = Object.fromEntries(Object.entries(replacements).map(([target, source]) => [
    path.join(root, target), path.join(root, source),
  ]))
  await writeFile(overlay, JSON.stringify({ Replace }, null, 2) + '\n')
  // 显式传入的审查 overlay 保持原有行为，默认入口自动包含本轮回归。
  const overlayArgs = args.some(arg => arg === '-overlay' || arg.startsWith('-overlay=')) ? [] : [`-overlay=${overlay}`]
  const goArguments = ['test', ...overlayArgs, ...(args.length ? args : ['./...'])]
  if (process.platform === 'win32') {
    const invocation = path.join(directory, 'go-invocation.json')
    await writeFile(invocation, JSON.stringify({ arguments: goArguments }, null, 2) + '\n')
    result = await run('backend', 'powershell.exe', [
      '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File',
      path.join(root, 'test', 'deployment', 'run-go.ps1'), '-InvocationFile', invocation,
    ], root)
  } else {
    result = await run('backend', 'go', goArguments, path.join(root, 'backend'))
  }
}
console.log(`测试记录：${path.relative(root, directory)}`)
process.exitCode = result
