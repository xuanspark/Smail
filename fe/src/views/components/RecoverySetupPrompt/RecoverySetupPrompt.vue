<template>
  <el-dialog
    v-model="visible"
    title="完善账户安全"
    width="min(520px, calc(100vw - 32px))"
    :close-on-click-modal="false"
    class="recovery-setup-dialog"
    @close="dismiss"
  >
    <div class="recovery-prompt-content">
      <div class="recovery-prompt-icon">盾</div>
      <h3>建议立即配置离线恢复码</h3>
      <p>
        离线恢复码可以在外部邮箱不可用时帮助您完成账户恢复。恢复码只会显示一次，生成后请保存到安全位置。
      </p>
      <el-alert
        v-if="generatedCodes.length === 0"
        title="当前尚未配置有效离线恢复码，高风险账户操作可能受到限制。"
        type="warning"
        :closable="false"
        show-icon
      />
      <div v-else class="recovery-code-result">
        <el-alert
          title="恢复码已生成，请立即复制并妥善保存。离开此提示后将无法再次读取。"
          type="success"
          :closable="false"
          show-icon
        />
        <div class="recovery-code-list" aria-label="离线恢复码列表">
          <code v-for="code in generatedCodes" :key="code">{{ code }}</code>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="recovery-prompt-actions">
        <el-button v-if="generatedCodes.length === 0" @click="dismiss">稍后处理</el-button>
        <el-button
          v-if="generatedCodes.length === 0"
          type="primary"
          :loading="loading"
          @click="generateRecoveryCodes"
        >
          立即生成恢复码
        </el-button>
        <el-button v-else type="primary" @click="dismiss">我已安全保存</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from "vue"

import { ElMessage } from "element-plus"

import { http } from "@/utils/axios"
import { useGlobalStatusStore } from "@/stores/useGlobalStatusStore"

const globalStatus = useGlobalStatusStore()
const loading = ref(false)
const generatedCodes = ref([])

const visible = computed({
  get: () => Boolean(
    globalStatus.userInfos.showRecoverySetupPrompt
      && !globalStatus.recoveryPromptDismissed
      && globalStatus.role !== "super_admin"
  ),
  set: value => {
    if (!value) {
      dismiss()
    }
  }
})

const dismiss = () => {
  generatedCodes.value = []
  globalStatus.dismissRecoveryPrompt()
}

const generateRecoveryCodes = async () => {
  if (loading.value) {
    return
  }
  loading.value = true
  try {
    const response = await http.post("/api/security/credentials/recovery-codes/generate", { count: 10 })
    if (response.errorNo !== 0 || !Array.isArray(response.data?.codes)) {
      throw new Error(response.errorMsg || "离线恢复码生成失败")
    }
    generatedCodes.value = response.data.codes
    globalStatus.userInfos.recoveryCodesConfigured = true
    globalStatus.userInfos.showRecoverySetupPrompt = false
    ElMessage.success("恢复码已生成，请仅保存这一次显示的内容")
  } catch (error) {
    ElMessage.error(error?.message || "离线恢复码生成失败，请稍后重试")
  } finally {
    loading.value = false
  }
}
</script>

<style scoped src="./RecoverySetupPrompt.css"></style>
