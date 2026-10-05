import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { downloadRecoveryCodes } from '../../frontend/src/utils/recoveryCodeDownload.js'
import { RECOVERY_CODE_DISPLAY_MS } from '../../frontend/src/utils/recoveryCodeDisplay.js'

const securitySettingsSource = readFileSync(new URL('../../frontend/src/views/components/SecuritySettings/SecuritySettings.js', import.meta.url), 'utf8')
const securitySettingsTemplate = readFileSync(new URL('../../frontend/src/views/components/SecuritySettings/SecuritySettings.vue', import.meta.url), 'utf8')

test('security codes use a shared 60-second display timeout', () => {
  assert.equal(RECOVERY_CODE_DISPLAY_MS, 60000)
  assert.match(securitySettingsSource, /setTimeout\(clearRecoveryCodes, RECOVERY_CODE_DISPLAY_MS\)/)
  assert.match(securitySettingsTemplate, /one-time-tip/)
})

function createDocumentFixture() {
  const anchors = []
  const body = {
    appendChild(anchor) {
      anchors.push(anchor)
    },
  }
  return {
    anchors,
    documentRef: {
      body,
      createElement() {
        return {
          style: {},
          click() {
            this.clicked = true
          },
          remove() {
            this.removed = true
          },
        }
      },
    },
  }
}

test('没有安全码时不生成下载文件', () => {
  const fixture = createDocumentFixture()
  let created = 0
  const urlApi = {
    createObjectURL() {
      created += 1
      return 'blob:test'
    },
    revokeObjectURL() {},
  }

  assert.equal(downloadRecoveryCodes([], fixture.documentRef, urlApi), false)
  assert.equal(created, 0)
  assert.equal(fixture.anchors.length, 0)
})

test('安全码只在浏览器本地生成下载，不产生后端请求', async () => {
  const fixture = createDocumentFixture()
  const blobs = []
  const revoked = []
  const urlApi = {
    createObjectURL(blob) {
      blobs.push(blob)
      return 'blob:recovery-codes'
    },
    revokeObjectURL(url) {
      revoked.push(url)
    },
  }

  assert.equal(
    downloadRecoveryCodes(['ABCD-1234', 'EFGH-5678'], fixture.documentRef, urlApi),
    true
  )
  assert.equal(fixture.anchors.length, 1)
  assert.equal(fixture.anchors[0].href, 'blob:recovery-codes')
  assert.equal(fixture.anchors[0].download, 'smail-recovery-codes.txt')
  assert.equal(fixture.anchors[0].clicked, true)
  assert.equal(fixture.anchors[0].removed, true)
  assert.deepEqual(revoked, ['blob:recovery-codes'])
  assert.equal(await blobs[0].text(), 'ABCD-1234\nEFGH-5678\n')
})
