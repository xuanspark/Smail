const DEFAULT_HTTPS_PORT = 443
const REDIRECT_STATUS_MIN = 300
const REDIRECT_STATUS_MAX = 399

const currentLocation = () => {
  if (typeof window === 'undefined' || !window.location) {
    return null
  }
  return window.location
}

const normalizeDomain = (value) => {
  const domain = String(value || '').trim()
  if (!domain) {
    return ''
  }
  return domain.replace(/^[a-z][a-z\d+.-]*:\/\//i, '').replace(/\/.*$/, '')
}

const isNonDefaultPort = (protocol, port) => {
  if (!port) {
    return false
  }
  return protocol === 'https:' ? port !== String(DEFAULT_HTTPS_PORT) : port !== '80'
}

export const isProxyTLSMode = (sslData) => sslData?.tlsMode === 'proxy'

export const buildFinalURL = (sslData, webDomain, location = currentLocation()) => {
  if (!location) {
    return ''
  }

  const configuredDomain = normalizeDomain(webDomain)
  if (!configuredDomain) {
    return location.href
  }

  let target
  try {
    target = new URL(`https://${configuredDomain}`)
  } catch (error) {
    return location.href
  }

  const proxyMode = isProxyTLSMode(sslData)
  const configuredPort = target.port
  const targetPort = Number(sslData?.httpsPort)
  const publicPort = Number(sslData?.publicHttpsPort)
  if (Number.isInteger(publicPort) && publicPort > 0 && publicPort <= 65535) {
    target.port = publicPort === DEFAULT_HTTPS_PORT ? '' : String(publicPort)
  } else if (!proxyMode) {
    target.port = targetPort > 0 && targetPort !== DEFAULT_HTTPS_PORT ? String(targetPort) : ''
  } else if (!isNonDefaultPort(target.protocol, configuredPort)) {
    target.port = ''
  }

  target.pathname = location.pathname
  target.search = location.search
  target.hash = location.hash
  return target.toString()
}

const createURL = (value, path) => {
  try {
    const url = new URL(path, value)
    url.search = ''
    url.hash = ''
    return url.toString()
  } catch (error) {
    return ''
  }
}

export const getServiceProbeURLs = (sslData, webDomain, location = currentLocation()) => {
  if (!location) {
    return []
  }

  const urls = []
  const add = (value, path) => {
    const url = createURL(value, path)
    if (url && !urls.includes(url)) {
      urls.push(url)
    }
  }

  add(location.href, '/api/ping')
  add(location.href, '/api/v1/ping')

  const finalURL = buildFinalURL(sslData, webDomain, location)
  if (finalURL) {
    add(finalURL, '/api/ping')
    add(finalURL, '/api/v1/ping')
  }
  return urls
}

export const isServiceProbeSuccess = (response) => {
  if (!response) {
    return false
  }
  return response.status >= 200 && response.status < 300
}

export const isServiceRedirect = (response, location = currentLocation()) => {
  if (!response || !location) {
    return false
  }
  if (response.status < REDIRECT_STATUS_MIN || response.status > REDIRECT_STATUS_MAX) {
    return false
  }

  try {
    return new URL(response.url).origin === location.origin
  } catch (error) {
    return false
  }
}

export const probeService = async (sslData, webDomain, location = currentLocation()) => {
  if (!location || typeof fetch !== 'function') {
    return false
  }

  const proxyMode = isProxyTLSMode(sslData)
  const urls = getServiceProbeURLs(sslData, webDomain, location)
  for (const url of urls) {
    try {
      const response = await fetch(url, {
        method: 'GET',
        credentials: 'include',
        cache: 'no-store',
        redirect: 'manual',
        headers: {
          Accept: 'application/json'
        }
      })
      if (isServiceProbeSuccess(response)) {
        return true
      }
      if (!proxyMode && isServiceRedirect(response, location)) {
        return true
      }
    } catch (error) {
      // 服务切换期间失败属于预期状态。
    }
  }
  return false
}
