import {computed, reactive, ref} from 'vue'

import lang from '../../../i18n/i18n';

import {http} from "@/utils/axios";

import {ElNotification} from "element-plus";

import {Plus} from "@element-plus/icons-vue";

import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";
import {confirmRecentAuthentication, isRecentAuthenticationCancelled, requestWithRecentAuthentication} from '@/utils/recentAuthentication'
import {roleMeta, assignableRoles, canManageUser} from '@/utils/userRoles.mjs'
import {useAccountGroups} from './AccountGroups.js'

export function recoveryCodeMeta(configured) {
  return configured
    ? { label: '已开启', type: 'success' }
    : { label: '未开启', type: 'info' }
}

export function externalEmailMeta(configured) {
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

  const groups = useAccountGroups(globalStatus, user => {
    if (selectedUser.value?.ID === user.ID) selectedUser.value = user
  })
  
  const userInfoDialog = ref(false)
  
  const editModel = ref("edit")

  const selectedUser = ref(null)
  const editingGroupIDs = ref([0])
  const editingMembershipVersion = ref(0)
  const membershipChanged = computed(() => {
    const saved = selectedUser.value?.GroupIDs || [0]
    return saved.length !== editingGroupIDs.value.length
      || saved.some(id => !editingGroupIDs.value.includes(id))
  })

  const selectedMembershipGroups = computed(() => {
    const user = selectedUser.value
    if (!user && editModel.value !== 'create') return []
    return groups.accountGroups.value.filter(group =>
      groups.canManageMembership.value
        ? editingGroupIDs.value.includes(group.id)
        : group.rows.some(member => member.ID === user.ID)
    )
  })

  const openMembershipDialog = () => {
    if (submitting.value || (!selectedUser.value && editModel.value !== 'create')) return
    const membershipUser = selectedUser.value || {
      Account: editUserInfo.account,
      Role: editUserInfo.role
    }
    groups.editMembership(membershipUser, {
      role: editUserInfo.role,
      groupIDs: editingGroupIDs.value,
      onApply: ids => { editingGroupIDs.value = ids }
    })
  }

  const recoveryCodeDialogVisible = ref(false)

  const externalEmailDialogVisible = ref(false)

  const notificationDialogVisible = ref(false)
  const notificationPanel = ref(null)
  const canManageNotification = computed(() => globalStatus.hasCapability('notification.manage'))
  const closeNotificationDialog = async done => {
    if (await notificationPanel.value?.beforeLeave()) done()
  }

  const securityDialogOpening = ref('')

  const submitting = ref(false)

  const availableRoles = computed(() => assignableRoles(globalStatus.role))

  const canCreateUser = computed(() => globalStatus.hasCapability('user.create'))

  const canEditUser = user => canManageUser(globalStatus.role, user?.Role)

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
  
  const reflushList = async function () {
    const selectedID = selectedUser.value?.ID
    await groups.refreshGroups()
    if (selectedID && selectedUser.value?.ID === selectedID) {
      const users = groups.accountGroups.value.flatMap(group => group.rows)
      let current = users.find(item => item.ID === selectedID)
      if (!current) {
        const response = await http.post('/api/v1/user/list', { user_id: selectedID, page_size: 1 })
        if (response?.errorNo !== 0) throw new Error(response?.errorMsg || lang.fail)
        current = response.data?.list?.find(item => item.ID === selectedID)
      }
      if (selectedUser.value?.ID !== selectedID) return
      if (!current) {
        selectedUser.value = null
        userInfoDialog.value = false
        return
      }
      const savedGroups = selectedUser.value.GroupIDs || [0]
      const currentGroups = current.GroupIDs || [0]
      if (current.Role === selectedUser.value.Role
        && currentGroups.length === savedGroups.length
        && savedGroups.every(id => currentGroups.includes(id))) {
        editingMembershipVersion.value = current.MembershipVersion
      }
      selectedUser.value = current
    }
  }
  
  const handleEdit = function (idx, row) {
    if (!canEditUser(row)) return
    selectedUser.value = row
    editingGroupIDs.value = [...(row.GroupIDs || [0])]
    editingMembershipVersion.value = row.MembershipVersion
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
    editingGroupIDs.value = [0]
    editingMembershipVersion.value = 0
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

  const handleSecurityChanged = async function () {
    try {
      await reflushList()
    } catch (error) {
      ElNotification({ title: lang.fail, message: error?.message || lang.fail, type: 'error' })
    }
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
      if (groups.canManageMembership.value && (creating || membershipChanged.value || roleChanged.value)) {
        if (editUserInfo.role !== 'admin' && editingGroupIDs.value.length !== 1) {
          throw new Error('普通用户只能属于一个分组，请在分组管理中选择一个分组')
        }
        newData.group_ids = [...editingGroupIDs.value]
        if (!creating) newData.membership_version = editingMembershipVersion.value
      }
      const res = await requestWithRecentAuthentication(() => creating
        ? http.post('/api/v1/user/create', newData)
        : http.post('/api/v1/user/edit', newData))
      if (!res || res.errorNo !== 0) {
        throw new Error(res?.errorMsg || '保存用户失败')
      }
      ElNotification({ title: lang.succ, message: '用户信息已保存', type: 'success' })
      editUserInfo.password = ''
      if (creating) {
        handleEdit(0, res.data)
      } else {
        userInfoDialog.value = false
      }
      await reflushList()
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
    ...groups,
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
    selectedMembershipGroups,
    openMembershipDialog,
    editingCurrentUser,
    recoveryCodeWebManaged,
    recoveryCodeDialogVisible,
    externalEmailDialogVisible,
    notificationDialogVisible,
    notificationPanel,
    canManageNotification,
    closeNotificationDialog,
    securityDialogOpening,
    openRecoveryCodeDialog,
    openExternalEmailDialog,
    handleSecurityChanged,
    title,
    reflushList,
    handleEdit,
    canEditUser,
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
