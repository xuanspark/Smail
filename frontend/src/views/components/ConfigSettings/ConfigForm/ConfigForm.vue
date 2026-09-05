<template>
  <div class="config-form" :data-form-section="section">
    <p v-if="!groups.length" class="form-empty">当前没有可配置的插件。安装插件后可在这里管理启用状态与运行设置。</p>
    <section v-for="group in groups" :key="group.id" class="form-group" :aria-labelledby="`form-group-${group.id}`">
      <div class="form-group-heading">
        <h5 :id="`form-group-${group.id}`">{{ group.title }}</h5>
        <span v-if="section === 'plugins'" class="credential-status">{{ group.secretConfigured ? '凭据已配置' : '未配置凭据' }}</span>
      </div>
      <p class="form-group-description">{{ group.description }}</p>
      <p v-if="group.note" class="form-group-description">{{ group.note }}</p>
      <div class="form-grid">
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
import { computed } from 'vue'
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
</script>

<style scoped src="./ConfigForm.css"></style>
