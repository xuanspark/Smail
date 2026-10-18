import {computed, onBeforeUnmount, ref, watch} from 'vue'

import {useRoute, useRouter} from 'vue-router'

import {Document, Back, Delete, Download, Operation} from '@element-plus/icons-vue';

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

  // 翻译相关状态
  const isTranslated = ref(false)
  const translating = ref(false)
  const translationCache = ref(null)
  const detectedLang = ref('')

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

    // 重置翻译状态
    isTranslated.value = false
    translating.value = false
    translationCache.value = null
    detectedLang.value = ''

    const rawId = String(route.params.id || '')
    const id = Number(rawId)
    if (!/^\d+$/.test(rawId) || !Number.isSafeInteger(id) || id <= 0) {
      loadError.value = '邮件地址无效，请返回邮件列表'
      loading.value = false
      return
    }

    try {
      const res = await http.post('/api/v1/email/detail', {id})
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

  const toggleTranslate = async () => {
    if (loading.value || loadError.value) return
    if (isTranslated.value) {
      isTranslated.value = false
      return
    }

    if (translationCache.value) {
      isTranslated.value = true
      return
    }

    const emailId = Number(detailData.value?.id)
    if (!Number.isSafeInteger(emailId) || emailId <= 0) return

    translating.value = true
    try {
      const res = await http.post('/api/v1/email/translate', {
        id: emailId,
        target_lang: 'zh-CN'
      })
      if (res?.errorNo === 0 && res.data) {
        translationCache.value = res.data
        detectedLang.value = res.data.detected_lang || 'auto'
        isTranslated.value = true
        ElMessage.success('翻译完成')
      } else {
        ElMessage.error(res?.errorMsg || '翻译失败，请稍后重试')
      }
    } catch (err) {
      ElMessage.error('翻译服务连接失败，请稍后重试')
    } finally {
      translating.value = false
    }
  }

  const displaySubject = computed(() => {
    if (isTranslated.value && translationCache.value?.subject_translated) {
      return translationCache.value.subject_translated
    }
    return detailData.value?.subject || ''
  })

  const displayContent = computed(() => {
    if (isTranslated.value && translationCache.value?.content_translated) {
      return translationCache.value.content_translated
    }
    if (detailData.value?.html) {
      return detailData.value.html
    }
    return detailData.value?.text || ''
  })

  const displayIsText = computed(() => {
    if (isTranslated.value) {
      return true
    }
    return !detailData.value?.html
  })

  const backToList = () => router.push({name: 'list'})
  const handleReply = () => {
    const id = Number(detailData.value.id)
    if (loading.value || loadError.value || detailData.value.type !== 0 || !Number.isSafeInteger(id) || id <= 0) return
    return router.push({name: 'editer', query: {reply: String(id)}})
  }
  
  const cleanSenderText = (text) => {
    if (!text) return String();
    return String(text).replace(/[\u200B-\u200D\u2060-\u206F\uFEFF]/g, String()).trim();
  };

  const getInitial = (name) => {
    const cleaned = cleanSenderText(name);
    if (!cleaned) return '?';
    return cleaned.charAt(0).toUpperCase();
  };

  const formatSenderName = (fromName, fromAddress) => {
    const cleaned = cleanSenderText(fromName);
    return cleaned || fromAddress || String();
  };

  const hasSenderName = (fromName) => {
    return cleanSenderText(fromName).length > 0;
  };
  
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
    Operation,
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
    cleanSenderText,
    formatSenderName,
    hasSenderName,
    formatDetailDate,
    handleDelete,
    isTranslated,
    translating,
    detectedLang,
    displaySubject,
    displayContent,
    displayIsText,
    toggleTranslate
  }
}

export {
  Back,
  Delete,
  Document,
  Download,
  Operation
}
