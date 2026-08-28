import { reactive, ref } from "vue"

import { ElMessage } from "element-plus"

import { useRouter } from "vue-router"

import { http } from "@/utils/axios"

import { useGlobalStatusStore } from "@/stores/useGlobalStatusStore"

export function usePage() {
  const router = useRouter()
  const globalStatus = useGlobalStatusStore()
  const loading = ref(false)
  const form = reactive({
    currentPassword: "",
    newPassword: "",
    confirmPassword: ""
  })

  const submit = async () => {
    if (!form.currentPassword || !form.newPassword || !form.confirmPassword) {
      ElMessage.warning("请完整填写密码信息")
      return
    }
    if (form.newPassword !== form.confirmPassword) {
      ElMessage.warning("两次输入的新密码不一致")
      return
    }
    if (new TextEncoder().encode(form.newPassword).length < 8) {
      ElMessage.warning("新密码长度至少为 8 个字节")
      return
    }

    loading.value = true
    try {
      const response = await http.post("/api/settings/modify_password", {
        current_password: form.currentPassword,
        password: form.newPassword
      })
      if (response.errorNo !== 0) {
        ElMessage.error(response.errorMsg || "密码修改失败")
        return
      }
      globalStatus.clear()
      ElMessage.success("密码已修改，请使用新密码重新登录")
      await router.replace({ name: "login" })
    } catch (error) {
      ElMessage.error("密码修改失败，请稍后重试")
    } finally {
      loading.value = false
    }
  }

  const logout = async () => {
    try {
      await http.post("/api/logout", {})
    } finally {
      globalStatus.clear()
      await router.replace({ name: "login" })
    }
  }

  return {
    form,
    loading,
    submit,
    logout
  }
}
