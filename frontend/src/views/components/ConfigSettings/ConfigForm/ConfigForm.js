const field = (path, label, type = 'text', extra = {}) => ({ path: path.split('.'), label, type, ...extra })
const options = entries => entries.map(([value, label]) => ({ value, label }))
const loginOptions = options([
  ['', '使用默认策略'], ['off', '关闭'], ['new_device', '新设备验证'], ['always', '每次登录验证']
])
const port = (path, label, extra = {}) => field(path, label, 'number', { min: 0, max: 65535, integer: true, ...extra })
const group = (id, title, description, fields) => ({ id, title, description, fields })
const smtpPorts = new Map([['starttls', 587], ['implicit_tls', 465], ['tls', 465], ['plain', 25]])

export function readFormConfig(text) {
  try {
    const value = JSON.parse(text)
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
  } catch {
    return {}
  }
}

export function getValue(config, path) {
  return path.reduce((value, key) => value?.[key], config)
}

export function fieldKey(item) {
  return item.path.join('.')
}

// 推荐值只用于用户主动操作，不在读取配置时覆盖已有的自定义端口。
export function getRecommendedPort(config, path) {
  switch (path.join('.')) {
    case 'http.port':
      return config.http?.tlsMode === 'proxy' ? 8080 : 80
    case 'http.httpsPort':
      return 443
    case 'relay.relayPort':
      return smtpPorts.get(config.relay?.security)
    case 'listener.port':
      return 25
    case 'listener.starttlsPort':
      return 587
    case 'listener.tlsPort':
      return 465
    case 'imap.tlsPort':
      return 993
    case 'pop3.port':
      return 110
    case 'pop3.tlsPort':
      return 995
    default:
      return undefined
  }
}

const validPort = value => Number.isInteger(value) && value > 0 && value <= 65535
const localSMTPHost = value => {
  if (typeof value !== 'string') return false
  const host = value.trim().toLowerCase().replace(/\.$/, '')
  if (host === 'localhost') return true
  if (/^127(?:\.(?:0|[1-9]\d{0,2})){3}$/.test(host)) {
    return host.split('.').every(part => Number(part) <= 255)
  }
  try {
    return new URL(`http://[${host}]`).hostname === '[::1]'
  } catch {
    return false
  }
}

const networkDatabase = config => config.database?.type !== 'sqlite'
const uploadedCertificate = config => ['managed_upload', 'custom'].includes(config.tls?.mode)
const normalizedDomain = value => value.toLowerCase().replace(/\.$/, '')
const validDomain = value => {
  if (typeof value !== 'string' || value !== value.trim()) return false
  const domain = normalizedDomain(value)
  return domain.length > 0 && domain.length <= 253 && !/^\d+(\.\d+){3}$/.test(domain) &&
    domain.split('.').every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))
}

