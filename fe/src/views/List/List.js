import './List.js'

import {EpArrowDownBold} from "vue-icons-plus/ep";

import {Delete, View, Folder, EditPen, Warning, CircleCheck, Switch} from "@element-plus/icons-vue";

import {useRouter} from 'vue-router'

import {ref, watch} from 'vue'

import useGroupStore from '../../stores/group'

import lang from '../../i18n/i18n';

import {http} from "@/utils/axios";

import {ElMessage, ElMessageBox} from "element-plus";

export function usePage() {
  const router = useRouter();
  
  const groupStore = useGroupStore()
  
  const groupList = ref([])
  
  const taskTableDataRef = ref(null)
  
  const selectedRows = ref([])
  
  let tag = groupStore.tag;
  
  if (tag === "") {
    tag = '{"type":0,"status":-1}'
  }
  
  watch(groupStore, async (newV) => {
    tag = newV.tag;
    if (tag === "") {
      tag = '{"type":0,"status":-1}'
    }
    data.value = []
    updateList()
  })
  
  const data = ref([])
  
  const totalPage = ref(0)
  
  const updateList = function () {
    http.post("/api/email/list", {tag: tag, page_size: 15}).then(res => {
      data.value = res.data.list || []
      totalPage.value = res.data.total_page || 0
    })
  }
  
  const updateGroupList = function () {
    http.post("/api/group/list").then(res => {
      groupList.value = res.data || []
    })
  }
  
  updateList()
  
  updateGroupList()
  
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
    http.post("/api/email/read", {"ids": ids}).then(res => {
      if (res.errorNo === 0) {
        updateList()
        ElMessage.success('Marked as read');
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
      http.post("/api/email/move", {"group_id": group_id, "group_name": group_name, "ids": ids}).then(res => {
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
      http.post("/api/email/del", {"ids": ids, "forcedDel": groupTag.status === 3}).then(res => {
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
    http.post("/api/email/list", {tag: tag, page_size: 15, current_page: p}).then(res => {
      data.value = res.data.list || []
    })
  }

  return {
    EpArrowDownBold,
    Delete,
    View,
    Folder,
    EditPen,
    Warning,
    CircleCheck,
    Switch,
    useRouter,
    ref,
    watch,
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
    pageChange
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
  Warning
}
