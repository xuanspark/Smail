import { lstatSync, readFileSync, readdirSync, realpathSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const sourceRoot = fileURLToPath(new URL('../', import.meta.url))
export const runtimeRoot = path.join(sourceRoot, 'yunx-test')

export function assertDeploymentLayout(root = runtimeRoot) {
  const allowed = new Set(['config', 'data', 'ssl', 'log', 'smail', 'smail-admin', 'smail.exe', 'smail-admin.exe'])
  for (const item of readdirSync(root, { withFileTypes: true })) {
    if (!allowed.has(item.name) || item.isSymbolicLink()) throw new Error('部署根目录包含额外项目或链接')
  }
  const names = ['config.json', 'smtp.json', 'email.json', 'ssl.json', 'plugins.json']
  if (JSON.stringify(readdirSync(path.join(root, 'config')).sort()) !== JSON.stringify(names.sort())) {
    throw new Error('config 只能包含指定的五个配置文件')
  }
  const config = JSON.parse(readFileSync(resolveRuntimeFile(root, 'config/config.json'), 'utf8'))
  for (const name of ['smtp', 'email', 'ssl', 'plugins']) {
    resolveRuntimeFile(root, `config/${name}.json`)
  }
  if (config.setupState === 'ready') {
    for (const name of ['credential.key', 'external-email-encryption.key', 'external-email-fingerprint.key']) {
      resolveRuntimeFile(root, `ssl/${name}`)
    }
    if (config.database?.type === 'sqlite') resolveRuntimeFile(root, `data/${config.database.localRef}`)
  }
  const ssl = JSON.parse(readFileSync(resolveRuntimeFile(root, 'config/ssl.json'), 'utf8')).tls
  for (const reference of [ssl?.certificateRef, ssl?.privateKeyRef]) {
    if (!reference?.startsWith('ssl/')) throw new Error('证书引用尚未迁移到 ssl，拒绝使用旧目录启动')
    resolveRuntimeFile(root, reference)
  }
  return config
}

export function resolveRuntimeFile(root, reference) {
  if (typeof reference !== 'string' || !reference || path.isAbsolute(reference)) {
    throw new Error('运行文件必须使用部署目录内的相对引用')
  }
  const resolvedRoot = realpathSync(root)
  const absolute = path.resolve(root, reference)
  const info = lstatSync(absolute)
  if (!info.isFile() || info.isSymbolicLink()) throw new Error('运行文件必须是普通文件')
  const target = realpathSync(absolute)
  const relative = path.relative(resolvedRoot, target)
  if (!relative || relative.startsWith(`..${path.sep}`) || relative === '..' || path.isAbsolute(relative)) {
    throw new Error('运行文件不能越过部署目录')
  }
  return target
}

// 仅供 Node/Vite 服务端读取，不能把包含凭据的配置对象暴露给浏览器。
export function readDevelopmentRuntime(root = runtimeRoot) {
  const read = name => JSON.parse(readFileSync(resolveRuntimeFile(root, `config/${name}.json`), 'utf8'))
  const config = read('config')
  const ssl = read('ssl')
  const webDomain = config.domains?.web
  if (typeof webDomain !== 'string' || !/^[a-z0-9.-]+(?::\d+)?$/i.test(webDomain)) {
    throw new Error('请先在 yunx-test/config/config.json 配置有效的 Web 域名')
  }
  const certificatePath = resolveRuntimeFile(root, ssl.tls?.certificateRef)
  const privateKeyPath = resolveRuntimeFile(root, ssl.tls?.privateKeyRef)
  return {
    origin: `https://${webDomain}`,
    certificatePath,
    privateKeyPath,
    generation: config.generation
  }
}
