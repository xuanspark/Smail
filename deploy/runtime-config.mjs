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
    for (const [folder, name] of [
      ['credentials', 'credential.key'],
      ['external-email-encryption', 'external-email-encryption.key'],
      ['external-email-fingerprint', 'external-email-fingerprint.key']
    ]) {
      const grouped = `ssl/${folder}/${name}`
      const legacy = `ssl/${name}`
      // 启动前兼容有效旧位置，迁移由新后端完成，检查器不生成或移动密钥。
      const locations = [grouped, legacy, `data/${name}`, `config/${name}`]
        .filter(reference => lstatSync(path.join(root, reference), { throwIfNoEntry: false }))
      if (locations.length !== 1 || ![grouped, legacy].includes(locations[0])) {
        throw new Error('业务密钥缺失或新旧位置冲突，拒绝自动选择')
      }
      const target = resolveRuntimeFile(root, locations[0])
      if (lstatSync(target).size !== 32) throw new Error('业务密钥长度无效')
    }
    if (config.database?.type === 'sqlite') resolveRuntimeFile(root, `data/${config.database.localRef}`)
  }
  const ssl = JSON.parse(readFileSync(resolveRuntimeFile(root, 'config/ssl.json'), 'utf8')).tls
  try {
    resolveDevelopmentCertificate(root, ssl?.certificateRef)
    resolveDevelopmentCertificate(root, ssl?.privateKeyRef, true)
  } catch (error) {
    // 启动预检与热加载使用相同边界；当前材料不可读时允许后端验证上一套。
    try {
      resolveDevelopmentCertificate(root, ssl?.previousCertificateRef)
      resolveDevelopmentCertificate(root, ssl?.previousPrivateKeyRef, true)
    } catch {
      throw error
    }
  }
  return config
}

export function resolveRuntimeFile(root, reference) {
  if (typeof reference !== 'string' || !reference || path.isAbsolute(reference)) {
    throw new Error('运行文件必须使用部署目录内的相对引用')
  }
  const resolvedRoot = realpathSync(root)
  const absolute = path.resolve(root, reference)
  const lexical = path.relative(path.resolve(root), absolute)
  if (!lexical || lexical === '..' || lexical.startsWith(`..${path.sep}`) || path.isAbsolute(lexical)) {
    throw new Error('运行文件不能越过部署目录')
  }
  let current = path.resolve(root)
  for (const component of lexical.split(path.sep)) {
    current = path.join(current, component)
    if (lstatSync(current).isSymbolicLink()) throw new Error('运行文件路径不能包含链接')
  }
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
export function readDevelopmentRuntime(root = runtimeRoot, { previous = false } = {}) {
  const read = name => JSON.parse(readFileSync(resolveRuntimeFile(root, `config/${name}.json`), 'utf8'))
  const config = read('config')
  const ssl = read('ssl')
  if (ssl.meta?.generation !== config.generation) {
    throw new Error('配置事务尚未完成，暂不切换开发证书')
  }
  const webDomain = config.domains?.web
  if (typeof webDomain !== 'string' || !/^[a-z0-9.-]+(?::\d+)?$/i.test(webDomain)) {
    throw new Error('请先在 yunx-test/config/config.json 配置有效的 Web 域名')
  }
  const mailDomain = config.domains?.primary || webDomain
  if (typeof mailDomain !== 'string' || !/^[a-z0-9.-]+(?::\d+)?$/i.test(mailDomain)) {
    throw new Error('请先在 yunx-test/config/config.json 配置有效的主邮件域名')
  }
  const certificatePath = resolveDevelopmentCertificate(root, previous ? ssl.tls?.previousCertificateRef : ssl.tls?.certificateRef)
  const privateKeyPath = resolveDevelopmentCertificate(root, previous ? ssl.tls?.previousPrivateKeyRef : ssl.tls?.privateKeyRef, true)
  return {
    origin: `https://${webDomain}`,
    mailOrigin: `https://${mailDomain}`,
    certificatePath,
    privateKeyPath,
    generation: config.generation
  }
}

// 证书允许显式绝对路径；配置与数据库仍保持部署根目录边界。
function resolveDevelopmentCertificate(root, reference, privateKey = false) {
  if (typeof reference !== 'string' || !reference || reference.includes('\0') || reference.split(/[\\/]/).includes('..')) {
    throw new Error('证书路径无效')
  }
  const target = path.isAbsolute(reference) ? path.resolve(reference) : resolveRuntimeFile(root, reference)
  let current = target
  while (true) {
    if (lstatSync(current).isSymbolicLink()) throw new Error('证书路径不能包含链接')
    const parent = path.dirname(current)
    if (parent === current) break
    current = parent
  }
  const info = lstatSync(target)
  if (!info.isFile() || info.nlink !== 1 || info.size <= 0 || info.size > 1024 * 1024) throw new Error('证书必须是安全的普通文件')
  if (privateKey && process.platform !== 'win32' && (info.mode & 0o077)) throw new Error('私钥权限过于宽松')
  return target
}
