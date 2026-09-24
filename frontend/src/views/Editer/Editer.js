import {useDraft} from './Draft'

import '@wangeditor/editor/dist/css/style.css'

import {ElMessage} from 'element-plus'

import {onBeforeUnmount, reactive, ref, shallowRef} from 'vue'

import {Close, Paperclip, Position, ArrowDown, Document, Delete} from '@element-plus/icons-vue';

import lang from '../../i18n/i18n';

import {Editor, Toolbar} from '@wangeditor/editor-for-vue'

import {i18nChangeLanguage} from '@wangeditor/editor'

import {useRouter} from 'vue-router';

import {http} from "@/utils/axios";

import useGroupStore from '../../stores/group'

import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";

export function usePage() {
  const router = useRouter();
  
  const groupStore = useGroupStore()
  
  const globalStatus = useGlobalStatusStore();
  
  const showCcBcc = ref(false);
  
  if (lang.lang === "zhCn") {
    i18nChangeLanguage('zh-CN')
  } else {
    i18nChangeLanguage('en')
  }
  
  const valueHtml = ref('<p><br></p>')
  
  const toolbarConfig = {}
  
  const editorConfig = {
    MENU_CONF: {},
    placeholder: '请填写邮件正文'
  }
  
  editorConfig.MENU_CONF['uploadImage'] = {
    base64LimitSize: 100 * 1024 * 1024 * 1024,
  }
  
  const mode = ref()
  
  const fileRef = ref();
  
  const ruleFormRef = ref()
  
  const ruleForm = reactive({
    nickName: '',
    sender: '',
    receivers: [],
    cc: [],
    bcc: [],
    subject: '',
    domains: [],
    pickedDomain: ""
  })
  
  const fileList = reactive([]);
  const contactOptions = ref([]);

  const loadContacts = function () {
    http.get("/api/v1/email/contacts").then(res => {
      if (res.errorNo === 0 && Array.isArray(res.data)) {
        contactOptions.value = res.data
      }
    }).catch(() => {})
  }

  const deleteContact = function (email) {
    if (!email) return
    http.post("/api/v1/email/contacts/delete", {email: email}).then(res => {
      if (res.errorNo === 0) {
        ElMessage.success("已从历史联系人中删除")
        contactOptions.value = contactOptions.value.filter(c => c.email !== email)
      } else {
        ElMessage.error(res.errorMsg || "删除失败")
      }
    }).catch(() => {
      ElMessage.error("删除联系人失败")
    })
  }
  
  const prepareIdentity = async function () {
    if (!globalStatus.userInfos?.account) await globalStatus.init()
    const info = globalStatus.userInfos
    if (!info?.account || !Array.isArray(info.domains) || !info.domains.length) {
      throw new Error(lang.draft_identity_failed)
    }
    return {sender: info.account, domains: info.domains, pickedDomain: info.domains[0], nickName: info.name || ''}
  }
  
  const validateSender = function (rule, value, callback) {
    if (typeof ruleForm.sender === "undefined" || ruleForm.sender === null || ruleForm.sender.trim() === "") {
      callback(new Error(lang.err_sender_must))
    } else if (ruleForm.sender.includes("@")) {
      callback(new Error(lang.only_prefix))
    } else {
      callback()
    }
  }
  
  const checkEmail = function (str) {
    const re = /.+@.+\..+/;
    return re.test(str);
  }
  
  const validateReceivers = function (rule, value, callback) {
    for (let index = 0; index < ruleForm.receivers.length; index++) {
      let element = ruleForm.receivers[index];
      if (!checkEmail(element)) {
        callback(new Error(lang.err_email_format))
        return
      }
    }
    callback()
  }
  
  const validateCc = function (rule, value, callback) {
    for (let index = 0; index < ruleForm.cc.length; index++) {
      let element = ruleForm.cc[index];
      if (!checkEmail(element)) {
        callback(new Error(lang.err_email_format))
        return
      }
    }
    callback()
  }
  
  const validateBcc = function (rule, value, callback) {
    for (let index = 0; index < ruleForm.bcc.length; index++) {
      let element = ruleForm.bcc[index];
      if (!checkEmail(element)) {
        callback(new Error(lang.err_email_format))
        return
      }
    }
    callback()
  }
  
  const rules = reactive({
    sender: [{validator: validateSender, trigger: 'change'}],
    receivers: [{validator: validateReceivers, trigger: 'change'}],
    cc: [{validator: validateCc, trigger: 'change'}],
    bcc: [{validator: validateBcc, trigger: 'change'}],
    subject: [{required: true, message: lang.err_title_must, trigger: 'change'}],
  })
  
  const editorRef = shallowRef()
  
  onBeforeUnmount(() => {
    const editor = editorRef.value
    if (editor == null) return
    editor.destroy()
  })
  
  const handleCreated = (editor) => {
    editorRef.value = editor
  }
  
  const isSending = ref(false)
  const pendingFiles = ref(0)
  const readers = new Set()
  const draft = useDraft({
    router, groupStore, ruleForm, valueHtml, fileList, editorRef,
    showCcBcc, isSending, pendingFiles, prepareIdentity
  })
  const init = () => {
    loadContacts()
    return draft.loadDraft()
  }
  init()
  onBeforeUnmount(() => {
    for (const reader of readers) reader.abort()
  })

  const send = function (formEl) {
    if (!formEl || draft.isBusy.value || !editorRef.value) return
    formEl.validate((valid) => {
      if (draft.isBusy.value) return
      if (valid) {
        if(ruleForm.receivers.length === 0 && ruleForm.cc.length === 0 && ruleForm.bcc.length === 0) {
          ElMessage.warning("请至少填写一个收件人");
          return;
        }
        const message = draft.buildMessage()
        isSending.value = true
  
        http.post("/api/v1/email/send", message).then(res => {
          if (res.errorNo === 0) {
            draft.markSent()
            ElMessage.success(lang.succ_send)
            groupStore.name = lang.outbox
            groupStore.tag = '{"type":1,"status":-1}'
            router.replace({name: 'list'})
          } else {
            ElMessage.error(res.errorMsg || res.data || lang.fail)
          }
        }).catch(() => {
          ElMessage.error(lang.fail)
        }).finally(() => {
          isSending.value = false
        })
      } else {
        return false
      }
    })
  }
  
  const upload = function () {
    if (draft.isBusy.value) return
    fileRef.value?.click()
  }
  
  const fileChange = function (e) {
    const files = Array.from(e.target.files || [])
    e.target.value = ''
    if (draft.isBusy.value || !files.length) return
    if (fileList.length + files.length > 20 || files.some(file => file.size > 10 * 1024 * 1024)) {
      ElMessage.warning(lang.draft_attachment_limit)
      return
    }
    for (const file of files) {
      const reader = new FileReader()
      readers.add(reader)
      pendingFiles.value += 1
      reader.onload = () => {
        const data = String(reader.result || '')
        fileList.push({
          name: file.name,
          data: data.startsWith('data:;')
            ? data.replace('data:;', 'data:application/octet-stream;')
            : data
        })
      }
      reader.onerror = () => ElMessage.error(lang.draft_attachment_failed)
      reader.onloadend = () => {
        readers.delete(reader)
        pendingFiles.value -= 1
      }
      try {
        reader.readAsDataURL(file)
      } catch {
        readers.delete(reader)
        pendingFiles.value -= 1
        ElMessage.error(lang.draft_attachment_failed)
      }
    }
  }
  
  const delFile = function (index) {
    if (draft.isBusy.value) return
    fileList.splice(index, 1);
  }

  return {
    ElMessage,
    onBeforeUnmount,
    reactive,
    ref,
    shallowRef,
    Close,
    Paperclip,
    Position,
    ArrowDown,
    Document,
    Delete,
    lang,
    Editor,
    Toolbar,
    useRouter,
    http,
    useGroupStore,
    useGlobalStatusStore,
    router,
    groupStore,
    globalStatus,
    showCcBcc,
    valueHtml,
    toolbarConfig,
    editorConfig,
    mode,
    fileRef,
    ruleFormRef,
    ruleForm,
    fileList,
    isSending,
    isBusy: draft.isBusy,
    isSaving: draft.isSaving,
    isLoading: draft.isLoading,
    loadFailed: draft.loadFailed,
    loadDraft: draft.loadDraft,
    saveDraft: draft.saveDraft,
    init,
    validateSender,
    checkEmail,
    validateReceivers,
    validateCc,
    validateBcc,
    rules,
    editorRef,
    handleCreated,
    send,
    upload,
    fileChange,
    delFile,
    contactOptions,
    loadContacts,
    deleteContact
  }
}

export {
  Paperclip,
  Position,
  ArrowDown,
  Toolbar,
  Editor,
  Document,
  Close,
  Delete
}
