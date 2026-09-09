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
    if (!name.value.trim()) return ElMessage.error('请输入显示名称')
    saving.value = true
    try {
      const result = await http.post('/api/v1/user/profile', { name: name.value.trim() })
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
