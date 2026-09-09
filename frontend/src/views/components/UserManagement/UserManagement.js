import {computed, reactive, ref} from 'vue'

import lang from '../../../i18n/i18n';

import {http} from "@/utils/axios";

import {ElNotification} from "element-plus";

import {Plus} from "@element-plus/icons-vue";

import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";
import {confirmRecentAuthentication, isRecentAuthenticationCancelled, requestWithRecentAuthentication} from '@/utils/recentAuthentication'
import {roleMeta, assignableRoles, canManageUser} from '@/utils/userRoles.mjs'

export function recoveryCodeMeta(configured) {
  return configured
    ? { label: '已开启', type: 'success' }
    : { label: '未开启', type: 'info' }
}

export function externalEmailMeta(enabled, configured) {
  if (!enabled) {
    return { label: '已关闭', type: 'warning' }
  }
  return configured
    ? { label: '已绑定', type: 'success' }
    : { label: '未绑定', type: 'info' }
}

export function accountStatusMeta(disabled) {
  return disabled === 1
    ? { label: '已禁用', type: 'danger' }
    : { label: '正常', type: 'success' }
}

export function isSameUser(selectedUser, currentUser) {
  const selectedID = Number(selectedUser?.ID || 0)
  const currentID = Number(currentUser?.id || 0)
  if (selectedID > 0 && currentID > 0) {
    return selectedID === currentID
  }
  return Boolean(selectedUser?.Account && currentUser?.account)
    && selectedUser.Account === currentUser.account
}

export function canManageRecoveryCode(user) {
  return Boolean(user?.ID) && user.Role !== 'super_admin'
}

export function usePage() {
  const globalStatus = useGlobalStatusStore()

  const userList = reactive([])
  
  const currentPage = ref(1)
  
  const totalPage = ref(1)
  
  const userInfoDialog = ref(false)
  
  const editModel = ref("edit")

  const selectedUser = ref(null)

  const recoveryCodeDialogVisible = ref(false)

  const externalEmailDialogVisible = ref(false)

  const securityDialogOpening = ref('')

  const submitting = ref(false)

  const availableRoles = computed(() => assignableRoles(globalStatus.role))

  const canCreateUser = computed(() => globalStatus.hasCapability('user.create'))

  const canEditSelectedUser = computed(() => editModel.value === 'create'
    ? canCreateUser.value && availableRoles.value.length > 0
    : canManageUser(globalStatus.role, selectedUser.value?.Role))

  const canSelectRole = computed(() => canEditSelectedUser.value
    && (editModel.value === 'create' || globalStatus.hasCapability('role.manage')))

  const editingCurrentUser = computed(() => isSameUser(selectedUser.value, globalStatus.userInfos))

  const recoveryCodeWebManaged = computed(() => canManageRecoveryCode(selectedUser.value))
  
  const editUserInfo = reactive({
    "account": "",
    "name": "",
    "password": "",
    "disabled": false,
    "role": "user"
  })

  const roleChanged = computed(() => editModel.value === 'edit'
    && editUserInfo.role !== selectedUser.value?.Role)
  
  const title = ref(lang.editUser)
  
  const reflushList = function () {
    http.post('/api/v1/user/list', {"current_page": currentPage.value, "page_size": 6}).then(res => {
      userList.length = 0
      totalPage.value = res.data.total_page
      if (res.data["list"]) {
        userList.push(...res.data["list"])
        if (selectedUser.value?.ID) {
          selectedUser.value = userList.find(item => item.ID === selectedUser.value.ID) || selectedUser.value
        }
      }
    })
  }
  
  const handleEdit = function (idx, row) {
    selectedUser.value = row
    editUserInfo.account = row.Account
    editUserInfo.name = row.Name
    editUserInfo.disabled = row.Disabled === 1
    editUserInfo.password = ""
    editUserInfo.role = row.Role
    editModel.value = "edit"
    title.value = lang.editUser
    userInfoDialog.value = true
  }
  
  const createUser = function () {
    if (!canCreateUser.value) return
    selectedUser.value = null
    editUserInfo.account = ""
    editUserInfo.name = ""
    editUserInfo.disabled = false
    editUserInfo.password = ""
    editUserInfo.role = "user"
    editModel.value = "create"
    title.value = lang.newUser
    userInfoDialog.value = true
  }

  const confirmSecurityDialogAccess = async function (dialogName) {
    if (securityDialogOpening.value) return false
    securityDialogOpening.value = dialogName
    try {
      await confirmRecentAuthentication()
      return true
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        ElNotification({
          title: lang.fail,
          message: error?.message || '身份确认失败',
          type: 'error',
        })
      }
      return false
    } finally {
      securityDialogOpening.value = ''
    }
  }

  const openRecoveryCodeDialog = async function () {
    if (!recoveryCodeWebManaged.value) return
    if (!await confirmSecurityDialogAccess('recovery-code')) return
    recoveryCodeDialogVisible.value = true
  }

  const openExternalEmailDialog = async function () {
    if (!selectedUser.value?.ID) return
    if (!await confirmSecurityDialogAccess('external-email')) return
    externalEmailDialogVisible.value = true
  }

  const handleSecurityChanged = function () {
    reflushList()
  }
  
  const submit = async function () {
    if (submitting.value || !canEditSelectedUser.value) return
    submitting.value = true
    try {
      const creating = editModel.value === 'create'
      const newData = {
        "account": editUserInfo.account,
        "username": editUserInfo.name,
        "disabled": editUserInfo.disabled ? 1 : 0
      }
      if (creating || roleChanged.value) newData.role = editUserInfo.role
      if (creating || editUserInfo.password !== '') newData.password = editUserInfo.password
      const res = await requestWithRecentAuthentication(() => creating
        ? http.post('/api/v1/user/create', newData)
        : http.post('/api/v1/user/edit', newData))
      if (!res || res.errorNo !== 0) {
        throw new Error(res?.errorMsg || '保存用户失败')
      }
      ElNotification({ title: lang.succ, message: '用户信息已保存', type: 'success' })
      editUserInfo.password = ''
      reflushList()
      userInfoDialog.value = false
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) {
        ElNotification({
          title: lang.fail,
          message: error?.message || '保存用户失败，请稍后重试',
          type: 'error'
        })
      }
    } finally {
      submitting.value = false
    }
  }
  
  reflushList()

  return {
    reactive,
    ref,
    lang,
    http,
    ElNotification,
    Plus,
    userList,
    currentPage,
    totalPage,
    userInfoDialog,
    editModel,
    editUserInfo,
    availableRoles,
    canCreateUser,
    canEditSelectedUser,
    canSelectRole,
    roleChanged,
    submitting,
    selectedUser,
    editingCurrentUser,
    recoveryCodeWebManaged,
    recoveryCodeDialogVisible,
    externalEmailDialogVisible,
    securityDialogOpening,
    openRecoveryCodeDialog,
    openExternalEmailDialog,
    handleSecurityChanged,
    title,
    reflushList,
    handleEdit,
    createUser,
    submit,
    roleMeta,
    recoveryCodeMeta,
    externalEmailMeta,
    accountStatusMeta
  }
}

export {
  Plus,
  roleMeta
}
