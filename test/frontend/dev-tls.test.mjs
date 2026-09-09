import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { once } from 'node:events'
import { chmod, mkdtemp, mkdir, writeFile } from 'node:fs/promises'
import { createServer } from 'node:https'
import os from 'node:os'
import path from 'node:path'
import { connect } from 'node:tls'
import test from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { readDevelopmentTLS, developmentTLSReload } from '../../frontend/dev-tls.js'
import { assertDeploymentLayout } from '../../deploy/runtime-config.mjs'

function findOpenSSL() {
  const candidates = ['openssl']
  if (process.platform === 'win32') {
    const located = spawnSync('where.exe', ['git'], { encoding: 'utf8', windowsHide: true })
    for (const executable of (located.stdout || '').trim().split(/\r?\n/).filter(Boolean)) {
      candidates.push(path.resolve(path.dirname(executable), '../usr/bin/openssl.exe'))
    }
  }
  const result = candidates.find(command => spawnSync(command, ['version'], { windowsHide: true }).status === 0)
  assert.ok(result, '开发 TLS 回归需要 OpenSSL 或 Windows Git 自带的 OpenSSL')
  return result
}

test('开发 HTTPS 完整换证、旧连接保留、直接路径和上一套恢复', async t => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'smail-dev-tls-'))
  for (const directory of ['config', 'ssl', 'data', 'log']) await mkdir(path.join(root, directory))
  await writeFile(path.join(root, 'config/config.json'), JSON.stringify({
    generation: 1, setupState: 'pending', domains: { web: 'mail.example.test' }
  }))
  for (const section of ['smtp', 'email', 'plugins']) await writeFile(path.join(root, `config/${section}.json`), '{}')
  const openssl = findOpenSSL()
  for (const name of ['first', 'second']) {
    const generated = spawnSync(openssl, [
      'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
      '-keyout', path.join(root, `ssl/${name}.key`), '-out', path.join(root, `ssl/${name}.crt`),
      '-days', '1', '-subj', '/CN=mail.example.test', '-addext', 'subjectAltName=DNS:mail.example.test'
    ], { windowsHide: true, encoding: 'utf8' })
    assert.equal(generated.status, 0, generated.stderr)
    await chmod(path.join(root, `ssl/${name}.key`), 0o600)
  }
  const save = async (current, previous, absolute = false) => {
    const reference = name => absolute ? path.join(root, `ssl/${name}`) : `ssl/${name}`
    await writeFile(path.join(root, 'config/ssl.json'), JSON.stringify({ meta: { generation: 1 }, tls: {
      certificateRef: reference(`${current}.crt`), privateKeyRef: reference(`${current}.key`),
      ...(previous ? { previousCertificateRef: reference(`${previous}.crt`), previousPrivateKeyRef: reference(`${previous}.key`) } : {})
    } }))
  }
  await save('first')
  const initial = readDevelopmentTLS(root)
  const server = createServer(initial.options)
  const warnings = []
  const connections = new Set()
  server.on('secureConnection', socket => {
    connections.add(socket)
    socket.on('close', () => connections.delete(socket))
  })
  developmentTLSReload(initial, root).configureServer({
    httpServer: server, config: { logger: { warn: message => warnings.push(message), error: message => warnings.push(message) } }
  })
  t.after(async () => {
    for (const socket of connections) socket.destroy()
    await new Promise(resolve => server.close(resolve))
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const dial = async () => {
    const socket = connect({ host: '127.0.0.1', port: server.address().port, rejectUnauthorized: false })
    t.after(() => socket.destroy())
    await once(socket, 'secureConnect')
    return socket
  }
  const oldConnection = await dial()
  const serial = oldConnection.getPeerCertificate().serialNumber
  await save('second', 'first', true)
  assert.equal(assertDeploymentLayout(root).generation, 1)
  await delay(1300)
  const newConnection = await dial()
  assert.notEqual(newConnection.getPeerCertificate().serialNumber, serial)
  assert.equal(oldConnection.destroyed, false)
  assert.equal(warnings.length, 0)

  await save('missing', 'first')
  assert.equal(assertDeploymentLayout(root).generation, 1)
  assert.equal(readDevelopmentTLS(root).fingerprint, initial.fingerprint)
  await delay(1300)
  assert.equal((await dial()).getPeerCertificate().serialNumber, serial)
  assert.equal(oldConnection.destroyed, false)

  await save('missing')
  assert.throws(() => readDevelopmentTLS(root))
  assert.throws(() => assertDeploymentLayout(root))
})
