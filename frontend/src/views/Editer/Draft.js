import {computed, nextTick, onBeforeUnmount, ref, watch} from 'vue'
import {onBeforeRouteLeave, onBeforeRouteUpdate, useRoute} from 'vue-router'
import {ElMessage, ElMessageBox} from 'element-plus'
import {http} from '@/utils/axios'
import lang from '../../i18n/i18n'
import {prepareReply} from './Reply'

export function useDraft({router, groupStore, ruleForm, ruleFormRef, valueHtml, fileList, editorRef, showCcBcc, isSending, pendingFiles, prepareIdentity}) {
  const route = useRoute()
  const draftID = ref(0)
  const draftVersion = ref('')
  const isSaving = ref(false)
  const isLoading = ref(true)
  const loadFailed = ref(false)
  const sent = ref(false)
  const replyTo = ref([])
  const replyEmailID = ref(0)
  let envelopeSender = null
  let originalFrom = ''
  let recipientNames = new Map()
  const isBusy = computed(() => sent.value || isSaving.value || isSending.value || isLoading.value || loadFailed.value || pendingFiles.value > 0)
  let savedSnapshot = ''
  let loadSequence = 0
  let disposed = false

  const buildMessage = () => ({
    draft_id: draftID.value,
    draft_version: draftVersion.value,
    reply_email_id: replyEmailID.value,
    from: {name: ruleForm.nickName, email: `${ruleForm.sender}@${ruleForm.pickedDomain}`},
    to: ruleForm.receivers.map(email => ({name: recipientNames.get(email) || '', email})),
    cc: ruleForm.cc.map(email => ({name: recipientNames.get(email) || '', email})),
    bcc: ruleForm.bcc.map(email => ({name: recipientNames.get(email) || '', email})),
    sender: envelopeSender?.email && envelopeSender.email !== originalFrom ? envelopeSender : undefined,
    reply_to: replyTo.value,
    subject: ruleForm.subject,
    text: editorRef.value?.getText() || '',
    html: valueHtml.value,
    attrs: fileList.map(item => ({name: item.name, data: item.data}))
  })

  const snapshot = () => JSON.stringify({
    replyEmailID: replyEmailID.value,
    from: [ruleForm.nickName, ruleForm.sender, ruleForm.pickedDomain],
    to: ruleForm.receivers, cc: ruleForm.cc, bcc: ruleForm.bcc,
    subject: ruleForm.subject, html: valueHtml.value, attrs: fileList
  })
  const isDirty = () => !sent.value && savedSnapshot !== '' && snapshot() !== savedSnapshot

  const resetMessage = () => {
    draftID.value = 0
    draftVersion.value = ''
    replyTo.value = []
    replyEmailID.value = 0
    envelopeSender = null
    originalFrom = ''
    recipientNames = new Map()
    ruleForm.receivers = []
    ruleForm.cc = []
    ruleForm.bcc = []
    ruleForm.subject = ''
    valueHtml.value = '<p><br></p>'
    fileList.splice(0)
    showCcBcc.value = false
    sent.value = false
    savedSnapshot = ''
  }

  const loadDraft = async () => {
    const sequence = ++loadSequence
    isLoading.value = true
    loadFailed.value = false
    resetMessage()
    const rawID = route.query.draft
    const rawReplyID = route.query.reply
    try {
      if (rawID !== undefined && rawReplyID !== undefined) throw new Error(lang.fail)
      const identity = await prepareIdentity()
      if (disposed || sequence !== loadSequence) return
      Object.assign(ruleForm, identity)
      if (rawID === undefined) {
        if (rawReplyID !== undefined) {
          if (typeof rawReplyID !== 'string' || !/^[1-9]\d*$/.test(rawReplyID)) throw new Error(lang.fail)
          const id = Number(rawReplyID)
          if (!Number.isSafeInteger(id)) throw new Error(lang.fail)
          const result = await http.post('/api/v1/email/detail', {id})
          if (disposed || sequence !== loadSequence) return
          if (result.errorNo !== 0 || Number(result.data?.id) !== id) throw new Error(result.errorMsg || lang.fail)
          const reply = prepareReply(result.data, identity)
          replyEmailID.value = id
          ruleForm.receivers = reply.recipients
          ruleForm.pickedDomain = reply.pickedDomain
          ruleForm.subject = reply.subject
          recipientNames = reply.names
          valueHtml.value = reply.html
        }
        return
      }
      if (typeof rawID !== 'string' || !/^[1-9]\d*$/.test(rawID)) throw new Error(lang.draft_load_failed)
      const id = Number(rawID)
      if (!Number.isSafeInteger(id) || id <= 0) throw new Error(lang.draft_load_failed)
      const result = await http.post('/api/v1/email/draft/detail', {id})
      if (disposed || sequence !== loadSequence) return
      if (result.errorNo !== 0) throw new Error(result.errorMsg || lang.draft_load_failed)
      const data = result.data
      const message = data?.message
      if (data?.id !== id || !data.version || !message?.from?.email) throw new Error(lang.draft_load_failed)
      const split = message.from.email.lastIndexOf('@')
      if (split <= 0) throw new Error(lang.draft_load_failed)
      draftID.value = data.id
      draftVersion.value = data.version
      ruleForm.sender = message.from.email.slice(0, split)
      ruleForm.pickedDomain = message.from.email.slice(split + 1)
      ruleForm.nickName = message.from.name || ''
      originalFrom = message.from.email
      envelopeSender = message.sender || null
      recipientNames = new Map(
        [...(message.to || []), ...(message.cc || []), ...(message.bcc || [])]
          .map(item => [item.email, item.name || ''])
      )
      ruleForm.receivers = (message.to || []).map(item => item.email)
      ruleForm.cc = (message.cc || []).map(item => item.email)
      ruleForm.bcc = (message.bcc || []).map(item => item.email)
      replyTo.value = message.reply_to || []
      replyEmailID.value = message.reply_email_id || 0
      ruleForm.subject = message.subject || ''
      const plainText = (message.text || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\r?\n/g, '<br>')
      valueHtml.value = message.html || (plainText ? `<p>${plainText}</p>` : '<p><br></p>')
      fileList.splice(0, fileList.length, ...(message.attrs || []))
      showCcBcc.value = ruleForm.cc.length > 0 || ruleForm.bcc.length > 0
      sent.value = false
    } catch (error) {
      if (disposed || sequence !== loadSequence) return
      resetMessage()
      loadFailed.value = true
      ElMessage.error(error.message || lang.draft_load_failed)
    } finally {
      await nextTick()
      if (!disposed && sequence === loadSequence) {
        ruleFormRef.value?.clearValidate()
        if (!loadFailed.value) savedSnapshot = snapshot()
        isLoading.value = false
      }
    }
  }

  const saveDraft = async () => {
    if (isBusy.value || !editorRef.value) return
    isSaving.value = true
    const submittedSnapshot = snapshot()
    try {
      const result = await http.post('/api/v1/email/draft/save', buildMessage())
      if (result.errorNo !== 0) throw new Error(result.errorMsg || lang.fail)
      if (!Number.isSafeInteger(result.data?.id) || result.data.id <= 0 || !result.data.version) throw new Error(lang.fail)
      draftID.value = result.data.id
      draftVersion.value = result.data.version
      savedSnapshot = submittedSnapshot
      groupStore.invalidate()
      // 将已保存草稿写入地址，刷新后仍可恢复；先释放保存锁再更新路由。
      isSaving.value = false
      await router.replace({name: 'editer', query: {draft: String(draftID.value)}})
      ElMessage.success(lang.draft_saved)
    } catch (error) {
      ElMessage.error(error.message || lang.fail)
    } finally {
      isSaving.value = false
    }
  }

  const confirmLeave = async () => {
    if (sent.value) return true
    if (isSaving.value || isSending.value || pendingFiles.value > 0) return false
    if (!isDirty()) return true
    try {
      await ElMessageBox.confirm(lang.draft_leave_warning, lang.draft_title, {
        confirmButtonText: lang.draft_leave, cancelButtonText: lang.draft_stay, type: 'warning'
      })
      return true
    } catch {
      return false
    }
  }
  onBeforeRouteLeave(confirmLeave)
  onBeforeRouteUpdate((to, from) =>
    to.query.draft === from.query.draft && to.query.reply === from.query.reply ? true : confirmLeave()
  )
  watch(() => [route.query.draft, route.query.reply], ([draft, reply]) => {
    if (!loadFailed.value && !isLoading.value && reply === undefined &&
      draftID.value > 0 && draft === String(draftID.value)) return
    loadDraft()
  })
  watch([editorRef, isBusy], ([editor, busy]) => {
    if (!editor || disposed) return
    if (busy) editor.disable()
    else editor.enable()
  }, {flush: 'post'})
  const beforeUnload = event => {
    if (isDirty() || isSaving.value || isSending.value || pendingFiles.value > 0) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  window.addEventListener('beforeunload', beforeUnload)
  onBeforeUnmount(() => {
    disposed = true
    loadSequence += 1
    window.removeEventListener('beforeunload', beforeUnload)
  })
  const markSent = () => {
    sent.value = true
    groupStore.invalidate()
  }
  return {draftID, draftVersion, isSaving, isLoading, isBusy, loadFailed, loadDraft, saveDraft, buildMessage, markSent}
}