const baseGroups = [
  group('site', '站点与域名', '填写域名即可，无需添加协议前缀或路径。', [
    field('domains.primary', '主邮件域名', 'text', { required: true, placeholder: 'example.com' }),
    field('domains.web', '网页访问域名', 'text', { required: true, placeholder: 'mail.example.com' }),
    field('domains.accepted', '接收邮件的域名', 'list', { required: true, wide: false, hint: '需包含主邮件域名，每个域名单独填写。', placeholder: 'example.com' }),
    field('logLevel', '日志级别', 'select', { options: options([['', '默认级别'], ['debug', '调试'], ['info', '信息'], ['warn', '警告'], ['error', '错误']]) })
  ]),
  group('database', '数据库连接', '修改数据库类型、名称或文件请使用右侧“数据库迁移”，支持 SQLite、MySQL、PostgreSQL，避免切换到空数据库。', [
    field('database.name', '数据库名称', 'text', { locked: true, show: networkDatabase }),
    field('database.host', '数据库主机', 'text', { required: true, show: networkDatabase, placeholder: '127.0.0.1' }),
    port('database.port', '数据库端口', { min: 1, show: networkDatabase }),
    field('database.username', '数据库用户名', 'text', { required: true, show: networkDatabase })
  ]),
  group('http', '网页访问服务', '端口和加密方式变更可能需要重启，并影响当前访问地址。', [
    field('http.tlsMode', '加密连接方式', 'select', {
      options: options([['', '沿用当前设置'], ['direct', '由 Smail 提供 HTTPS'], ['proxy', '由反向代理提供 HTTPS']]),
      hint: '选择内置 HTTPS 自动启用并补全端口；选择反向代理后，Smail 不自动申请或续期 SSL 证书，由反代服务器负责；邮件协议仍需有效的本地证书。'
    }),
    port('http.port', 'HTTP 监听端口', {
      hint: '保留已有自定义端口；未设置时自动补全。反代请转发到此端口，外部 HTTPS 端口在反代服务器配置。'
    }),
    field('http.httpsEnabled', '启用内置 HTTPS', 'switch', {
      show: config => config.http?.tlsMode !== 'proxy',
      hint: '开启时自动补全 HTTPS 端口，关闭时清空端口；正式站点须由内置服务或反代提供 HTTPS。'
    }),
    port('http.httpsPort', 'HTTPS 监听端口', {
      min: 1,
      show: config => config.http?.tlsMode !== 'proxy' && !!config.http?.httpsEnabled,
      hint: '默认使用 443；需要避开端口占用时可手动修改。'
    })
  ]),
  group('security', '通行密钥站点信息', '外部邮箱与通行密钥服务默认可用，由用户在安全设置中自行选择。留空时使用当前站点的安全默认值；密码算法和加密密钥由系统维护。', [
    field('security.webAuthnRpId', '通行密钥站点域名', 'text', {
      placeholder: '默认使用网页访问域名',
      hint: '已绑定通行密钥后不要随意更换域名，否则原密钥可能无法使用。'
    }),
    field('security.webAuthnRpDisplayName', '通行密钥显示名称', 'text', { placeholder: 'Smail' }),
    field('security.webAuthnRpOrigins', '通行密钥允许的来源', 'list', {
      placeholder: 'https://mail.example.com',
      hint: '每项填写不含路径的完整 HTTPS 来源；反代使用非标准外部端口时，请显式填写端口。'
    })
  ])
]

const smtpGroups = [
  group('listener', 'SMTP 服务监听', '本地邮件服务默认启动，不受中继配置或用户协议开关控制。', [
    port('listener.port', 'SMTP 收信端口', { min: 1, hint: '默认 25，用于接收其他邮件服务器投递，支持 STARTTLS；不是发信中继端口。' }),
    port('listener.starttlsPort', 'SMTP 提交端口（STARTTLS）', { min: 1, hint: '默认 587，客户端升级 TLS 后进行认证。' }),
    port('listener.tlsPort', 'SMTP 隐式 TLS 端口', { min: 1, hint: '默认 465，客户端建立连接时直接使用 TLS。' })
  ]),
  group('relay', 'SMTP 中继连接（可选）', '仅用于验证及通知邮件外发，不负责启动本地邮件服务；未使用时可保留空值。', [
    field('relay.relayHost', 'SMTP 服务器', 'text', { placeholder: 'smtp.example.com' }),
    field('relay.security', '连接加密', 'select', {
      options: options([
        ['', '尚未配置'],
        ['starttls', 'STARTTLS（推荐端口 587）'],
        ['implicit_tls', '隐式 TLS（推荐端口 465）'],
        ['tls', 'TLS 兼容模式（推荐端口 465）'],
        ['plain', '不加密（仅限本机，端口 25）']
      ]),
      hint: '切换加密方式会自动匹配推荐端口，之后仍可手动填写服务商指定的端口。'
    }),
    port('relay.relayPort', 'SMTP 发信中继端口', { hint: '外部 SMTP 服务器端口，与本机监听无关；选择加密方式后自动填写。' }),
    field('relay.username', '认证用户名', 'text', { placeholder: '需要认证时填写' }),
    field('relay.from', '发件邮箱', 'text', { placeholder: 'noreply@example.com' }),
    field('relay.fromDomain', '发件域名', 'text', { placeholder: 'example.com' })
  ]),
  group('verification', '验证邮件策略', '启用前请确认服务器、端口、发件邮箱和认证信息完整。', [
    field('relay.verificationEnabled', '启用验证邮件发送', 'switch'),
    field('relay.loginPolicy', '中继登录验证策略', 'select', { options: loginOptions })
  ])
]

