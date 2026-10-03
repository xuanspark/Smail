import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { roleMeta } from '@/utils/userRoles.mjs'

export function useProfile() {
  const store = useGlobalStatusStore()
  const name = ref(store.userInfos.name || '')
  const saving = ref(false)
  async function save() {
    if (saving.value) return
    const displayName = name.value.trim()
    if (!displayName || Array.from(displayName).length > 10 || /[\u0000-\u001f\u007f-\u009f]/u.test(displayName)) {
      return ElMessage.error('显示名称须为 1–10 个字符，且不能包含控制字符')
    }
    saving.value = true
    try {
      const result = await http.post('/api/v1/user/profile', { name: displayName })
      if (result?.errorNo !== 0) throw new Error(result?.errorMsg || '个人资料保存失败')
      store.userInfos.name = result.data.name
      name.value = result.data.name
      ElMessage.success('个人资料已保存')
    } catch (error) {
      ElMessage.error(error.message || '个人资料保存失败')
    } finally {
      saving.value = false
    }
  }
  return { store, name, saving, save, roleMeta }
}
