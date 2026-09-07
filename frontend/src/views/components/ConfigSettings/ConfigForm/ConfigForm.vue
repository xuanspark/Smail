<template>
  <div class="config-form" :data-form-section="section">
    <p v-if="!groups.length" class="form-empty">当前插件配置不存在，请刷新配置或检查初始化状态。</p>
    <section
      v-for="group in groups"
      :key="group.id"
      class="form-group"
      :data-group="group.id"
      :aria-label="group.title"
    >
      <div class="form-group-heading">
        <h4>{{ group.title }}</h4>
        <span v-if="group.fields.some(field => errors[fieldKey(field)])" class="section-error">待修正</span>
      </div>
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