const imapGroups = [
  group('imap', 'IMAP 服务监听', '', [
    port('imap.tlsPort', 'IMAP 隐式 TLS 端口', { min: 1, required: true, hint: '默认 993。当前仅支持隐式 TLS，不提供明文或 STARTTLS 监听。' })
  ])
]

const pop3Groups = [
  group('pop3', 'POP3 服务监听', '', [
    port('pop3.port', 'POP3 端口（STLS）', { min: 1, required: true, hint: '默认 110，客户端使用 STLS 升级加密后认证。' }),
    port('pop3.tlsPort', 'POP3 隐式 TLS 端口', { min: 1, required: true, hint: '默认 995，客户端建立连接时直接使用 TLS。' })
  ])
]

const emailGroups = [
  group('policy', '邮件与账号策略', '设置邮件大小、垃圾邮件过滤和账号恢复方式。', [
    field('policy.maxMessageBytes', '最大邮件大小（字节）', 'number', { min: 0, max: Number.MAX_SAFE_INTEGER, integer: true, hint: '0 使用后端默认限制；1 MiB = 1048576 字节。' }),
    field('policy.spamFilterLevel', '垃圾邮件过滤级别', 'select', { fallback: 0, options: options([[0, '不过滤'], [1, 'SPF 与 DKIM 均失败时过滤'], [2, 'SPF 失败时过滤'], [3, 'DKIM 失败时过滤']]) }),
    field('policy.loginVerificationPolicy', '账号登录验证策略', 'select', { options: loginOptions }),
    field('policy.externalRecoveryEnabled', '允许外部邮箱恢复账号', 'switch')
  ])
]

const sslGroups = [
  group('certificate', '证书与存储', '这里只填写已导入受管存储的相对引用，不会上传文件或绕过证书校验。', [
    field('tls.mode', '证书来源', 'select', { options: options([['managed_default', '系统管理证书'], ['managed_upload', '已导入的自定义证书'], ['default', '默认目录（兼容模式）'], ['custom', '自定义目录（兼容模式）']]) }),
    field('tls.defaultDirectory', '默认证书目录', 'text', { required: true, placeholder: 'config/ssl' }),
    field('tls.customDirectory', '自定义证书目录', 'text', { required: true, show: uploadedCertificate, placeholder: 'config/ssl/example.com' }),
    field('tls.certificateRef', '证书文件引用', 'text', { placeholder: 'config/ssl/example.com/fullchain.pem', hint: '必须位于当前证书目录内，与私钥成对填写。' }),
    field('tls.privateKeyRef', '私钥文件引用', 'text', { placeholder: 'config/ssl/example.com/privkey.pem', hint: '填写文件引用，不是私钥内容。' })
  ])
]

const pluginDefinitions = {
  spam_block: { label: '垃圾邮件拦截', fields: [
    field('url', '检测服务地址', 'text', { placeholder: 'https://filter.example.com' }),
    field('timeout', '请求超时（毫秒）', 'number', { min: 0, max: 2147483647, integer: true, step: 100, hint: '1000 毫秒 = 1 秒；0 使用插件默认值 3000 毫秒。' }),
    field('threshold', '拦截阈值（百分比）', 'number', { min: 0, max: Number.MAX_SAFE_INTEGER, step: 0.1, hint: '例如 20 表示 20%；0 使用插件默认值 20%。' })
  ] },
  telegram_push: { label: 'Telegram 推送', fields: [
    field('proxy', '代理地址', 'text', { hint: '可选，不使用代理时留空。' }),
    field('timeout', '请求超时（秒）', 'number', { min: 0, max: 120, integer: true, hint: '0 使用插件默认值，最大 120 秒。' }),
    field('webDomain', '网页域名', 'text'),
    field('debug', '启用调试日志', 'switch'),
    field('httpsEnabled', '链接使用 HTTPS', 'switch')
  ] },
  wechat_push: { label: '微信推送', fields: [
    field('appId', '应用编号（App ID）'),
    field('templateId', '消息模板编号'),
    field('userId', '接收用户编号'),
    field('webDomain', '网页域名')
  ] }
}

