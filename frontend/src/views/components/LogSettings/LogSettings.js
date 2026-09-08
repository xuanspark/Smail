import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { http } from '@/utils/axios'
import { parseRuntimeLine, summarizeRuntime, timeParts } from './RuntimeLog.js'

export function useLogs() {
  const category = ref('operation')
  const loading = ref(false)
  const error = ref('')
  const items = ref([])
  const lines = ref([])
  const offset = ref(0)
  const hasMore = ref(false)
  const actorId = ref('')
  const recentRange = () => {
    const end = new Date()
    return [new Date(end.getTime() - 24 * 60 * 60 * 1000), end]
  }
  const dateRange = ref(recentRange())
  const detail = ref(null)
  const runtimeDetail = ref(null)
  const pageSize = 10
  let requestId = 0
  let aborter
  const previousOffsets = []
  let appliedFilters = {}

  const categories = [
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
  const runtimeRows = computed(() => lines.value.map((line, index) => parseRuntimeLine(line, offset.value + index + 1)))
  const runtimeSummary = computed(() => summarizeRuntime(runtimeRows.value))
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
      const params = { ...appliedFilters, offset: offset.value, limit: pageSize }
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
    // 只在查询时采纳筛选条件，翻页沿用快照，不随当前时间或未提交输入漂移。
    if (!dateRange.value?.length) dateRange.value = recentRange()
    const [from, to] = (dateRange.value || []).map(value => new Date(value))
    if (!from || !to || !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from > to) {
      ++requestId
      aborter?.abort()
      loading.value = false
      error.value = '请选择有效的起止时间'
      items.value = []
      lines.value = []
      hasMore.value = false
      return
    }
    appliedFilters = { kind: category.value, from: from.toISOString(), to: to.toISOString() }
    if (!isRuntime.value && actorId.value.trim()) appliedFilters.actorId = actorId.value.trim()
    offset.value = 0
    previousOffsets.length = 0
    detail.value = null
    runtimeDetail.value = null
    return load()
  }
  function recent() {
    dateRange.value = recentRange()
    return search()
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
  onMounted(search)
  onBeforeUnmount(() => {
    ++requestId
    aborter?.abort()
  })

  return {
    category, categories, loading, error, items, lines, offset,
    hasMore, actorId, dateRange, detail, details, isRuntime,
    runtimeDetail, runtimeRows, runtimeSummary, visibleCount, pageRange,
    currentCategory, resultType, runtimeRowClass, timeParts,
    formatTime, resultLabel, actionLabel, displayActor, search, recent, next, previous
  }
}
