import {onBeforeUnmount, ref, watch} from 'vue'

import {useRoute, useRouter} from 'vue-router'

import {Document, Back, Delete, Download} from '@element-plus/icons-vue';

import {ElMessage, ElMessageBox} from 'element-plus';

import lang from '../../i18n/i18n';

import {http} from "@/utils/axios";

import useGroupStore from '../../stores/group';

export function usePage() {
  const route = useRoute()
  
  const router = useRouter()
  
  const groupStore = useGroupStore()
  
  const detailData = ref({
    attachments: []
  })
  
  const tos = ref([])
  
  const ccs = ref([])

  const bccs = ref([])
  
  const showCC = ref(false)

  const showBCC = ref(false)

  const loading = ref(false)
  const loadError = ref('')
  let requestSequence = 0

  const parseAddresses = (value) => {
    try {
      const addresses = JSON.parse(value || '[]')
      return Array.isArray(addresses) ? addresses : []
    } catch {
      return []
    }
  }

  const loadDetail = async () => {
    const sequence = ++requestSequence
    detailData.value = {attachments: []}
    tos.value = []
    ccs.value = []
    bccs.value = []
    showCC.value = false
    showBCC.value = false
    loadError.value = ''
    loading.value = true

    const rawId = String(route.params.id || '')
    const id = Number(rawId)
    if (!/^\d+$/.test(rawId) || !Number.isSafeInteger(id) || id <= 0) {
      loadError.value = '邮件地址无效，请返回邮件列表'
      loading.value = false
      return
    }

    try {
      const res = await http.post('/api/v1/email/detail', {id})
      // 切换邮件或离开页面后，旧请求不得覆盖当前状态。
      if (sequence !== requestSequence) return
      if (res?.errorNo !== 0) {
        loadError.value = res?.errorMsg || '读取邮件失败，请重试'
        return
      }
      const data = res.data
      if (!data || typeof data !== 'object' || Array.isArray(data) || Number(data.id) !== id) {
        loadError.value = '邮件不可用，请返回邮件列表'
        return
      }
      detailData.value = {...data, attachments: Array.isArray(data.attachments) ? data.attachments : []}
      tos.value = parseAddresses(data.to)
      ccs.value = parseAddresses(data.cc)
      showCC.value = ccs.value.length > 0
      // 仅在本人的发件副本展示密送；后端仍负责所有权校验和收件副本脱敏。
      bccs.value = data.type === 1 ? parseAddresses(data.bcc) : []
      showBCC.value = bccs.value.length > 0
    } catch {
      if (sequence === requestSequence) {
        loadError.value = '网络连接失败，请稍后重试'
      }
    } finally {
      if (sequence === requestSequence) loading.value = false
    }
  }

  watch(() => route.params.id, loadDetail, {immediate: true})
  onBeforeUnmount(() => {
    requestSequence++
  })

  const backToList = () => router.push({name: 'list'})
  const handleReply = () => {
    const id = Number(detailData.value.id)
    if (loading.value || loadError.value || detailData.value.type !== 0 || !Number.isSafeInteger(id) || id <= 0) return
    return router.push({name: 'editer', query: {reply: String(id)}})
  }
  
  const getInitial = (name) => {
    if (!name) return '?';
    return name.charAt(0).toUpperCase();
  }
  
  const formatDetailDate = (dateStr) => {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    return d.toLocaleString([], {
      year: 'numeric', month: 'short', day: 'numeric',
      hour: '2-digit', minute:'2-digit',
      hour12: false
    });
  }
  
  const handleDelete = () => {
    if (loading.value || loadError.value) return;
    const id = detailData.value.id;
    if (!id) return;
  
    let tag = groupStore.tag;
    if (!tag) {
      tag = '{"type":0,"status":-1}';
    }
  
    let forcedDel = false;
    try {
      const groupTag = JSON.parse(tag);
      forcedDel = groupTag.status === 3;
    } catch (e) {
      forcedDel = false;
    }
  
    ElMessageBox.confirm(lang.del_email_confirm, '警告', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    }).then(() => {
      http.post("/api/v1/email/del", {ids: [id], forcedDel}).then(res => {
        if (res.errorNo === 0) {
          ElMessage.success('删除成功');
          router.push({name: 'list'});
        } else {
          ElMessage.error(res.errorMsg);
        }
      });
    }).catch(() => {});
  }

  return {
    ref,
    useRoute,
    useRouter,
    Document,
    Back,
    Delete,
    Download,
    ElMessage,
    ElMessageBox,
    lang,
    http,
    useGroupStore,
    route,
    router,
    groupStore,
    detailData,
    tos,
    ccs,
    showCC,
    bccs,
    showBCC,
    loading,
    loadError,
    loadDetail,
    backToList,
    handleReply,
    getInitial,
    formatDetailDate,
    handleDelete
  }
}

export {
  Back,
  Delete,
  Document,
  Download
}
