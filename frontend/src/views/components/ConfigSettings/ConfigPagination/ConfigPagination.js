import { computed, ref } from 'vue'

// 只分页展示，不切割源配置；绝对索引确保编辑和移除始终命中原条目。
export function usePagination(items, pageSize = 6) {
  const requestedPage = ref(1)
  const total = computed(() => items.value.length)
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
  const currentPage = computed({
    get: () => Math.min(requestedPage.value, pageCount.value),
    set: value => { requestedPage.value = Math.max(1, Math.min(pageCount.value, Number(value) || 1)) }
  })
  const pageItems = computed(() => {
    const start = (currentPage.value - 1) * pageSize
    return items.value.slice(start, start + pageSize).map((entry, offset) => ({ entry, index: start + offset }))
  })
  return { currentPage, pageCount, pageItems, total }
}
