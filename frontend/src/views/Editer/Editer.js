import './Editer.js'

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
  
  const init = function () {
      loadContacts()
      if (globalStatus.userInfos === null || globalStatus.userInfos == undefined || Object.keys(globalStatus.userInfos).length == 0){
        globalStatus.init(()=>{
          ruleForm.sender = globalStatus.userInfos.account
          ruleForm.domains = globalStatus.userInfos.domains
          ruleForm.pickedDomain = globalStatus.userInfos.domains[0]
          ruleForm.nickName = globalStatus.userInfos.name
        })
      }else{
        ruleForm.sender = globalStatus.userInfos.account
        ruleForm.domains = globalStatus.userInfos.domains
        ruleForm.pickedDomain = globalStatus.userInfos.domains[0]
        ruleForm.nickName = globalStatus.userInfos.name
      }
  }
  
  init()
  
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

  const send = function (formEl) {
    if (!formEl || isSending.value) return
    formEl.validate((valid) => {
      if (valid) {
        if(ruleForm.receivers.length === 0 && ruleForm.cc.length === 0 && ruleForm.bcc.length === 0) {
          ElMessage.warning("请至少填写一个收件人");
          return;
        }
        let objectTos = ruleForm.receivers.map(e => ({name: "", email: e}));
        let objectCcs = ruleForm.cc.map(e => ({name: "", email: e}));
        let objectBccs = ruleForm.bcc.map(e => ({name: "", email: e}));
  
        if (!editorRef.value) return
        const text = editorRef.value.getText()
        isSending.value = true
  
        http.post("/api/v1/email/send", {
          from: {name: ruleForm.nickName, email: ruleForm.sender + "@" + ruleForm.pickedDomain},
          to: objectTos,
          cc: objectCcs,
          bcc: objectBccs,
          subject: ruleForm.subject,
          text: text,
          html: valueHtml.value,
          attrs: fileList
        }).then(res => {
          if (res.errorNo === 0) {
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
    fileRef.value.dispatchEvent(new MouseEvent('click'))
  }
  
  const fileChange = function (e) {
    let files = e.target.files || e.dataTransfer.files;
    if (!files.length) return;
    for (let i = 0; i < files.length; i++) {
      const reader = new FileReader();
      reader.onload = function fileReadCompleted() {
        fileList.push({
          name: files[i].name,
          data: this.result
        })
      };
      reader.readAsDataURL(files[i]);
    }
  }
  
  const delFile = function (index) {
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
