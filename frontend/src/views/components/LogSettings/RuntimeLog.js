const levels = {
  trace: { label: '跟踪', type: 'info' },
  debug: { label: '调试', type: 'info' },
  info: { label: '信息', type: 'primary' },
  warning: { label: '警告', type: 'warning' },
  error: { label: '错误', type: 'danger' },
  fatal: { label: '致命', type: 'danger' },
  panic: { label: '异常', type: 'danger' },
  unknown: { label: '未标注', type: 'info' }
}

const localDate = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric', month: '2-digit', day: '2-digit'
})
const localTime = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23'
})

export function timeParts(value) {
  const date = new Date(value)
  if (!value || Number.isNaN(date.getTime())) return { date: '—', time: '—' }
  return { date: localDate.format(date), time: localTime.format(date) }
}

export function cleanRuntimeMessage(text) {
  if (!text) return ''
  const boundary = text.lastIndexOf(' @event ')
  if (boundary >= 0) {
    return text.slice(0, boundary).trim()
  }
  return text
}

export function friendlyOutcomeText(text) {
  if (!text) return '（无详细说明）'
  const trimmed = text.trim()
  const dictionary = {
    'IMAP authentication failed': 'IMAP 客户端登录认证失败',
    'IMAP authentication succeeded': 'IMAP 客户端登录认证成功',
    'POP3 authentication failed': 'POP3 客户端登录认证失败',
    'POP3 authentication succeeded': 'POP3 客户端登录认证成功',
    'SMTP authentication failed': 'SMTP 客户端登录认证失败',
    'SMTP authentication succeeded': 'SMTP 客户端登录认证成功',
    'SMTP MAIL accepted': 'SMTP 接收发件人地址',
    'SMTP RCPT accepted': 'SMTP 接收收件人地址',
    'Plugin Stop': '停止插件执行',
    'Server Start Success': '服务启动成功',
    '开始绑定监听端口': '开始绑定并监听网络端口'
  }
  if (dictionary[trimmed]) return dictionary[trimmed]

  if (/^SMTP STARTTLS on 25 Send Error\.\s*(.*)$/i.test(trimmed)) {
    const detail = trimmed.replace(/^SMTP STARTTLS on 25 Send Error\.\s*/i, '')
    return `SMTP 25 端口 STARTTLS 投递失败：${detail}`
  }
  if (/^SMTPS on 587 Send Error\.\s*(.*)$/i.test(trimmed)) {
    const detail = trimmed.replace(/^SMTPS on 587 Send Error\.\s*/i, '')
    return `SMTP 587 端口投递失败：${detail}`
  }
  if (/^SMTPS on 465 Send Error\.\s*(.*)$/i.test(trimmed)) {
    const detail = trimmed.replace(/^SMTPS on 465 Send Error\.\s*/i, '')
    return `SMTP 465 端口投递失败：${detail}`
  }
  return trimmed
}

export function friendlyActionText(action) {
  if (!action) return '历史运行事件'
  const known = {
    '运行事件 · imap_server.(*serverSession).Login': 'IMAP 客户端登录',
    '运行事件 · imap_server.(*serverSession).Close': 'IMAP 断开连接',
    '运行事件 · imap_server.(*serverSession).Select': 'IMAP 选择邮箱',
    '运行事件 · imap_server.(*serverSession).Fetch': 'IMAP 获取邮件',
    '运行事件 · imap_server.(*serverSession).Append': 'IMAP 追加邮件',
    '运行事件 · imap_server.(*serverSession).Delete': 'IMAP 删除邮箱',
    '运行事件 · pop3_server.action.Pass': 'POP3 客户端登录',
    '运行事件 · pop3_server.action.Retr': 'POP3 获取邮件',
    '运行事件 · smtp_server.(*Session).AuthPlain': 'SMTP 客户端认证',
    '运行事件 · send.doSend': 'SMTP 邮件投递',
    '运行事件 · send.doSend.func2': 'SMTP 邮件投递',
    '运行事件 · send.Send': '发送邮件'
  }
  if (known[action]) return known[action]

  const m = action.match(/^运行事件\s*·\s*([a-zA-Z0-9_]+)\.(?:\(\*?[a-zA-Z0-9_]+\)\.)?([a-zA-Z0-9_]+)(?:\.func\d+)?$/)
  if (m) {
    const pkg = m[1]
    const fn = m[2]
    const pkgLabel = {
      imap_server: 'IMAP 服务',
      pop3_server: 'POP3 服务',
      smtp_server: 'SMTP 服务',
      send: '邮件投递',
      controllers: '接口处理',
      services: '系统服务',
      res_init: '系统初始化',
      hooks: '插件钩子',
      adminreset: '超级管理'
    }[pkg] || pkg
    return `${pkgLabel} · ${fn}`
  }
  return action
}

