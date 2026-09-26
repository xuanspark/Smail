import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessageBox, ElNotification } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { isRecentAuthenticationCancelled, requestWithRecentAuthentication } from '@/utils/recentAuthentication'

export function usePage(props = {}) {
  const globalStatus = useGlobalStatusStore()
  const manager = computed(() => globalStatus.hasCapability('notification.manage'))
  const loading = ref(false)
  const submitting = ref(false)
  const error = ref('')
  const status = ref(null)
  const accounts = ref([])
  const page = ref(1)
  const total = ref(0)
  const selectedID = ref(Number(props.userId || globalStatus.userInfos?.id || 0))
  const form = reactive({ enabled: false, minute_limit: 10, day_limit: 100 })
  const secret = ref('')
  const secretAccount = ref('')
  const secretVisible = ref(false)
  const seconds = ref(0)
  let secretTimer = null
  let requestSequence = 0
  let accountsSequence = 0
  let disposed = false

  const canManage = computed(() => manager.value && Boolean(status.value?.can_manage))
  const stateText = computed(() => {
    if (!status.value?.enabled) return '未开启'
    if (!status.value.account_available) return '账号不可用'
    return status.value.configured ? '已开启，可发信' : '已开启，待生成凭据'
  })
  const dirty = computed(() => status.value && (
    form.enabled !== status.value.enabled || form.minute_limit !== status.value.minute_limit || form.day_limit !== status.value.day_limit
  ))

  const dataOf = result => {
    if (result?.errorNo !== 0) throw new Error(result?.errorMsg || '操作失败')
    return result.data
  }
  const showError = cause => {
    if (isRecentAuthenticationCancelled(cause) || cause === 'cancel' || cause === 'close') return
    ElNotification({ title: '操作失败', message: cause?.message || '请稍后重试', type: 'error' })
  }
  const clearSecret = () => {
    if (secretTimer) clearInterval(secretTimer)
    secretTimer = null
    secret.value = ''
    secretAccount.value = ''
    seconds.value = 0
    secretVisible.value = false
  }

  const loadStatus = async () => {
    const sequence = ++requestSequence
    loading.value = true
    error.value = ''
    try {
      const data = dataOf(await http.get('/api/v1/system/notification/status', { params: { user_id: selectedID.value || undefined } }))
      if (disposed || sequence !== requestSequence) return
      status.value = data
      Object.assign(form, { enabled: data.enabled, minute_limit: data.minute_limit, day_limit: data.day_limit })
    } catch (cause) {
      if (disposed || sequence !== requestSequence) return
      status.value = null
      error.value = cause?.message || '读取通知发信状态失败'
    } finally {
      if (!disposed && sequence === requestSequence) loading.value = false
    }
  }
  const loadAccounts = async () => {
    if (!manager.value || props.userId) return
    const sequence = ++accountsSequence
    try {
      const data = dataOf(await http.get('/api/v1/system/notification/accounts', { params: { page: page.value } }))
      if (disposed || sequence !== accountsSequence) return
      accounts.value = data.list || []
      total.value = data.total || 0
    } catch (cause) {
      if (!disposed && sequence === accountsSequence) {
        accounts.value = []
        showError(cause)
      }
    }
  }
  const beforeLeave = async () => {
    if (submitting.value) return false
    if (!dirty.value) return true
    try {
      await ElMessageBox.confirm('通知授权尚未保存，是否放弃本次修改？', '离开确认', {
        confirmButtonText: '放弃修改', cancelButtonText: '继续编辑', type: 'warning'
      })
      return true
    } catch { return false }
  }
  const selectAccount = async row => {
    if (loading.value || !(await beforeLeave())) return
    clearSecret()
    status.value = null
    selectedID.value = row.user_id
    await loadStatus()
  }
  const refresh = async () => {
    if (!(await beforeLeave())) return
    await Promise.all([loadStatus(), loadAccounts()])
  }
  const save = async () => {
    if (!canManage.value || !status.value || submitting.value) return
    const target = status.value
    const input = { user_id: target.user_id, version: target.version, ...form }
    if (input.day_limit < input.minute_limit) {
      showError(new Error('每日限额不能小于每分钟限额'))
      return
    }
    try {
      await ElMessageBox.confirm(
        input.enabled ? `保存账号「${target.account}」的通知发信授权与限额？` : `关闭账号「${target.account}」的通知发信后，旧凭据将永久撤销；普通收发信不受影响。`,
        '确认通知授权', { confirmButtonText: '确认保存', cancelButtonText: '取消', type: 'warning' }
      )
      submitting.value = true
      dataOf(await requestWithRecentAuthentication(() => http.post('/api/v1/system/notification/authorization', input)))
      clearSecret()
      ElNotification({ title: '已保存', message: '账号通知授权已更新', type: 'success' })
      await Promise.all([loadStatus(), loadAccounts()])
    } catch (cause) {
      showError(cause)
    } finally { submitting.value = false }
  }
  const rotate = async () => {
    if (!canManage.value || !status.value?.enabled || submitting.value || dirty.value) return
    const target = status.value
    try {
      await ElMessageBox.confirm(`为「${target.account}」生成新凭据？旧通知凭据和旧连接将失效，普通邮箱凭据不变。`, '确认重置凭据', {
        confirmButtonText: '生成新凭据', cancelButtonText: '取消', type: 'warning'
      })
      submitting.value = true
      const data = dataOf(await requestWithRecentAuthentication(() => http.post('/api/v1/system/notification/credential', {
        user_id: target.user_id, version: target.version
      })))
      if (disposed) return
      clearSecret()
      secret.value = data.code
      secretAccount.value = target.account
      seconds.value = 60
      secretVisible.value = true
      secretTimer = setInterval(() => {
        seconds.value -= 1
        if (seconds.value <= 0) clearSecret()
      }, 1000)
      await Promise.all([loadStatus(), loadAccounts()])
    } catch (cause) { showError(cause) }
    finally { submitting.value = false }
  }
  const copySecret = async () => {
    if (!secret.value) return
    try {
      await navigator.clipboard.writeText(secret.value)
      ElNotification({ title: '已复制', message: '请安全交付给账号使用者，不要放入日志或代码仓库', type: 'success' })
    } catch { showError(new Error('复制失败，请手动复制授权码')) }
  }

  onMounted(() => { loadStatus(); loadAccounts() })
  onUnmounted(() => { disposed = true; requestSequence += 1; accountsSequence += 1; clearSecret() })

  return {
    manager, loading, submitting, error, status, accounts, page, total, form,
    secret, secretAccount, secretVisible, seconds, canManage, stateText, dirty,
    loadAccounts, selectAccount, refresh, save, rotate, clearSecret, copySecret, beforeLeave
  }
}