export function getFieldGroups(section, config) {
  const pluginId = Object.hasOwn(pluginDefinitions, section) ? section : ''
  if (section !== 'plugins' && !pluginId) {
    return ({ base: baseGroups, smtp: smtpGroups, imap: imapGroups, pop3: pop3Groups, email: emailGroups, ssl: sslGroups }[section] || [])
      .map(item => ({
        ...item,
        fields: item.fields
          .filter(input => !input.show || input.show(config))
          .map(input => ({ ...input, recommendedPort: getRecommendedPort(config, input.path) }))
      }))
      .filter(item => item.fields.length > 0)
  }
  return Object.entries(config.plugins || {}).filter(([id]) => !pluginId || id === pluginId).map(([id, plugin]) => {
    const definition = pluginDefinitions[id]
    const prefix = ['plugins', id]
    const input = (name, label, type, extra = {}) => ({ path: [...prefix, name], label, type, ...extra })
    const settingsEditable = plugin.settings == null || (typeof plugin.settings === 'object' && !Array.isArray(plugin.settings))
    return {
      id,
      title: definition?.label || `插件：${id}`,
      description: `插件标识：${id}。程序与权限由安装目录维护；凭据请在专用插件设置中管理。`,
      fields: [
        input('enabled', '启用插件', 'switch', { locked: !definition }),
        input('executable', '运行程序', 'text', { locked: true }),
        { path: [...prefix, 'permissions', 'network'], label: '允许访问的网络目标', type: 'network', hint: '仅允许 HTTPS 或 WSS；填写域名和端口，不接受 IP 地址。', locked: !definition },
        ...(definition && settingsEditable ? definition.fields.map(item => ({ ...item, path: [...prefix, 'settings', ...item.path] })) : [])
      ],
      note: !settingsEditable ? '当前插件使用非标准设置格式，已保留原值；请在专用插件页面调整。' : '',
      secretConfigured: !!plugin.secretStatus?.configured
    }
  })
}

// 只更新明确可编辑的字段，完整保留代次、内部引用以及未来版本的未展示字段。
export function updateFormConfig(text, section, path, value) {
  const config = readFormConfig(text)
  if (!Array.isArray(path) || path.some(key => ['__proto__', 'prototype', 'constructor'].includes(key))) return text
  const key = path.join('.')
  const input = getFieldGroups(section, config).flatMap(item => item.fields).find(item => fieldKey(item) === key)
  if (!input || input.locked) return text
  const previousValue = getValue(config, path)
  let parent = config
  for (const part of path.slice(0, -1)) {
    if (!parent[part] || typeof parent[part] !== 'object') parent[part] = {}
    parent = parent[part]
  }
  parent[path.at(-1)] = value ?? (input.type === 'number' ? null : '')
  if (previousValue !== value) {
    if (key === 'relay.security') {
      const recommended = getRecommendedPort(config, ['relay', 'relayPort'])
      if (recommended !== undefined) config.relay.relayPort = recommended
    }
    if (key === 'http.tlsMode' && ['direct', 'proxy'].includes(value)) {
      if (!validPort(config.http.port)) {
        config.http.port = getRecommendedPort(config, ['http', 'port'])
      }
      config.http.httpsEnabled = value === 'direct'
      config.http.httpsPort = value === 'proxy' ? 0 : (
        validPort(config.http.httpsPort) ? config.http.httpsPort : 443
      )
    }
    if (key === 'http.httpsEnabled') {
      config.http.httpsPort = value ? (validPort(config.http.httpsPort) ? config.http.httpsPort : 443) : 0
    }
  }
  if (key === 'tls.mode' && ['managed_default', 'default'].includes(value)) config.tls.customDirectory = ''
  return JSON.stringify(config, null, 2)
}

