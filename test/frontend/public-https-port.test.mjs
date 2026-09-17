import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'

const source = readFileSync(new URL('../../frontend/src/views/Setup/SetupNavigation.js', import.meta.url), 'utf8')
const { buildFinalURL } = compileFunction(
  source.replaceAll('export const ', 'const ') + '\nreturn { buildFinalURL }'
)()

test('初始化完成跳转优先使用公网端口，保持路径和页面状态', () => {
  const location = new URL('https://mail.example.com/setup?step=ssl#/finish')
  for (const tlsMode of ['direct', 'proxy']) {
    assert.equal(buildFinalURL({ tlsMode, httpsPort: 8002, publicHttpsPort: 443 }, 'mail.example.com', location),
      'https://mail.example.com/setup?step=ssl#/finish')
    assert.equal(buildFinalURL({ tlsMode, httpsPort: 8002, publicHttpsPort: 9443 }, 'mail.example.com', location),
      'https://mail.example.com:9443/setup?step=ssl#/finish')
  }
})

test('没有公网配置时保留直连和旧反代跳转行为', () => {
  const location = new URL('https://mail.example.com/')
  assert.equal(buildFinalURL({ tlsMode: 'direct', httpsPort: 8443 }, 'mail.example.com', location),
    'https://mail.example.com:8443/')
  assert.equal(buildFinalURL({ tlsMode: 'proxy', httpsPort: 8002 }, 'mail.example.com', location),
    'https://mail.example.com/')
})
