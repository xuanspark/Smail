import { computed, onBeforeUnmount, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/axios'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'

export function useAccountGroups(globalStatus, onMembershipSaved = () => {}) {
  const accountGroups = ref([])
  const groupsLoading = ref(false)
  const groupsError = ref('')
  const canManageGroups = computed(() => globalStatus.hasCapability('account_group.manage'))
  const groupSaving = ref(false)
  const membershipVisible = ref(false)
  const membershipUser = ref(null)
  const membershipSelection = ref([])
  const membershipDraft = ref(false)
  let applyMembershipDraft = null
  const membershipLimit = computed(() => membershipUser.value?.Role === 'admin' ? 100 : 1)
  const membershipSingleSelection = computed({
    get: () => membershipSelection.value.length === 1 ? membershipSelection.value[0] : null,
    set: value => { membershipSelection.value = [value] }
  })
  let generation = 0
  let alive = true
  onBeforeUnmount(() => { alive = false; generation++ })

  const loadGroup = async group => {
    const requestID = ++group.requestID
    group.loading = true
    group.error = ''
    // 切页先清空旧成员，失败时不能继续显示上一页数据。
    group.rows = []
    try {
      const response = await http.post('/api/v1/user/list', {
        current_page: group.page,
        page_size: 6,
        group_id: group.id
      })
      if (!alive || requestID !== group.requestID || !accountGroups.value.includes(group)) return
      if (response?.errorNo !== 0) throw new Error(response?.errorMsg || '分组成员加载失败')
      group.totalPages = Math.max(1, Number(response.data.total_page) || 1)
      if (group.page > group.totalPages) {
        group.page = group.totalPages
        return await loadGroup(group)
      }
      group.rows = response.data.list || []
    } catch (error) {
      if (alive && requestID === group.requestID) group.error = error?.message || '分组成员加载失败'
    } finally {
      if (requestID === group.requestID) group.loading = false
    }
  }

  const refreshGroups = async () => {
    const requestID = ++generation
    groupsLoading.value = true
    groupsError.value = ''
    if (!['admin', 'super_admin'].includes(globalStatus.role)) {
      accountGroups.value = []
      groupsLoading.value = false
      return
    }
    const previous = new Map(accountGroups.value.map(group => [group.id, { page: group.page, expanded: group.expanded }]))
    accountGroups.value = []
    try {
      const response = await http.get('/api/v1/user/groups')
      if (!alive || requestID !== generation) return
      if (response?.errorNo !== 0) throw new Error(response?.errorMsg || '账号分组加载失败')
      accountGroups.value = (response.data.groups || []).map(group => ({
        ...group,
        expanded: group.id === 0 || previous.get(group.id)?.expanded || false,
        page: previous.get(group.id)?.page || 1,
        totalPages: 1,
        rows: [],
        error: '',
        loading: false,
        requestID: 0
      }))
      await Promise.all(accountGroups.value.filter(group => group.expanded).map(loadGroup))
    } catch (error) {
      if (alive && requestID === generation) groupsError.value = error?.message || '账号分组加载失败'
    } finally {
      if (requestID === generation) groupsLoading.value = false
    }
  }

  const toggleGroup = group => {
    if (group.id === 0) return
    group.expanded = !group.expanded
    if (group.expanded) loadGroup(group)
  }

  const saveGroup = async (group = null) => {
    if (!canManageGroups.value || groupSaving.value || group?.id === 0) return
    groupSaving.value = true
    try {
      const { value } = await ElMessageBox.prompt('请输入分组名称（1–32 个字符）', group ? '重命名账号分组' : '新建账号分组', {
        inputValue: group?.name || '',
        confirmButtonText: '保存',
        cancelButtonText: '取消',
        inputValidator: value => {
          const name = (value || '').trim()
          return (name.length > 0 && [...name].length <= 32 && name !== '默认分组') || '名称不能为空、过长或使用“默认分组”'
        }
      })
      const response = await requestWithRecentAuthentication(() => http.post('/api/v1/user/groups/save', { id: group?.id || 0, name: value.trim() }))
      if (response?.errorNo !== 0) throw new Error(response?.errorMsg || '保存分组失败')
      ElMessage.success('账号分组已保存')
      await refreshGroups()
      return response.data || null
    } catch (error) {
      if (error !== 'cancel' && error !== 'close' && !isRecentAuthenticationCancelled(error)) ElMessage.error(error?.message || '保存分组失败')
    } finally {
      groupSaving.value = false
    }
  }

  const createMembershipGroup = async () => {
    if (!membershipVisible.value || !canManageGroups.value || groupSaving.value) return
    const group = await saveGroup()
    if (!group?.id) return
    if (membershipLimit.value === 1) {
      membershipSelection.value = [group.id]
      return
    }
    const selectedGroups = membershipSelection.value.filter(id => id !== 0)
    if (!selectedGroups.includes(group.id)) selectedGroups.push(group.id)
    membershipSelection.value = selectedGroups
  }

  const editMembership = (user, options = {}) => {
    if (!canManageGroups.value || groupSaving.value) return
    membershipUser.value = { ...user, Role: options.role || user.Role }
    membershipSelection.value = [...(options.groupIDs || user.GroupIDs || [0])]
    applyMembershipDraft = options.onApply || null
    membershipDraft.value = Boolean(applyMembershipDraft)
    membershipVisible.value = true
  }

  const saveMembership = async () => {
    if (!canManageGroups.value || groupSaving.value || !membershipUser.value) return
    if (membershipSelection.value.length === 0) membershipSelection.value = [0]
    if (membershipSelection.value.length > membershipLimit.value) {
      ElMessage.error('普通用户只能选择一个分组，请重新选择')
      return
    }
    if (applyMembershipDraft) {
      applyMembershipDraft([...membershipSelection.value])
      membershipVisible.value = false
      return
    }
    groupSaving.value = true
    try {
      const response = await requestWithRecentAuthentication(() => http.post('/api/v1/user/groups/membership', {
        user_id: membershipUser.value.ID,
        version: membershipUser.value.MembershipVersion,
        group_ids: [...membershipSelection.value]
      }))
      if (response?.errorNo !== 0) throw new Error(response?.errorMsg || '保存成员分组失败')
      const updatedUser = {
        ...membershipUser.value,
        GroupIDs: [...membershipSelection.value],
        MembershipVersion: response.data.membership_version
      }
      membershipUser.value = updatedUser
      onMembershipSaved(updatedUser)
      membershipVisible.value = false
      ElMessage.success('成员分组已更新，目标账号需重新登录')
      await refreshGroups()
    } catch (error) {
      if (!isRecentAuthenticationCancelled(error)) ElMessage.error(error?.message || '保存成员分组失败，请刷新后重试')
    } finally {
      groupSaving.value = false
    }
  }

  return {
    accountGroups, groupsLoading, groupsError, canManageGroups, groupSaving,
    membershipVisible, membershipUser, membershipSelection, membershipLimit,
    membershipDraft, membershipSingleSelection,
    refreshGroups, loadGroup, toggleGroup, saveGroup, createMembershipGroup, editMembership, saveMembership
  }
}
