import { createHash, createPrivateKey, createPublicKey, X509Certificate } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { createSecureContext } from 'node:tls'
import { readDevelopmentRuntime } from '../deploy/runtime-config.mjs'

// 开发 HTTPS 也读取同一部署配置；整套材料校验后再替换，不关闭现有连接。
export function readDevelopmentTLS(root) {
  try {
    return readTLSMaterial(readDevelopmentRuntime(root))
  } catch (error) {
    try {
      return readTLSMaterial(readDevelopmentRuntime(root, { previous: true }))
    } catch {
      throw error
    }
  }
}

function readTLSMaterial(runtime) {
  const cert = readFileSync(runtime.certificatePath)
  const key = readFileSync(runtime.privateKeyPath)
  const certificate = new X509Certificate(cert)
  const now = Date.now()
  if (now < Date.parse(certificate.validFrom) || now >= Date.parse(certificate.validTo)) {
    throw new Error('开发证书尚未生效或已经过期')
  }
  const publicKey = createPublicKey(createPrivateKey(key))
  if (!certificate.publicKey.equals(publicKey) || !certificate.checkHost(new URL(runtime.origin).hostname)) {
    throw new Error('开发证书与私钥或 Web 域名不匹配')
  }
  createSecureContext({ cert, key, minVersion: 'TLSv1.2' })
  const fingerprint = createHash('sha256').update(cert).update(key).digest('hex')
  return { runtime, options: { cert, key, minVersion: 'TLSv1.2' }, fingerprint, expiresAt: Date.parse(certificate.validTo) }
}

export function developmentTLSReload(initial, root) {
  return {
    name: 'smail-development-tls-reload',
    apply: 'serve',
    configureServer(server) {
      let active = initial
      let warned = false
      let expired = false
      const timer = setInterval(() => {
        try {
          const next = readDevelopmentTLS(root)
          if (expired || next.fingerprint !== active.fingerprint) {
            server.httpServer.setSecureContext(next.options)
            active = next
            expired = false
          }
          warned = false
        } catch {
          if (!expired && Date.now() >= active.expiresAt) {
            // 不再给新握手提供过期证书；已经建立的连接不关闭。
            try {
              server.httpServer.setSecureContext({ minVersion: 'TLSv1.2' })
              expired = true
            } catch {
              server.config.logger.error('无法停用过期开发证书，请重启前端服务')
            }
          }
          if (!warned) server.config.logger.warn('证书热加载失败，暂时保留上一套有效证书，请检查 yunx-test/ssl')
          warned = true
        }
      }, 1000)
      timer.unref()
      server.httpServer.once('close', () => clearInterval(timer))
    }
  }
}
