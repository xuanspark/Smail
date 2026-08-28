import './EmailDetail.js'

import {ref} from 'vue'

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
  
  const showCC = ref(false)
  
  http.post("/api/email/detail", {id: parseInt(route.params.id)}).then(res => {
    detailData.value = res.data || {}
    detailData.value.attachments = res.data.attachments || [];
    
    if (res.data.to && res.data.to !== "") {
      try {
        tos.value = JSON.parse(res.data.to)
      } catch (error) {
        tos.value = []
      }
    }
    if (res.data.cc && res.data.cc !== "") {
      try {
        ccs.value = JSON.parse(res.data.cc)
      } catch (error) {
        ccs.value = []
      }
    }
    showCC.value = ccs.value && ccs.value.length > 0
  })
  
  const getInitial = (name) => {
    if (!name) return '?';
    return name.charAt(0).toUpperCase();
  }
  
  const formatDetailDate = (dateStr) => {
    if (!dateStr) return "";
    const d = new Date(dateStr);
    return d.toLocaleString([], {
      year: 'numeric', month: 'short', day: 'numeric',
      hour: '2-digit', minute:'2-digit'
    });
  }
  
  const handleDelete = () => {
    const id = detailData.value.id || parseInt(route.params.id);
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
      http.post("/api/email/del", {ids: [id], forcedDel}).then(res => {
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
