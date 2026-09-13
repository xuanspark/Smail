import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'

export function useSenderFilters() {
  const loading = ref(false)
  const filterList = ref([])
  const total = ref(0)
  const whitelistCount = ref(0)
  const blacklistCount = ref(0)

  const currentPage = ref(1)
  const pageSize = ref(7)
  const searchKeyword = ref('')
  const currentTab = ref('all') // 'all' | 'whitelist' | 'blacklist'

  const addForm = reactive({
    email: '',
    filter_type: 'whitelist',
  })
  const addLoading = ref(false)

  const loadFilters = async () => {
    loading.value = true
    try {
      const params = {
        page: currentPage.value,
        page_size: pageSize.value,
        keyword: searchKeyword.value.trim(),
      }
      if (currentTab.value === 'whitelist' || currentTab.value === 'blacklist') {
        params.filter_type = currentTab.value
      }
      const res = await http.get('/api/v1/email/sender-filters', { params })
      if (res.errorNo === 0 && res.data) {
        filterList.value = Array.isArray(res.data.list) ? res.data.list : []
        total.value = res.data.total || 0
        whitelistCount.value = res.data.whitelist_count || 0
        blacklistCount.value = res.data.blacklist_count || 0
      } else {
        filterList.value = []
        total.value = 0
        if (res.errorNo !== 0) {
          ElMessage.error(res.errorMsg || '加载名单失败')
        }
      }
    } catch (error) {
      filterList.value = []
      total.value = 0
      ElMessage.error(error?.message || '加载名单失败')
    } finally {
      loading.value = false
    }
  }

  const handleSearch = () => {
    currentPage.value = 1
    loadFilters()
  }

  const handleTabChange = () => {
    currentPage.value = 1
    loadFilters()
  }

  const handlePageChange = (page) => {
    currentPage.value = page
    loadFilters()
  }

  const handleAdd = async () => {
    const email = addForm.email.trim()
    if (!email) {
      ElMessage.warning('请输入发件人邮件地址')
      return
    }
    addLoading.value = true
    try {
      const res = await http.post('/api/v1/email/sender-filters', {
        email,
        filter_type: addForm.filter_type,
      })
      if (res.errorNo === 0) {
        ElMessage.success(addForm.filter_type === 'whitelist' ? '已成功添加至信任白名单' : '已成功添加至拦截黑名单')
        addForm.email = ''
        loadFilters()
      } else {
        ElMessage.error(res.errorMsg || '添加失败')
      }
    } catch (error) {
      ElMessage.error(error?.message || '添加失败')
    } finally {
      addLoading.value = false
    }
  }

  const handleToggle = async (row) => {
    const targetType = row.filter_type === 'whitelist' ? 'blacklist' : 'whitelist'
    const actionText = targetType === 'blacklist' ? '拉入黑名单' : '转为白名单'
    const tip = targetType === 'blacklist'
      ? `确定将发件人 "${row.email}" 拉入黑名单吗？后续来自该地址的邮件将受到强拦截与风险警示提示。`
      : `确定将发件人 "${row.email}" 转为信任白名单吗？`

    try {
      await ElMessageBox.confirm(tip, `确认${actionText}`, {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: targetType === 'blacklist' ? 'warning' : 'info',
      })
      const res = await http.post('/api/v1/email/sender-filters/toggle', {
        email: row.email,
        target_type: targetType,
      })
      if (res.errorNo === 0) {
        ElMessage.success(`已将 ${row.email} ${actionText}`)
        loadFilters()
      } else {
        ElMessage.error(res.errorMsg || `${actionText}失败`)
      }
    } catch (e) {
      if (e !== 'cancel') {
        ElMessage.error(e?.message || `${actionText}失败`)
      }
    }
  }

  const handleDelete = async (row) => {
    try {
      await ElMessageBox.confirm(
        `确定从名单中移除发件人 "${row.email}" 吗？`,
        '确认移除',
        {
          confirmButtonText: '确定移除',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )
      const res = await http.post('/api/v1/email/sender-filters/delete', {
        email: row.email,
        id: row.id,
      })
      if (res.errorNo === 0) {
        ElMessage.success('已移除发件人规则')
        loadFilters()
      } else {
        ElMessage.error(res.errorMsg || '移除失败')
      }
    } catch (e) {
      if (e !== 'cancel') {
        ElMessage.error(e?.message || '移除失败')
      }
    }
  }

  const formatDate = (dateStr) => {
    if (!dateStr) return '-'
    const d = new Date(dateStr)
    if (isNaN(d.getTime())) return dateStr
    const pad = (n) => String(n).padStart(2, '0')
    const year = d.getFullYear()
    const month = pad(d.getMonth() + 1)
    const day = pad(d.getDate())
    const hours = pad(d.getHours())
    const minutes = pad(d.getMinutes())
    return `${year}-${month}-${day} ${hours}:${minutes}`
  }

  onMounted(() => {
    loadFilters()
  })

  return {
    loading,
    filterList,
    total,
    whitelistCount,
    blacklistCount,
    currentPage,
    pageSize,
    searchKeyword,
    currentTab,
    addForm,
    addLoading,
    loadFilters,
    handleSearch,
    handleTabChange,
    handlePageChange,
    handleAdd,
    handleToggle,
    handleDelete,
    formatDate,
  }
}