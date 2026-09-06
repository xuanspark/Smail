import { access, copyFile, mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { buildPlan, root, targets } from './build-plugins.mjs'

export function packagePlan(target) {
  const pluginPlan = buildPlan({ target })
  const [goos, goarch] = target.split('_')
  const extension = goos === 'windows' ? '.exe' : ''
  const suffix = `${goos === 'darwin' ? 'mac' : goos}_${goarch}${extension}`
  const directory = path.join(root, 'out', 'package', target)
  return [
    ...['smail', 'smail-admin'].map(name => ({
      source: path.join(root, 'out', 'backend', `${name}_${suffix}`),
      target: path.join(directory, `${name}${extension}`)
    })),
    ...pluginPlan.map(item => ({ source: item.output, target: path.join(directory, 'plugins', path.basename(item.output)) })),
    { source: path.join(root, 'README.md'), target: path.join(directory, 'README.md') }
  ]
}

export async function packageRelease(selectedTargets = targets) {
  const plan = selectedTargets.flatMap(packagePlan)
  // 所有输入齐备后才写发布目录，不把旧的或其他平台插件混入发布包。
  for (const item of plan) await access(item.source)
  for (const item of plan) {
    await mkdir(path.dirname(item.target), { recursive: true })
    await copyFile(item.source, item.target)
  }
  for (const target of selectedTargets) {
    await writeFile(path.join(root, 'out', 'package', target, 'release.json'), JSON.stringify({
      version: process.env.SMAIL_VERSION || 'dev', gitHash: process.env.GIT_HASH || '',
      protocolVersion: 1, target, plugins: buildPlan({ target }).map(item => item.id), createdAt: new Date().toISOString()
    }, null, 2) + '\n')
  }
  console.log(`发布包已生成：${selectedTargets.map(target => `out/package/${target}`).join('、')}`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  packageRelease(process.argv.length > 2 ? process.argv.slice(2) : targets).catch(error => {
    console.error(`发布打包失败：${error.message}`)
    process.exitCode = 1
  })
}