// 只识别已知的日志头；未知格式原样展示，不丢弃记录或猜测级别。
export function parseRuntimeLine(line, sequence) {
  const raw = String(line ?? '')
  let message = raw
  let createdAt = ''
  let level = 'unknown'
  let source = ''
  let requestId = ''
  const timestamp = message.match(/^\[(\d{4}-\d{2}-\d{2}T[^\]]+)\]\s*/)
  if (timestamp && !Number.isNaN(Date.parse(timestamp[1]))) {
    createdAt = timestamp[1]
    message = message.slice(timestamp[0].length)
    const header = message.match(/^\[(trace|debug|info|warn|warning|error|fatal|panic)\]\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]/i)
    if (header) {
      level = header[1].toLowerCase().replace(/^warn$/, 'warning')
      message = message.slice(header[0].length)
      const location = message.match(/^(?:\[([^\]\r\n]*)\])?\[([^\]\r\n]+\.go:\d+)\]/)
      if (location) {
        requestId = location[1] || ''
        source = location[2]
        message = message.slice(location[0].length)
      }
    }
  }
  const separator = /^\*{3,}$/.test(message.trim())
  const legacyStartup = level === 'info'
    && /(?:^|\/)internal\/app\/app\.go:\d+$/.test(source.replace(/\\/g, '/'))
    && /^\*{3}\s+Server Start Success\s*(?:\\r)?(?:\\n)?\s*$/.test(message)
  // 历史启动信息只简化摘要，详情仍保留完整消息和原始记录。
  const summary = legacyStartup ? '服务启动' : separator ? '启动分隔线' : message.trim() || '（空消息）'
  const sourceShort = source.replace(/^git\.pcux\.cn\/xuans\/Smail\//, '')
  let event = {}
  let content = summary
  const boundary = message.lastIndexOf(' @event ')
  if (boundary >= 0) {
    try {
      const parsed = JSON.parse(message.slice(boundary + 8))
      if (parsed?.logSchema === 1 && typeof parsed.action === 'string' && typeof parsed.result === 'string') {
        event = parsed
        content = message.slice(0, boundary).trim() || '（无补充说明）'
      }
    } catch {
      // 旧日志或截断记录保留原文，不能猜测结果。
    }
  }
  const displayMessage = cleanRuntimeMessage(message)
  const rawAction = event.action || '历史运行事件'
  const action = friendlyActionText(rawAction)
  const rawOutcome = event.outcome || content
  const outcome = friendlyOutcomeText(rawOutcome)

  return {
    sequence, raw, createdAt, level, source, sourceShort, requestId,
    message: displayMessage, summary,
    action, result: event.result || 'unknown',
    object: event.object || event.section || '', outcome,
    ...timeParts(createdAt), ...levels[level]
  }
}

export function summarizeRuntime(rows) {
  return {
    total: rows.length,
    info: rows.filter(row => row.level === 'info').length,
    warning: rows.filter(row => row.level === 'warning').length,
    error: rows.filter(row => ['error', 'fatal', 'panic'].includes(row.level)).length
  }
}

export function formatFileSize(value) {
  const bytes = Number(value)
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}