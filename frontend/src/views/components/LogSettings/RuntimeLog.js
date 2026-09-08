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
  return {
    sequence, raw, createdAt, level, source, sourceShort, requestId, message, summary,
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
