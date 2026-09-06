<template>
  <div
    class="config-field"
    :class="{ 'field-wide': ['list', 'network'].includes(field.type) && field.wide !== false }"
    :data-field="fieldKey(field)"
  >
    <label :id="`${id}-label`" :for="id" class="field-label">
      {{ field.label }}
      <span v-if="field.required" class="field-required" aria-hidden="true">*</span>
      <span v-if="field.locked" class="field-locked">系统维护</span>
      <el-popover v-if="field.hint" trigger="click" :width="300" :content="field.hint" placement="top">
        <template #reference>
          <button type="button" class="field-help" :aria-label="`${field.label}填写说明`" @click.prevent>?</button>
        </template>
      </el-popover>
    </label>
    <el-select
      v-if="field.type === 'select'"
      :id="id"
      :model-value="value ?? field.fallback ?? ''"
      :empty-values="[null, undefined]"
      :disabled="disabled || field.locked"
      :aria-describedby="descriptionId"
      :aria-invalid="!!error"
      placeholder="请选择"
      @update:model-value="update"
    >
      <el-option v-for="option in selectOptions" :key="String(option.value)" :label="option.label" :value="option.value" />
    </el-select>
    <el-switch
      v-else-if="field.type === 'switch'"
      :id="id"
      :model-value="!!value"
      :disabled="disabled || field.locked"
      :aria-label="field.label"
      :aria-describedby="descriptionId"
      :aria-invalid="!!error"
      active-text="已开启"
      inactive-text="已关闭"
      @update:model-value="update"
    />
    <el-input-number
      v-else-if="field.type === 'number'"
      :id="id"
      :model-value="value === undefined ? 0 : value"
      :min="field.min"
      :max="field.max"
      :step="field.step || 1"
      :precision="field.integer ? 0 : undefined"
      :disabled="disabled || field.locked"
      :aria-label="field.label"
      :aria-describedby="descriptionId"
      :aria-invalid="!!error"
      controls-position="right"
      @update:model-value="update"
    />
    <div v-else-if="field.type === 'list'" class="field-list" role="group" :aria-labelledby="`${id}-label`">
      <div v-for="{ entry, index } in pageItems" :key="index" class="field-list-row">
        <el-input
          class="field-text-input"
          :id="index === pageItems[0]?.index ? id : `${id}-${index}`"
          :model-value="entry"
          :disabled="disabled || field.locked"
          :placeholder="field.placeholder || '填写内容'"
          :aria-label="`${field.label}，第 ${index + 1} 项`"
          :aria-describedby="descriptionId"
          :aria-invalid="!!error"
          @update:model-value="updateEntry(index, $event)"
        />
        <el-button :disabled="disabled || field.locked" :aria-label="`移除${field.label}第 ${index + 1} 项`" @click="removeEntry(index)">移除</el-button>
      </div>
      <el-button class="field-add" :disabled="disabled || field.locked" plain @click="addEntry">添加一项</el-button>
    </div>
    <div v-else-if="field.type === 'network'" class="field-list" role="group" :aria-labelledby="`${id}-label`">
      <div v-for="{ entry, index } in pageItems" :key="index" class="network-row">
        <el-select
          :id="index === pageItems[0]?.index ? id : `${id}-${index}`"
          :model-value="entry.scheme"
          :disabled="disabled || field.locked"
          :aria-label="`网络目标 ${index + 1} 协议`"
          :aria-describedby="descriptionId"
          :aria-invalid="!!error"
          placeholder="协议"
          @update:model-value="updateTarget(index, 'scheme', $event)"
        >
          <el-option label="HTTPS" value="https" />
          <el-option label="WSS" value="wss" />
        </el-select>
        <el-input
          class="field-text-input"
          :model-value="entry.host"
          :disabled="disabled || field.locked"
          placeholder="api.example.com"
          :aria-label="`网络目标 ${index + 1} 域名`"
          :aria-describedby="descriptionId"
          :aria-invalid="!!error"
          @update:model-value="updateTarget(index, 'host', $event)"
        />
        <el-input-number
          :model-value="entry.port"
          :disabled="disabled || field.locked"
          :min="1"
          :max="65535"
          :precision="0"
          :controls="false"
          :aria-label="`网络目标 ${index + 1} 端口`"
          :aria-describedby="descriptionId"
          :aria-invalid="!!error"
          @update:model-value="updateTarget(index, 'port', $event)"
        />
        <el-button :disabled="disabled || field.locked" :aria-label="`移除网络目标 ${index + 1}`" @click="removeEntry(index)">移除</el-button>
      </div>
      <el-button class="field-add" :disabled="disabled || field.locked" plain @click="addEntry">添加网络目标</el-button>
    </div>
    <el-input
      v-else
      class="field-text-input"
      :id="id"
      :model-value="value ?? ''"
      :disabled="disabled || field.locked"
      :placeholder="field.placeholder || '填写内容'"
      :aria-describedby="descriptionId"
      :aria-invalid="!!error"
      autocomplete="off"
      @update:model-value="update"
    />
    <ConfigPagination
      v-if="['list', 'network'].includes(field.type)"
      v-model:current-page="currentPage"
      :page-count="pageCount"
      :total="total"
      :label="`${field.label}分页`"
    />
    <div v-if="field.recommendedPort !== undefined" class="field-port-guide">
      <span :class="{ 'port-custom': value !== field.recommendedPort }">
        {{ value === field.recommendedPort ? '已使用推荐端口' : '推荐端口' }}：{{ field.recommendedPort }}
      </span>
      <el-button
        v-if="value !== field.recommendedPort"
        link
        type="primary"
        :disabled="disabled || field.locked"
        :aria-label="`将${field.label}恢复为推荐端口 ${field.recommendedPort}`"
        @click="update(field.recommendedPort)"
      >
        使用推荐端口
      </el-button>
    </div>
    <p v-if="field.hint" :id="`${id}-hint`" class="field-hint field-description">{{ field.hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="field-error" role="alert">{{ error }}</p>
  </div>
</template>

<script setup>
import { fieldKey, useField } from './ConfigField.js'
import ConfigPagination from '../ConfigPagination/ConfigPagination.vue'

const props = defineProps({
  field: { type: Object, required: true },
  value: { type: [String, Number, Boolean, Array, Object], default: undefined },
  disabled: Boolean,
  error: { type: String, default: '' }
})
const emit = defineEmits(['change'])
const {
  id, descriptionId, selectOptions, update, updateEntry, updateTarget, removeEntry,
  addEntry, currentPage, pageCount, pageItems, total
} = useField(props, emit)
</script>

<style scoped src="./ConfigField.css"></style>
