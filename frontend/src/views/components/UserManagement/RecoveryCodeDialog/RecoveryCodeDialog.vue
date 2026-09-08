<template>
  <el-dialog
    :model-value="visible"
    title="管理安全码"
    width="min(560px, calc(100vw - 32px))"
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    :show-close="!busy"
    append-to-body
    destroy-on-close
    class="recovery-code-dialog"
    @update:model-value="value => { if (!value) close() }"
  >
    <div class="recovery-code-summary">
      <div>
        <span class="summary-label">账号</span>
        <strong>{{ user?.Account || '-' }}</strong>
      </div>
      <el-tag :type="configured ? 'success' : 'info'" effect="plain">
        {{ configured ? '已设置安全码' : '未设置安全码' }}
      </el-tag>
    </div>

    <div v-if="codes.length" class="recovery-code-result">
      <div class="result-title">
        <strong>新安全码仅显示这一次</strong>
        <span>请立即交给用户妥善保存，关闭弹窗后无法再次查看。</span>
      </div>
      <div class="recovery-code-grid">
        <code v-for="code in codes" :key="code">{{ code }}</code>
      </div>
      <el-button class="copy-button" @click="copyCodes">复制全部安全码</el-button>
    </div>

    <p v-else class="recovery-code-description">
      重新生成会立即撤销该用户原有的全部安全码。安全码明文不会写入用户列表或普通编辑请求。
    </p>

    <p v-if="errorMessage" class="dialog-feedback error" role="alert">
      {{ errorMessage }}
    </p>

    <template #footer>
      <div class="recovery-dialog-actions">
        <el-button :disabled="busy" @click="close">关闭</el-button>
        <el-button
          v-if="configured && !codes.length"
          type="danger"
          plain
          :loading="revoking"
          :disabled="generating"
          @click="revokeCodes"
        >
          撤销安全码
        </el-button>
        <el-button
          type="primary"
          :loading="generating"
          :disabled="revoking"
          @click="generateCodes"
        >
          {{ configured ? '重新生成安全码' : '生成安全码' }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import {
  recoveryCodeDialogProps,
  useRecoveryCodeDialog,
} from './RecoveryCodeDialog.js'

const props = defineProps(recoveryCodeDialogProps)
const emit = defineEmits(['update:modelValue', 'changed'])

const {
  visible,
  configured,
  codes,
  generating,
  revoking,
  busy,
  errorMessage,
  close,
  generateCodes,
  revokeCodes,
  copyCodes,
} = useRecoveryCodeDialog(props, emit)
</script>

<style scoped src="./RecoveryCodeDialog.css"></style>
