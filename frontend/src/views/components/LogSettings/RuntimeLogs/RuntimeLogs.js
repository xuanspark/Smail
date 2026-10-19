import { computed, onMounted, ref } from 'vue'
import { http } from '@/utils/axios'
import { parseRuntimeLine, summarizeRuntime, timeParts } from '../RuntimeLog.js'

const PAGE_SIZE = 10

export function useRuntimeLogs() {
  const loading = ref(false)
  const error = ref('')
  const items = ref([])
  const offset = ref(0)
  const hasMore = ref(false)
  const recentRange = () => {
    const end = new Date()
    return [new Date(end.getTime() - 24 * 60 * 60 * 1000), end]
  }
  const dateRange = ref(recentRange())
  const runtimeDetail = ref(null)
  let requestId = 0
  let aborter
  const previousOffsets = []
  let appliedFilters = {}

  const runtimeRows = computed(() => items.value.map(line => parseRuntimeLine(line, timeParts)))
  const runtimeSummary = computed(() => summarizeRuntime(runtimeRows.value))

  const pageRange = computed(() => {
    if (items.value.length === 0) {
      return '????'
    }
    const start = offset.value + 1
    const end = offset.value + items.value.length
    return `? ${start}-${end} ?`
  })

  function resultType(result) {
    if (result === 'success') return 'success'
    if (result === 'failure' || result === 'failed') return 'danger'
    if (result === 'blocked') return 'warning'
    return 'info'
  }

  function resultLabel(result) {
    return {
      success: '??',
      failure: '??',
      failed: '??',
      blocked: '???',
      unknown: '???',
      accepted: '????',
      pending: '???',
      started: '???',
      recorded: '????',
      partial: '????',
      discarded: '???',
      skipped: '???'
    }[result] || result || '???'
  }

  function runtimeRowClass({ row }) {
    if (row.type === 'danger') return 'log-row-danger'
    if (row.type === 'warning') return 'log-row-warning'
    return ''
  }

  function formatTime(isoString) {
    if (!isoString) return '?'
    const date = new Date(isoString)
    if (isNaN(date.getTime())) return isoString
    return date.toLocaleString('zh-CN', { hour12: false })
  }

  function requireData(result) {
    if (result?.errorNo !== 0 || !result.data) throw new Error(result?.errorMsg || '??????')
    return result.data
  }

  async function loadLogs() {
    const id = ++requestId
    aborter?.abort()
    aborter = new AbortController()
    loading.value = true
    error.value = ''
    try {
      const params = { ...appliedFilters, offset: offset.value, limit: PAGE_SIZE }
      const data = requireData(await http.get('/api/v1/logs', { params, signal: aborter.signal }))
      if (id !== requestId) return
      items.value = data.items || []
      hasMore.value = Boolean(data.hasMore)
    } catch (cause) {
      if (id === requestId && cause?.code !== 'ERR_CANCELED') {
        error.value = cause.message || '??????'
        items.value = []
        hasMore.value = false
      }
    } finally {
      if (id === requestId) loading.value = false
    }
  }

  function search() {
    if (!dateRange.value?.length) dateRange.value = recentRange()
    const [from, to] = (dateRange.value || []).map(val => new Date(val))
    if (!from || !to || !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from > to) {
      ++requestId
      aborter?.abort()
      loading.value = false
      error.value = '??????????'
      items.value = []
      hasMore.value = false
      return
    }
    appliedFilters = { kind: 'runtime', from: from.toISOString(), to: to.toISOString() }
    offset.value = 0
    previousOffsets.length = 0
    runtimeDetail.value = null
    return loadLogs()
  }

  function recent() {
    dateRange.value = recentRange()
    return search()
  }

  function next() {
    if (loading.value || !hasMore.value) return
    previousOffsets.push(offset.value)
    offset.value += PAGE_SIZE
    return loadLogs()
  }

  function previous() {
    if (loading.value || offset.value === 0) return
    offset.value = previousOffsets.pop() ?? 0
    return loadLogs()
  }

  onMounted(search)

  return {
    loading,
    error,
    items,
    offset,
    hasMore,
    dateRange,
    runtimeDetail,
    runtimeRows,
    runtimeSummary,
    pageRange,
    resultType,
    runtimeRowClass,
    formatTime,
    resultLabel,
    search,
    recent,
    next,
    previous
  }
}
