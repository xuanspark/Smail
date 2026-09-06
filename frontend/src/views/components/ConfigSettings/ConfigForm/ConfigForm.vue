<template>
  <div class="config-form" :data-form-section="section">
    <p v-if="!groups.length" class="form-empty">当前插件配置不存在，请刷新配置或检查初始化状态。</p>
    <nav v-if="section === 'base' && groups.length > 1" class="form-sections" aria-label="基础配置分区">
      <button
        v-for="group in groups"
        :key="group.id"
        type="button"
        :class="{ active: activeGroup === group.id }"
        :aria-pressed="activeGroup === group.id"
        @click="activeGroup = group.id"
      >
        {{ group.title }}
        <span v-if="group.fields.some(field => errors[fieldKey(field)])" class="section-error">待修正</span>
      </button>
    </nav>
    <section
      v-for="group in groups"
      v-show="section !== 'base' || activeGroup === group.id"
      :key="group.id"
      class="form-group"
      :data-group="group.id"
      :aria-label="group.title"
    >
      <p v-if="group.secretConfigured !== undefined" class="credential-status">{{ group.secretConfigured ? '凭据已配置' : '未配置凭据' }}</p>
      <p v-if="group.note" class="form-group-note" role="note">{{ group.note }}</p>
      <div class="form-grid" :class="{ 'site-grid': section === 'base' && group.id === 'site' }">
        <ConfigField
          v-for="field in group.fields"
          :key="fieldKey(field)"
          :field="field"
          :value="getValue(config, field.path)"
          :error="errors[fieldKey(field)]"
          :disabled="disabled"
          @change="(path, value) => emit('change', path, value)"
        />
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { fieldKey, getFieldGroups, getValue } from './ConfigForm.js'
import ConfigField from '../ConfigField/ConfigField.vue'

const props = defineProps({
  section: { type: String, required: true },
  config: { type: Object, required: true },
  errors: { type: Object, default: () => ({}) },
  disabled: Boolean
})
const emit = defineEmits(['change'])
const groups = computed(() => getFieldGroups(props.section, props.config))
const activeGroup = ref(groups.value[0]?.id)

// 切换分区只改变展示，不卸载字段或丢弃尚未保存的配置。
watch(groups, value => {
  if (!value.some(group => group.id === activeGroup.value)) activeGroup.value = value[0]?.id
})
watch(() => props.errors, errors => {
  const invalid = groups.value.find(group => group.fields.some(field => errors[fieldKey(field)]))
  if (invalid) activeGroup.value = invalid.id
})
</script>

<style scoped src="./ConfigForm.css"></style>
