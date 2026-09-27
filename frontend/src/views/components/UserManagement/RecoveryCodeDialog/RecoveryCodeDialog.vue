<template>
  <el-dialog
    :model-value="visible"
    title="管理安全码"
    width="min(800px, calc(100vw - 32px))"
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
      <div v-if="!revealed" class="result-title">
        <strong>新安全码已生成</strong>
        <span>请交给用户妥善保存。60 秒后自动隐藏，验证当前管理员密码后可再次查看有效安全码。</span>
      </div>
      <div v-else class="result-title">
        <strong>当前有效安全码</strong>
        <span>60 秒后自动隐藏；关闭弹窗后，需再次验证密码才能查看。</span>
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
        <el-button
          v-if="configured && !codes.length"
          :loading="revealing"
          :disabled="loading || generating || revoking"
          @click="revealCodes"
        >
          验证后查看
        </el-button>
        <el-button v-if="codes.length" :disabled="busy" @click="clearCodes">
          隐藏安全码
        </el-button>
        <el-button :disabled="busy" @click="close">关闭</el-button>
        <el-button
          v-if="configured && !codes.length"
          type="danger"
          plain
          :loading="revoking"
          :disabled="generating || revealing || loading"
          @click="revokeCodes"
        >
          撤销安全码
        </el-button>
        <el-button
          type="primary"
          :loading="generating"
          :disabled="revoking || revealing || loading"
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
  loading,
  revealing,
  revealed,
  clearCodes,
  generating,
  revoking,
  busy,
  errorMessage,
  close,
  generateCodes,
  revealCodes,
  revokeCodes,
  copyCodes,
} = useRecoveryCodeDialog(props, emit)
</script>

<style scoped src="./RecoveryCodeDialog.css"></style>
