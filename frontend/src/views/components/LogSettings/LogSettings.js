import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { http } from '@/utils/axios'
import { formatFileSize, parseRuntimeLine, summarizeRuntime, timeParts } from './RuntimeLog.js'

export function useLogs() {
  const category = ref('config')
  const loading = ref(false)
  const error = ref('')
  const items = ref([])
  const files = ref([])
  const selectedFile = ref('')
  const lines = ref([])
  const offset = ref(0)
  const hasMore = ref(false)
  const actorId = ref('')
  const dateRange = ref([])
  const detail = ref(null)
  const runtimeDetail = ref(null)
  const pageSize = 50
  let requestId = 0
  let aborter
  const previousOffsets = []

  const categories = [
    { label: '配置审计', value: 'config' },
    { label: '操作记录', value: 'operation' },
    { label: '收发信记录', value: 'mail' },
    { label: '运行日志', value: 'runtime' }
  ]
  const isRuntime = computed(() => category.value === 'runtime')
  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
  const resultLabel = value => ({ success: '成功', failure: '失败', unknown: '结果未知', accepted: '已接收' }[value] || value)
  const actionLabel = value => ({ update: '保存配置', rollback: '回滚配置', migration: '迁移配置' }[value] || value)
  const displayActor = item => item.payload?.actorName || (item.actorId && item.actorId !== '0' ? item.actorId : '系统')
  const details = computed(() => detail.value?.payload || {})
  const changes = computed(() => details.value.changes || [])
  const runtimeRows = computed(() => lines.value.map((line, index) => parseRuntimeLine(line, offset.value + index + 1)))
  const runtimeSummary = computed(() => summarizeRuntime(runtimeRows.value))
  const currentFile = computed(() => files.value.find(file => file.name === selectedFile.value))
  const visibleCount = computed(() => isRuntime.value ? lines.value.length : items.value.length)
  const pageRange = computed(() => visibleCount.value ? `第 ${offset.value + 1}–${offset.value + visibleCount.value} 条` : '暂无记录')
  const currentCategory = computed(() => categories.find(item => item.value === category.value)?.label)
  const resultType = value => ({ success: 'success', accepted: 'success', failure: 'danger' }[value] || 'info')
  const runtimeRowClass = ({ row }) => ['warning', 'danger'].includes(row.type) ? `log-row-${row.type}` : ''

  function requireData(result) {
    if (result?.errorNo !== 0 || !result.data) throw new Error(result?.errorMsg || '日志查询失败')
    return result.data
  }

  async function load() {
    const id = ++requestId
    aborter?.abort()
    aborter = new AbortController()
    loading.value = true
    error.value = ''
    try {
      const params = { kind: category.value, offset: offset.value, limit: pageSize }
      if (isRuntime.value) {
        const listing = requireData(await http.get('/api/v1/logs', {
          params: { kind: 'runtime' }, signal: aborter.signal
        }))
        if (id !== requestId) return
        files.value = listing.files || []
        if (!files.value.some(file => file.name === selectedFile.value)) {
          selectedFile.value = files.value[0]?.name || ''
          offset.value = 0
          params.offset = 0
          previousOffsets.length = 0
        }
        if (!selectedFile.value) {
          lines.value = []
          hasMore.value = false
          return
        }
        params.file = selectedFile.value
      } else {
        if (actorId.value.trim()) params.actorId = actorId.value.trim()
        if (dateRange.value?.length === 2) {
          params.from = new Date(dateRange.value[0]).toISOString()
          params.to = new Date(dateRange.value[1]).toISOString()
        }
      }
      const data = requireData(await http.get('/api/v1/logs', { params, signal: aborter.signal }))
      if (id !== requestId) return
      items.value = data.items || []
      lines.value = data.lines || []
      hasMore.value = Boolean(data.hasMore)
    } catch (cause) {
      if (id === requestId && cause?.code !== 'ERR_CANCELED') {
        error.value = cause.message || '日志查询失败'
        items.value = []
        lines.value = []
        hasMore.value = false
      }
    } finally {
      if (id === requestId) loading.value = false
    }
  }

  function search() {
    offset.value = 0
    previousOffsets.length = 0
    return load()
  }
  function next() {
    if (loading.value || !hasMore.value) return
    previousOffsets.push(offset.value)
    offset.value += isRuntime.value ? lines.value.length : pageSize
    return load()
  }
  function previous() {
    if (loading.value || offset.value === 0) return
    offset.value = previousOffsets.pop() ?? 0
    return load()
  }
  watch(category, () => {
    detail.value = null
    runtimeDetail.value = null
    search()
  })
  onMounted(load)
  onBeforeUnmount(() => {
    ++requestId
    aborter?.abort()
  })

  return {
    category, categories, loading, error, items, files, selectedFile, lines, offset,
    hasMore, actorId, dateRange, detail, details, changes, isRuntime,
    runtimeDetail, runtimeRows, runtimeSummary, currentFile, visibleCount, pageRange,
    currentCategory, resultType, runtimeRowClass, timeParts, formatFileSize,
    formatTime, resultLabel, actionLabel, displayActor, search, next, previous
  }
}
