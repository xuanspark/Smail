<template>
  <div class="relay-config">
    <div class="relay-list-heading">
      <div>
        <h5>已配置的 SMTP 中继</h5>
        <p>中继配置独立于本机邮件服务；关闭开关只停止使用，不会删除已填写的信息。</p>
      </div>
      <el-button v-if="hasConfigured && !editing" type="primary" plain :disabled="disabled" @click="editing = true">
        编辑中继
      </el-button>
    </div>

    <el-alert
      v-if="relay.clear"
      title="已标记清除，保存后将删除服务器、端口、账号、密码、发件邮箱和发件域名。"
      type="warning"
      :closable="false"
      show-icon
      class="relay-alert"
    />

    <div v-if="hasConfigured && !editing" class="relay-list" role="list" aria-label="SMTP 中继列表">
      <article class="relay-item" role="listitem">
        <div class="relay-item-heading">
          <div>
            <strong>默认 SMTP 中继</strong>
            <span class="relay-subtitle">单个默认发信中继配置</span>
          </div>
          <el-tag :type="relay.enabled ? 'success' : 'info'" effect="plain">
            {{ relay.enabled ? '已启用' : '已关闭' }}
          </el-tag>
        </div>
        <dl class="relay-summary">
          <div><dt>SMTP 服务器</dt><dd>{{ relay.relayHost || '未填写' }}</dd></div>
          <div><dt>端口</dt><dd>{{ relay.relayPort || '未填写' }}</dd></div>
          <div><dt>认证账号</dt><dd>{{ relay.username || '未填写' }}</dd></div>
          <div><dt>发件邮箱</dt><dd>{{ relay.from || '未填写' }}</dd></div>
          <div><dt>密码</dt><dd>{{ passwordConfigured ? '已配置（不显示明文）' : '未配置' }}</dd></div>
          <div><dt>发件域名</dt><dd>{{ relay.fromDomain || '未填写' }}</dd></div>
        </dl>
        <div class="relay-item-actions">
          <el-button type="primary" plain :disabled="disabled" @click="editing = true">编辑</el-button>
          <el-button type="danger" plain :disabled="disabled" @click="clearRelay">清除中继</el-button>
        </div>
      </article>
    </div>

    <el-empty v-else-if="!editing" description="暂无已配置的 SMTP 中继" />

    <div v-if="editing" class="relay-editor">
      <div class="relay-editor-heading">
        <h5>{{ hasConfigured ? '编辑 SMTP 中继' : '添加 SMTP 中继' }}</h5>
        <span>密码留空表示保持原值，清除操作需要单独点击“清除中继”。</span>
      </div>
      <div class="form-grid relay-fields">
        <ConfigField
          v-for="field in fields"
          :key="fieldKey(field)"
          :field="field"
          :value="getValue(config, field.path)"
          :error="errors[fieldKey(field)]"
          :disabled="disabled"
          @change="(path, value) => emit('change', path, value)"
        />
      </div>
      <div class="relay-editor-actions">
        <el-button v-if="hasConfigured" :disabled="disabled" @click="editing = false">取消编辑</el-button>
        <el-button v-if="hasConfigured" type="danger" plain :disabled="disabled" @click="clearRelay">清除中继</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
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
const passwordConfigured = computed(() => !!props.config.relayPasswordStatus?.configured)
const editing = ref(!hasConfigured.value)

const clearRelay = async () => {
  try {
    await ElMessageBox.confirm(
      '清除后将删除已保存的 SMTP 中继信息和密码，关闭中继开关不会产生此影响。确认清除吗？',
      '确认清除 SMTP 中继',
      { type: 'warning', confirmButtonText: '确认清除', cancelButtonText: '取消' }
    )
    emit('change', ['relay', 'clear'], true)
    editing.value = false
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}
</script>

<style scoped src="./RelayConfig.css"></style>