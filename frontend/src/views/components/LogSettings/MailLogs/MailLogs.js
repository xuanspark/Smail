import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { timeParts } from '../RuntimeLog.js'

export function useMailLogs() {
  const store = useGlobalStatusStore()
  const canFilterActor = computed(() => ['super_admin', 'admin'].includes(store.role))
  const loading = ref(false)
  const error = ref('')
  const items = ref([])
  const offset = ref(0)
  const hasMore = ref(false)
  const actorId = ref('')
  const mailScope = ref('all')
  const canFilterSystemMail = computed(() => store.role === 'super_admin')
  const systemMailSelected = computed(() => canFilterSystemMail.value && mailScope.value === 'system')

  const recentRange = () => {
    const end = new Date()
    return [new Date(end.getTime() - 24 * 60 * 60 * 1000), end]
  }
  const dateRange = ref(recentRange())
  const detail = ref(null)
  const pageSize = 10
  let requestId = 0
  let aborter
  const previousOffsets = []
  let appliedFilters = {}

  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
  const resultLabel = value => ({ success: '成功', failure: '失败', unknown: '未确认', accepted: '对端接收', pending: '待验证', started: '已开始', recorded: '状态记录', partial: '部分接收', discarded: '已丢弃', skipped: '已跳过' }[value] || value || '未记录')
  const actionLabel = value => ({ update: '保存配置', rollback: '回滚配置', migration: '迁移配置' }[value] || value || '未记录操作')
  const displayObject = item => item.payload?.object || item.payload?.stage || '未记录对象'
  const displayOutcome = item => item.payload?.outcome || '历史记录未提供结果说明'
  const displayActor = item => !item.actorId || item.actorId === '0' ? '系统' : (item.payload?.actorName || item.actorId)
  const details = computed(() => detail.value?.payload || {})
  const pageRange = computed(() => items.value.length ? \第 ${offset.value + 1}–${offset.value + items.value.length} 条\ : '暂无记录')
  const resultType = value => ({ success: 'success', accepted: 'primary', failure: 'danger', pending: 'warning', partial: 'warning', discarded: 'warning', started: 'primary' }[value] || 'info')

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
      hasMore.value = Boolean(data.hasMore)
    } catch (cause) {
      if (id === requestId && cause?.code !== 'ERR_CANCELED') {
        error.value = cause.message || '日志查询失败'
        items.value = []
        hasMore.value = false
      }
    } finally {
      if (id === requestId) loading.value = false
    }
  }

  function search() {
    if (!dateRange.value?.length) dateRange.value = recentRange()
    const [from, to] = (dateRange.value || []).map(value => new Date(value))
    if (!from || !to || !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from > to) {
      ++requestId
      aborter?.abort()
      loading.value = false
      error.value = '请选择有效的起止时间'
      items.value = []
      hasMore.value = false
      return
    }
    appliedFilters = { kind: 'mail', from: from.toISOString(), to: to.toISOString() }
    if (systemMailSelected.value) {
      appliedFilters.systemOnly = true
    } else if (canFilterActor.value && actorId.value.trim()) {
      appliedFilters.actorId = actorId.value.trim()
    }
    offset.value = 0
    previousOffsets.length = 0
    detail.value = null
    return load()
  }

  function recent() {
    dateRange.value = recentRange()
    return search()
  }

  function next() {
    if (loading.value || !hasMore.value) return
    previousOffsets.push(offset.value)
    offset.value += pageSize
    return load()
  }

  function previous() {
    if (loading.value || offset.value === 0) return
    offset.value = previousOffsets.pop() ?? 0
    return load()
  }

  watch(mailScope, search)
  onMounted(search)
  onBeforeUnmount(() => {
    ++requestId
    aborter?.abort()
  })

  return {
    loading,
    error,
    items,
    offset,
    hasMore,
    actorId,
    canFilterActor,
    mailScope,
    canFilterSystemMail,
    systemMailSelected,
    dateRange,
    detail,
    details,
    pageRange,
    resultType,
    timeParts,
    formatTime,
    resultLabel,
    actionLabel,
    displayActor,
    displayObject,
    displayOutcome,
    search,
    recent,
    next,
    previous
  }
}