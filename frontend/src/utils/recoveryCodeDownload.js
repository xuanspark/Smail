export function buildRecoveryCodesFilename(username) {
  const normalizedUsername = String(username || '')
    .trim()
    .replace(/[<>:"/\\|?*\u0000-\u001F]/g, '-')
    .replace(/^[. -]+/g, '')
    .replace(/-{2,}/g, '-')
    .replace(/[. ]+$/g, '')

  return `smail-${normalizedUsername || 'recovery-codes'}.txt`
}

export function downloadRecoveryCodes(codes, username, documentRef = document, urlApi = URL) {
  if (!Array.isArray(codes) || codes.length === 0) return false

  const blob = new Blob([`${codes.join('\n')}\n`], {
    type: 'text/plain;charset=utf-8',
  })
  const objectUrl = urlApi.createObjectURL(blob)
  const anchor = documentRef.createElement('a')

  anchor.href = objectUrl
  anchor.download = buildRecoveryCodesFilename(username)
  anchor.style.display = 'none'
  documentRef.body.appendChild(anchor)

  try {
    anchor.click()
  } finally {
    anchor.remove()
    urlApi.revokeObjectURL(objectUrl)
  }

  return true
}
