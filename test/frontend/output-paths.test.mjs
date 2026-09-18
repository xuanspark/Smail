import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { buildPlan, hostTarget, plugins, targets } from '../../deploy/build-plugins.mjs'
import { packagePlan } from '../../deploy/package.mjs'

const root = fileURLToPath(new URL('../../', import.meta.url))
const read = relative => readFile(path.join(root, relative), 'utf8')

test('跨平台构建输出不得越过项目 out 目录', async () => {
  const content = await read('Makefile')
  assert.match(content, /GO_VERSION \?= \$\(shell cd backend && go version\)/)
  const commands = [...content.matchAll(/cd ([^\s]+) && [^\n]*go build [^\n]* -o ([^\s]+) /g)]
  assert.equal(commands.length, 18)
  for (const [, directory, output] of commands) {
    const relative = path.relative(path.join(root, 'out'), path.resolve(root, directory, output))
    assert.ok(relative && !relative.startsWith('..') && !path.isAbsolute(relative), output)
  }
})

test('前端构建必须自动同步 Go 嵌入资源', async () => {
  const frontend = JSON.parse(await read('frontend/package.json'))
  const telegram = JSON.parse(await read('plugins/pmail_telegram_push/package.json'))
  assert.equal(frontend.scripts.postbuild, 'node ../deploy/prepare-frontend.mjs')
  assert.equal(telegram.scripts['postbuild-only'], 'node ../../deploy/prepare-frontend.mjs telegram')
  assert.match(await read('frontend/vite.config.js'), /\.\.\/out\/frontend/)
  assert.match(await read('plugins/pmail_telegram_push/vite.config.ts'), /\.\.\/\.\.\/out\/plugins\/telegram-ui/)
})

test('前端入口按需加载 Element Plus，并延迟设置页和写信编辑器', async () => {
  const main = await read('frontend/src/main.js')
  const mainApp = await read('frontend/src/main-app.js')
  const vite = await read('frontend/vite.config.js')
  const header = await read('frontend/src/views/components/HomeHeader/HomeHeader.js')
  assert.doesNotMatch(main, /app\.use\(ElementPlus\)/)
  assert.doesNotMatch(main, /element-plus\/dist\/index\.css/)
  assert.match(main, /public-recovery\.js/)
  assert.match(mainApp, /element-services\.css/)
  assert.match(vite, /manualChunks\(id\)/)
  assert.match(header, /defineAsyncComponent/)
  assert.match(header, /import\('\.\.\/ConfigSettings\/ConfigSettings\.vue'\)/)
})

test('开发服务器将匿名恢复入口和接口安全转发到后端', async () => {
  const vite = await read('frontend/vite.config.js')
  assert.match(vite, /"\/public-password-recovery": developmentBackendProxy\(backendUrl, backendOrigin\)/)
  assert.match(vite, /"\/rest\/api\/recovery": developmentBackendProxy\(backendUrl, backendOrigin\)/)
  assert.doesNotMatch(vite, /publicRecoveryOrigin|mailOrigin/)
})

test('开发启动必须固定运行配置根目录并集中保存日志', async () => {
  const content = await read('deploy/start-dev.ps1')
  assert.match(content, /\$env:SMAIL_ROOT = \$runtime\r?\n/)
  assert.match(content, /\$legacyConfig = Join-Path \$root 'backend\\config'/)
  assert.match(content, /Test-Path -LiteralPath \(Join-Path \$legacyConfig \$name\)/)
  assert.match(content, /\$workingDirectory = \$runtime/)
  for (const name of ['spam_block.exe', 'pmail_telegram_push.exe', 'wechat_push.exe']) assert.ok(content.includes(name))
  assert.match(content, /Join-Path \$runtime 'smail\.exe'/)
  assert.match(content, /Join-Path \$root 'test-log\\service-launch'/)
  assert.match(content, /-WindowStyle Hidden/)
  assert.doesNotMatch(content, /SkipBuild/)
  assert.match(content, /--dev-disable-admin-reset/)
  assert.match(content, /SMAIL_HTTP_PORT_OVERRIDE = '5201'/)
  assert.match(content, /SMAIL_HTTP_TLS_MODE_OVERRIDE = 'proxy'/)
  assert.match(content, /LocalPort 5201/)
  assert.match(content, /@\(80, 443\)/)
  assert.match(await read('frontend/.env.development'), /VITE_DEV_BACKEND_URL=http:\/\/127\.0\.0\.1:5201/)
  assert.match(await read('frontend/vite.config.js'), /port: 443/)
  assert.match(await read('frontend/vite.config.js'), /smail-development-http-redirect/)
  assert.match(content, /deploy-runtime\.ps1/)
  assert.ok(content.charCodeAt(0) === 0xfeff, 'Windows PowerShell 5 脚本应有 UTF-8 BOM')
})

test('六个平台默认包含三个插件，构建和发布名称与后端目录完全匹配', async () => {
  const catalog = await read('backend/plugins/catalog/catalog.go')
  for (const target of targets) {
    const plan = buildPlan({ target })
    assert.equal(plan.length, 3)
    assert.deepEqual(plan.map(item => item.id), plugins.map(item => item.id))
    for (const item of plan) {
      assert.equal(path.dirname(item.output), path.join(root, 'out', 'plugins', target))
      assert.ok(catalog.includes(`"${path.basename(item.output)}"`))
    }
    const files = packagePlan(target)
    assert.equal(files.length, 3)
    assert.equal(files.filter(file => path.basename(path.dirname(file.target)) === 'plugins').length, 0)
    assert.ok(files.every(file => path.dirname(file.target) === path.join(root, 'out', 'package', `Smail_${target}`, 'smail')))
  }
  assert.ok(buildPlan({ development: true }).every(item => path.dirname(item.output) === path.join(root, 'out', 'plugins')))
  assert.throws(() => buildPlan({ target: '../invalid' }))
  assert.throws(() => buildPlan({ plugin: 'unknown' }))
  assert.throws(() => buildPlan({ development: true, target: targets.find(target => target !== hostTarget()) }))
  assert.match(await read('deploy/build.ps1'), /build-plugins\.mjs'\) --dev/)
  assert.match(await read('Makefile'), /package: build_server plugin/)
  assert.match(await read('.github/workflows/release.yml'), /node deploy\/build-plugins\.mjs --target=/)
  assert.match(await read('deploy/package.mjs'), /sha256: createHash\('sha256'\)/)
  assert.match(await read('.github/workflows/release.yml'), /node deploy\/package\.mjs/)
  assert.match(await read('.github/workflows/release.yml'), /\.\/cmd\/smail-config/)
})

test('安全工作流必须先构建嵌入资源再执行 Go 测试', async () => {
  for (const file of ['.github/workflows/release.yml', '.github/workflows/security.yml']) {
    const content = (await read(file)).split('  security:')[1]
    assert.ok(content)
    const prepare = content.indexOf('yarn build')
    const goTest = content.indexOf('node test/run.mjs backend')
    assert.ok(prepare >= 0 && goTest > prepare, file)
  }
})
