import { computed, ref, watch } from 'vue'

// 只分页展示，不切割源配置；绝对索引确保编辑和移除始终命中原条目。
export function usePagination(items, pageSize = 6) {
  const size = Math.max(1, Math.floor(Number(pageSize)) || 6)
  const requestedPage = ref(1)
  const total = computed(() => items.value.length)
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / size)))
  const currentPage = computed({
    get: () => Math.min(requestedPage.value, pageCount.value),
    set: value => { requestedPage.value = Math.max(1, Math.min(pageCount.value, Math.floor(Number(value)) || 1)) }
  })
  watch(pageCount, count => {
    requestedPage.value = Math.min(requestedPage.value, count)
  }, { flush: 'sync' })
  const pageItems = computed(() => {
    const start = (currentPage.value - 1) * size
    return items.value.slice(start, start + size).map((entry, offset) => ({ entry, index: start + offset }))
  })
  return { currentPage, pageCount, pageItems, total }
}
