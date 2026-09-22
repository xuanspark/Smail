import './List.js'

import {EpArrowDownBold} from "vue-icons-plus/ep";

import {Delete, View, Folder, EditPen, Warning, CircleCheck, Switch, Check, Refresh} from "@element-plus/icons-vue";

import {useRouter} from 'vue-router'

import {ref, watch, computed, onMounted, onUnmounted} from 'vue'

import useGroupStore from '../../stores/group'
import {useGlobalStatusStore} from '../../stores/useGlobalStatusStore'

import lang from '../../i18n/i18n';

import {http} from "@/utils/axios";

import {ElMessage, ElMessageBox} from "element-plus";

export function usePage() {
  const router = useRouter();
  
  const groupStore = useGroupStore()
  const globalStatus = useGlobalStatusStore()
  
  const groupList = ref([])
  const isRefreshing = ref(false)
  let refreshTimer = null
  let requestVersion = 0
  const currentPage = ref(1)
  
  const taskTableDataRef = ref(null)
  
  const selectedRows = ref([])
  
  const errorDialogVisible = ref(false)
  const currentErrorMsg = ref('')

  let tag = groupStore.tag;
  
  if (tag === "") {
    tag = '{"type":0,"status":-1}'
  }

  const isOutbox = computed(() => {
    try {
      const parsed = JSON.parse(groupStore.tag || tag || '{}')
      return parsed.type === 1 || groupStore.name === lang.outbox || groupStore.name === '已发送'
    } catch {
      return false
    }
  })
  
  watch([() => groupStore.tag, () => groupStore.keyword], () => {
    tag = groupStore.tag;
    currentPage.value = 1;
    taskTableDataRef.value?.clearSelection();
    if (tag === "") {
      tag = '{"type":0,"status":-1}'
    }
    data.value = []
    updateList()
  })
  
  const data = ref([])
  
  const totalPage = ref(0)
  
  const updateList = async function (silent = false) {
    if (silent && selectedRows.value.length > 0) return
    const version = ++requestVersion
    if (!silent) {
      isRefreshing.value = true
    }
    try {
      const res = await http.post("/api/v1/email/list", {
        tag, keyword: groupStore.keyword, current_page: currentPage.value, page_size: 15
      })
      if (version !== requestVersion) return
      if (res.errorNo !== 0) throw new Error(res.errorMsg)
      const available = new Set((res.data.list || []).map(row => row.id))
      selectedRows.value.filter(row => !available.has(row.id)).forEach(row => {
        taskTableDataRef.value?.toggleRowSelection(row, false)
      })
      data.value = res.data.list || []
      totalPage.value = res.data.total_page || 0
      if (currentPage.value > Math.max(1, totalPage.value)) {
        currentPage.value = Math.max(1, totalPage.value)
        await updateList(silent)
      }
    } catch (err) {
      if (!silent && version === requestVersion) {
        ElMessage.error(err?.message || '获取邮件列表失败')
      }
    } finally {
      if (!silent && version === requestVersion) {
        isRefreshing.value = false
      }
    }
  }
  
  const updateGroupList = async function () {
    try {
      const res = await http.get("/api/v1/group/list")
      if (res.errorNo !== 0) {
        throw new Error(res.errorMsg || "分组列表加载失败")
      }
      groupList.value = res.data || []
    } catch (error) {
      groupList.value = []
      ElMessage.error(error?.message || "分组列表加载失败，请稍后重试")
    }
  }
  
  updateList()
  
  updateGroupList()
  watch(() => groupStore.revision, updateGroupList)

  // 定时刷新逻辑：已登录 60s，未登录 300s
  const getRefreshInterval = () => {
    return globalStatus.isLogin ? 60000 : 300000
  }

  const startAutoRefreshTimer = () => {
    stopAutoRefreshTimer()
    const interval = getRefreshInterval()
    refreshTimer = setInterval(() => {
      updateList(true)
      updateGroupList()
    }, interval)
  }

  const stopAutoRefreshTimer = () => {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
  }

  const manualRefresh = async () => {
    await Promise.all([
      updateList(false),
      updateGroupList()
    ])
    ElMessage.success('已刷新')
    startAutoRefreshTimer()
  }

  watch(() => globalStatus.isLogin, () => {
    startAutoRefreshTimer()
  })

  onMounted(() => {
    startAutoRefreshTimer()
  })

  onUnmounted(() => {
    requestVersion += 1
    stopAutoRefreshTimer()
  })
  
  const rowClick = function (row) {
    router.push("/detail/" + row.id)
  }
  
  const formatShortDate = (dateStr) => {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    const now = new Date();
    if (d.toDateString() === now.toDateString()) {
      return d.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
    }
    return d.toLocaleDateString([], {month: 'short', day: 'numeric'});
  }
  
  const selectionChange = (rows) => {
    selectedRows.value = rows
  }
  
  const selectAll = () => {
    taskTableDataRef.value?.clearSelection()
    data.value.forEach(row => taskTableDataRef.value?.toggleRowSelection(row, true))
  }
  
  const invertSelection = () => {
    const selectedIds = new Set(selectedRows.value.map(row => row.id))
    data.value.forEach(row => {
      taskTableDataRef.value?.toggleRowSelection(row, !selectedIds.has(row.id))
    })
  }
  
  const markRead = function () {
    let rows = taskTableDataRef.value?.getSelectionRows()
    if (!rows || rows.length === 0) {
      ElMessage.warning('请先选择邮件');
      return;
    }
    let ids = rows.map(e => e.id);
    http.post("/api/v1/email/read", {"ids": ids}).then(res => {
      if (res.errorNo === 0) {
        updateList()
        ElMessage.success('已标记为已读');
      } else {
        ElMessage.error(res.errorMsg)
      }
    })
  }
  
  const move = function (group_id, group_name) {
    let rows = taskTableDataRef.value?.getSelectionRows()
    if (!rows || rows.length === 0) {
      ElMessage.warning('请先选择邮件');
      return;
    }
    let ids = rows.map(e => e.id);
  
    ElMessageBox.confirm(lang.move_email_confirm, '警告', {
      confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning'
    }).then(() => {
      http.post("/api/v1/email/move", {"group_id": group_id, "group_name": group_name, "ids": ids}).then(res => {
        if (res.errorNo === 0) {
          updateList()
          ElMessage.success('移动完成')
        } else {
          ElMessage.error(res.errorMsg)
        }
      })
    }).catch(()=>{})
  }
  
  const del = function () {
    let rows = taskTableDataRef.value?.getSelectionRows()
    if (!rows || rows.length === 0) {
      ElMessage.warning('请先选择邮件');
      return;
    }
    let ids = rows.map(e => e.id);
    let groupTag = JSON.parse(tag)
  
    ElMessageBox.confirm(lang.del_email_confirm, '警告', {
      confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning'
    }).then(() => {
      http.post("/api/v1/email/del", {"ids": ids, "forcedDel": groupTag.status === 3}).then(res => {
        if (res.errorNo === 0) {
          updateList()
          ElMessage.success('删除成功')
        } else {
          ElMessage.error(res.errorMsg)
        }
      })
    }).catch(()=>{})
  }
  
  const rowStyle = function () {
    return {'cursor': 'pointer'}
  }
  
  const pageChange = function (p) {
    currentPage.value = p
    taskTableDataRef.value?.clearSelection()
    updateList()
  }

  const trustSender = function (senderEmail) {
    if (!senderEmail) return
    ElMessageBox.confirm(
      `确定将发件人 "${senderEmail}" 加入信任白名单吗？后续来自该地址的新邮件将不再提示风险。`,
      '信任发件人',
      {
        confirmButtonText: '信任并加入白名单',
        cancelButtonText: '取消',
        type: 'warning'
      }
    ).then(() => {
      http.post("/api/v1/email/trust-sender", {email: senderEmail, trusted: true}).then(res => {
        if (res.errorNo === 0) {
          ElMessage.success('已加入信任白名单，风险提示已免除')
          // 局部更新视图，避免整页重新刷新
          data.value.forEach(item => {
            if (item.sender?.EmailAddress === senderEmail) {
              item.dangerous = false
            }
          })
        } else {
          ElMessage.error(res.errorMsg || '操作失败')
        }
      })
    }).catch(() => {})
  }

  const showError = function (row) {
    currentErrorMsg.value = row.error || '未知投递错误'
    errorDialogVisible.value = true
  }

  return {
    EpArrowDownBold,
    Delete,
    View,
    Folder,
    EditPen,
    Warning,
    Check,
    CircleCheck,
    Switch,
    useRouter,
    ref,
    watch,
    computed,
    useGroupStore,
    lang,
    http,
    ElMessage,
    ElMessageBox,
    router,
    groupStore,
    groupList,
    taskTableDataRef,
    selectedRows,
    tag,
    data,
    totalPage,
    currentPage,
    updateList,
    updateGroupList,
    rowClick,
    formatShortDate,
    selectionChange,
    selectAll,
    invertSelection,
    markRead,
    move,
    del,
    rowStyle,
    pageChange,
    isOutbox,
    trustSender,
    showError,
    errorDialogVisible,
    currentErrorMsg,
    isRefreshing,
    manualRefresh
  }
}

export {
  CircleCheck,
  Switch,
  Delete,
  View,
  Folder,
  EpArrowDownBold,
  EditPen,
  Warning,
  Check,
  Refresh
}
