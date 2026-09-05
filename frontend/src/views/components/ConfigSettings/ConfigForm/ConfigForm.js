const field = (path, label, type = 'text', extra = {}) => ({ path: path.split('.'), label, type, ...extra })
const options = entries => entries.map(([value, label]) => ({ value, label }))
const loginOptions = options([
  ['', '使用默认策略'], ['off', '关闭'], ['new_device', '新设备验证'], ['always', '每次登录验证']
])
const port = (path, label, extra = {}) => field(path, label, 'number', { min: 0, max: 65535, integer: true, ...extra })
const group = (id, title, description, fields) => ({ id, title, description, fields })

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
    field('domains.accepted', '接收邮件的域名', 'list', { required: true, hint: '需包含主邮件域名，每个域名单独填写。', placeholder: 'example.com' }),
    field('domains.accessHosts', '额外允许的访问主机', 'list', { hint: '按需填写域名或 IP 地址，不填写则保持默认访问范围。', placeholder: 'mail.example.com' }),
    field('logLevel', '日志级别', 'select', { options: options([['', '默认级别'], ['debug', '调试'], ['info', '信息'], ['warn', '警告'], ['error', '错误']]) })
  ]),
  group('database', '数据库连接', '修改数据库类型、名称或文件请使用右侧“数据库迁移”，支持 SQLite、MySQL、PostgreSQL，避免切换到空数据库。', [
    field('database.type', '数据库类型', 'select', { locked: true, options: options([['sqlite', 'SQLite（本地文件）'], ['mysql', 'MySQL'], ['postgresql', 'PostgreSQL']]) }),
    field('database.localRef', '数据库文件', 'text', { locked: true, show: config => !networkDatabase(config) }),
    field('database.name', '数据库名称', 'text', { locked: true, show: networkDatabase }),
    field('database.host', '数据库主机', 'text', { required: true, show: networkDatabase, placeholder: '127.0.0.1' }),
    port('database.port', '数据库端口', { min: 1, show: networkDatabase }),
    field('database.username', '数据库用户名', 'text', { required: true, show: networkDatabase })
  ]),
  group('http', '网页访问服务', '端口和加密方式变更可能需要重启，并影响当前访问地址。', [
    port('http.port', 'HTTP 端口', { hint: '填写 0 使用后端默认值。' }),
    field('http.tlsMode', '加密连接方式', 'select', { options: options([['', '使用默认方式'], ['direct', '由 Smail 提供 HTTPS'], ['proxy', '由反向代理提供 HTTPS']]) }),
    field('http.httpsEnabled', '启用内置 HTTPS', 'switch', { show: config => config.http?.tlsMode !== 'proxy' }),
    port('http.httpsPort', 'HTTPS 端口', { show: config => config.http?.tlsMode !== 'proxy', hint: '启用内置 HTTPS 时需填写有效端口；关闭时 0 表示不使用。' }),
    field('http.trustedProxyCidrs', '可信代理网段', 'list', { placeholder: '127.0.0.1/32', hint: '仅填写实际代理的 CIDR 网段，不要无条件信任所有来源。' })
  ]),
  group('security', '全局安全选项', '只调整功能开关与站点信息，密码算法和加密密钥由系统维护。', [
    field('security.externalEmailEnabled', '启用外部邮箱功能', 'switch'),
    field('security.webAuthnEnabled', '启用通行密钥', 'switch'),
    field('security.webAuthnRpId', '通行密钥站点域名', 'text', { show: config => config.security?.webAuthnEnabled, placeholder: 'mail.example.com' }),
    field('security.webAuthnRpDisplayName', '通行密钥显示名称', 'text', { show: config => config.security?.webAuthnEnabled, placeholder: 'Smail' }),
    field('security.webAuthnRpOrigins', '通行密钥允许的来源', 'list', { show: config => config.security?.webAuthnEnabled, placeholder: 'https://mail.example.com' })
  ])
]

const smtpGroups = [
  group('relay', 'SMTP 中继连接', '用于发送验证及通知邮件。未使用中继时可保留空值。', [
    field('relay.relayHost', 'SMTP 服务器', 'text', { placeholder: 'smtp.example.com' }),
    port('relay.relayPort', 'SMTP 端口', { hint: '常见端口：STARTTLS 使用 587，隐式 TLS 使用 465。' }),
    field('relay.security', '连接加密', 'select', { options: options([['', '使用默认方式'], ['starttls', 'STARTTLS'], ['implicit_tls', '隐式 TLS'], ['tls', 'TLS（兼容模式）'], ['plain', '不加密（仅限本机）']]) }),
    field('relay.username', '认证用户名', 'text', { placeholder: '需要认证时填写' }),
    field('relay.from', '发件邮箱', 'text', { placeholder: 'noreply@example.com' }),
    field('relay.fromDomain', '发件域名', 'text', { placeholder: 'example.com' })
  ]),
  group('verification', '验证邮件策略', '启用前请确认服务器、端口、发件邮箱和认证信息完整。', [
    field('relay.verificationEnabled', '启用验证邮件发送', 'switch'),
    field('relay.loginPolicy', '中继登录验证策略', 'select', { options: loginOptions })
  ])
]

const emailGroups = [
  group('ports', '邮件服务端口', '各服务端口不能重复，最终可用性由后端统一校验。', [
    port('policy.smtpPort', 'SMTP 收信端口', { min: 1, required: true }),
    port('policy.imapPort', 'IMAP 端口', { min: 1, required: true }),
    port('policy.pop3Port', 'POP3 端口', { min: 1, required: true })
  ]),
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
  if (section !== 'plugins') {
    return ({ base: baseGroups, smtp: smtpGroups, email: emailGroups, ssl: sslGroups }[section] || [])
      .map(item => ({ ...item, fields: item.fields.filter(input => !input.show || input.show(config)) }))
  }
  return Object.entries(config.plugins || {}).map(([id, plugin]) => {
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
  let parent = config
  for (const part of path.slice(0, -1)) {
    if (!parent[part] || typeof parent[part] !== 'object') parent[part] = {}
    parent = parent[part]
  }
  parent[path.at(-1)] = value ?? (input.type === 'number' ? null : '')
  if (key === 'http.tlsMode' && value === 'proxy') {
    config.http.httpsEnabled = false
    config.http.httpsPort = 0
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
  }
  if (section === 'email') {
    const ports = ['smtpPort', 'imapPort', 'pop3Port']
    for (const name of ports) {
      const value = config.policy?.[name]
      if (value > 0 && ports.some(other => other !== name && config.policy?.[other] === value)) errors[`policy.${name}`] = '邮件服务端口不能重复'
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
