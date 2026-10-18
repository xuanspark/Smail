import { spawn } from 'node:child_process'
import { access, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const root = fileURLToPath(new URL('../', import.meta.url))
export const plugins = [
  { id: 'spam_block', executable: 'spam_block', directory: 'backend', source: './hooks/spam_block' },
  { id: 'telegram_push', executable: 'pmail_telegram_push', directory: 'plugins/pmail_telegram_push', source: '.' },
  { id: 'wechat_push', executable: 'wechat_push', directory: 'backend', source: './hooks/wechat_push' }
]
export const targets = ['linux_amd64', 'linux_arm64', 'windows_amd64', 'windows_arm64', 'darwin_amd64', 'darwin_arm64']

export function hostTarget() {
  const goos = { win32: 'windows', linux: 'linux', darwin: 'darwin' }[process.platform]
  const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch]
  return `${goos}_${goarch}`
}

export function buildPlan({ target = hostTarget(), plugin, development = false } = {}) {
  if (!targets.includes(target)) throw new Error(`不支持的编译平台：${target}`)
  if (plugin && !plugins.some(item => item.id === plugin)) throw new Error(`未知插件：${plugin}`)
  if (development && target !== hostTarget()) throw new Error('开发运行目录只能使用本机平台插件')
  const [goos, goarch] = target.split('_')
  return plugins.filter(item => !plugin || item.id === plugin).map(item => ({
    ...item,
    goos,
    goarch,
    output: path.join(root, 'out', 'plugins', ...(development ? [] : [target]), `${item.executable}${goos === 'windows' ? '.exe' : ''}`)
  }))
}

function run(command, args, cwd, env = process.env) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, env, stdio: 'inherit', windowsHide: true })
    child.once('error', reject)
    child.once('exit', code => code === 0 ? resolve() : reject(new Error(`构建失败：${command}，退出码 ${code}`)))
  })
}

export async function buildPlugins(args) {
  const options = {}
  let all = false
  for (const arg of args) {
    if (arg === '--all') all = true
    else if (arg === '--dev') options.development = true
    else if (arg.startsWith('--target=')) options.target = arg.slice(9)
    else if (arg.startsWith('--plugin=')) options.plugin = arg.slice(9)
    else throw new Error(`未知构建参数：${arg}`)
  }
  if (all && (options.target || options.development)) throw new Error('全平台编译不能同时指定单个平台或开发目录')
  const plan = (all ? targets : [options.target || hostTarget()]).flatMap(target => buildPlan({ ...options, target }))
  if (plan.some(item => item.id === 'telegram_push')) {
    const directory = path.join(root, 'plugins', 'pmail_telegram_push')
    const vite = path.join(directory, 'node_modules', 'vite', 'bin', 'vite.js')
    try { await access(vite) } catch { throw new Error('请先在 plugins/pmail_telegram_push 执行 npm ci 安装依赖') }
    // 构建并同步 Telegram 单文件界面，保证 Go 嵌入的是本次资源。
    await run(process.execPath, [path.join(root, 'deploy', 'prepare-frontend.mjs'), 'telegram', '--prepare'], root)
    await run(process.execPath, [vite, 'build'], directory)
    await run(process.execPath, [path.join(root, 'deploy', 'prepare-frontend.mjs'), 'telegram'], root)
  }
  for (const item of plan) {
    await mkdir(path.dirname(item.output), { recursive: true })
    await run('go', ['build', '-mod=mod', '-ldflags', '-s -w', '-o', item.output, item.source], path.join(root, item.directory), {
      ...process.env, CGO_ENABLED: '0', GOOS: item.goos, GOARCH: item.goarch
    })
    console.log(`插件已编译：${path.relative(root, item.output)}`)
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  buildPlugins(process.argv.slice(2)).catch(error => {
    console.error(error.message)
    process.exitCode = 1
  })
}
