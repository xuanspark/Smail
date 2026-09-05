import { spawn } from 'node:child_process'
import { createWriteStream } from 'node:fs'
import { mkdir, readdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../', import.meta.url))
const [suite = 'all', ...extra] = process.argv.slice(2)
if (!['all', 'frontend', 'backend'].includes(suite)) {
  console.error('用法：node test/run.mjs [all|frontend|backend] [Go 测试参数]')
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
if (suite !== 'backend') {
  const tests = (await readdir(path.join(root, 'test', 'frontend')))
    .filter(name => name.endsWith('.test.mjs'))
    .map(name => path.join(root, 'test', 'frontend', name))
  if (!tests.length) throw new Error('没有找到前端测试，拒绝空测试通过')
  result = await run('frontend', process.execPath, ['--test', ...tests], path.join(root, 'frontend'))
}
if (suite !== 'frontend' && result === 0) {
  const args = extra[0] === '--' ? extra.slice(1) : extra
  result = await run('backend', 'go', ['test', ...(args.length ? args : ['./...'])], path.join(root, 'backend'))
}
console.log(`测试记录：${path.relative(root, directory)}`)
process.exitCode = result
