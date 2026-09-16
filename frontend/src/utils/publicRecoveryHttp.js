const publicRecoveryAPIBase = '/rest/api/recovery'

export async function postPublicRecovery(endpoint, payload) {
  const response = await fetch(`${publicRecoveryAPIBase}/${endpoint}`, {
    method: 'POST',
    credentials: 'omit',
    cache: 'no-store',
    referrerPolicy: 'no-referrer',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload)
  })

  let result = null
  try {
    result = await response.json()
  } catch {
    result = null
  }

  return {
    ok: response.ok,
    data: result
  }
}