export function validateFormConfig(section, config) {
  const errors = {}
  for (const item of getFieldGroups(section, config).flatMap(item => item.fields)) {
    if (item.locked) continue
    const value = getValue(config, item.path)
    const key = fieldKey(item)
    if (item.required && (value == null || (typeof value === 'string' && !value.trim()) || (Array.isArray(value) && !value.length))) errors[key] = `请填写${item.label}`
    if (item.type === 'number' && value !== undefined && (typeof value !== 'number' || !Number.isFinite(value) || (item.integer && !Number.isSafeInteger(value)) || value < item.min || value > item.max)) {
      errors[key] = `请填写 ${item.min}～${item.max} 范围内的${item.integer ? '整数' : '数值'}`
    }
    if (item.type === 'list' && value != null && (!Array.isArray(value) || value.some(entry => typeof entry !== 'string' || !entry.trim()) || new Set(value).size !== value.length)) {
      errors[key] = '每项都需填写，且不能重复；不需要的项目请移除'
    }
    if (item.type === 'network' && value != null) {
      const valid = Array.isArray(value) && value.every(target => target && ['https', 'wss'].includes(target.scheme) && validDomain(target.host) && Number.isInteger(target.port) && target.port > 0 && target.port <= 65535)
      if (!valid || new Set(value.map(target => `${target.scheme}://${normalizedDomain(target.host)}:${target.port}`)).size !== value.length) errors[key] = '请填写不重复的有效协议、域名和端口'
    }
  }
  if (section === 'base') {
    const domains = config.domains || {}
    for (const name of ['primary', 'web']) {
      if (!validDomain(domains[name])) errors[`domains.${name}`] = '请填写有效域名，不包含协议、路径或空格'
    }
    if (Array.isArray(domains.accepted) && domains.accepted.every(validDomain)) {
      const accepted = domains.accepted.map(normalizedDomain)
      if (new Set(accepted).size !== accepted.length) errors['domains.accepted'] = '接收域名不能重复，大小写和末尾点视为同一域名'
      if (validDomain(domains.primary) && !accepted.includes(normalizedDomain(domains.primary))) errors['domains.accepted'] = '接收域名列表必须包含主邮件域名'
    } else {
      errors['domains.accepted'] = '请逐项填写有效的接收邮件域名'
    }
    if (config.http?.httpsEnabled && !config.http.httpsPort) errors['http.httpsPort'] = '启用内置 HTTPS 时请填写 1～65535 的端口'
    if (config.http?.httpsEnabled && (config.http.port || 80) === config.http.httpsPort) {
      errors['http.httpsPort'] = 'HTTP 与 HTTPS 监听端口不能相同'
    }
  }
  if (section === 'smtp') {
    const relay = config.relay || {}
    if (relay.verificationEnabled || (relay.relayHost && relay.security)) {
      if (!['starttls', 'implicit_tls', 'tls', 'plain'].includes(relay.security)) {
        errors['relay.security'] = '请选择明确的连接加密方式，系统会自动匹配端口'
      }
      if (!validPort(relay.relayPort)) errors['relay.relayPort'] = '请填写 1～65535 的 SMTP 端口'
    }
    if (relay.security === 'plain' && !localSMTPHost(relay.relayHost)) {
      errors['relay.relayHost'] = '不加密连接仅限本机地址；远程服务器请选择 STARTTLS 或隐式 TLS'
    }
  }
  if (['smtp', 'imap', 'pop3'].includes(section)) {
    const paths = section === 'smtp'
      ? ['listener.port', 'listener.starttlsPort', 'listener.tlsPort']
      : ['imap.tlsPort', 'pop3.port', 'pop3.tlsPort']
    for (const path of paths) {
      const value = getValue(config, path.split('.'))
      if (value > 0 && paths.some(other => other !== path && getValue(config, other.split('.')) === value)) errors[path] = '邮件服务监听端口不能重复'
    }
  }
  if (section === 'ssl') {
    const tls = config.tls || {}
    const directory = uploadedCertificate(config) ? tls.customDirectory : tls.defaultDirectory
    const controlled = value => typeof value === 'string' && !!value && !/^(\/|[a-z]:)/i.test(value) && !/[\\:]/.test(value) && value.split('/').every(part => part && part !== '.' && part !== '..')
    for (const name of ['defaultDirectory', ...(uploadedCertificate(config) ? ['customDirectory'] : [])]) {
      if (!controlled(tls[name])) errors[`tls.${name}`] = '请填写受管存储中的相对目录，不允许绝对路径或上级目录'
    }
    for (const name of ['certificateRef', 'privateKeyRef']) {
      if (tls[name] && (!controlled(tls[name]) || !tls[name].startsWith(`${directory}/`))) errors[`tls.${name}`] = '文件引用必须位于当前证书目录内'
    }
    if (!!tls.certificateRef !== !!tls.privateKeyRef) errors['tls.privateKeyRef'] = '证书与私钥引用必须成对填写'
  }
  return errors
}
