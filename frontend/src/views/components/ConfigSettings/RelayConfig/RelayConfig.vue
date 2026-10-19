<template>
  <div class="relay-config">
    <!-- 顶部标题与控制开关 -->
    <div class="relay-header-card">
      <div class="relay-title-group">
        <div class="relay-main-title">
          <h5>SMTP 发信中继</h5>
          <el-tag :type="relay.enabled ? 'success' : 'info'" effect="light" class="relay-status-tag">
            {{ relay.enabled ? '中继服务：已启用' : '中继服务：已停用' }}
          </el-tag>
        </div>
        <p class="relay-desc">
          配置独立于本机邮件服务；关闭中继开关只停止使用，绝不删除已填写的中继信息。
        </p>
      </div>

      <div class="relay-switch-control">
        <span class="control-label">中继控制开关</span>
        <el-switch
          :model-value="!!relay.enabled"
          :disabled="disabled"
          active-text="启用"
          inactive-text="关闭"
          inline-prompt
          @change="val => emit('change', ['relay', 'enabled'], val)"
        />
      </div>
    </div>

    <!-- 清除标记提醒 -->
    <el-alert
      v-if="relay.clear"
      title="中继配置已标记清除，保存生效后将删除服务器、端口、账号、密码及发信配置。"
      type="warning"
      :closable="false"
      show-icon
      class="relay-alert"
    />

    <!-- 中继编辑表单区域：平铺展示，不再折叠收起 -->
    <div class="relay-editor-box">
      <div class="editor-header">
        <div class="editor-title-wrap">
          <h6>编辑 SMTP 中继配置</h6>
          <span class="editor-tip">表单字段已按响应式一行两列/三列进行紧凑排列；密码留空保持原值。</span>
        </div>
        <div class="editor-btns">
          <el-button
            v-if="hasConfigured"
            type="danger"
            plain
            size="small"
            :disabled="disabled"
            @click="clearRelay"
          >
            清除中继
          </el-button>
        </div>
      </div>

      <!-- 紧凑表单排版：一行两列/三列 -->
      <div class="relay-grid-form">
        <ConfigField
          v-for="field in formFields"
          :key="fieldKey(field)"
          :field="field"
          :value="getValue(config, field.path)"
          :error="errors[fieldKey(field)]"
          :disabled="disabled"
          @change="(path, value) => emit('change', path, value)"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { ElMessageBox } from 'element-plus'
import ConfigField from '../ConfigField/ConfigField.vue'
import { fieldKey, getValue, relayConfigured } from '../ConfigForm/ConfigForm.js'

const props = defineProps({
  config: { type: Object, required: true },
  fields: { type: Array, required: true },
  errors: { type: Object, default: () => ({}) },
  disabled: Boolean
})
const emit = defineEmits(['change'])

const relay = computed(() => props.config.relay || {})
const hasConfigured = computed(() => relayConfigured(props.config))

// 过滤掉已在顶部独立展示的中继开关，其余表单字段在下方平铺展示
const formFields = computed(() => {
  return (props.fields || []).filter(f => fieldKey(f) !== 'relay.enabled')
})

const clearRelay = async () => {
  try {
    await ElMessageBox.confirm(
      '清除后将删除已保存的 SMTP 中继信息和密码，关闭中继开关不会产生此影响。确认清除吗？',
      '确认清除 SMTP 中继',
      { type: 'warning', confirmButtonText: '确认清除', cancelButtonText: '取消' }
    )
    emit('change', ['relay', 'clear'], true)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}
</script>

<style scoped src="./RelayConfig.css"></style>