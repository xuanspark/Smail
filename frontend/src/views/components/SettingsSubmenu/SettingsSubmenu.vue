<template>
  <nav class="settings-submenu" :aria-label="title">
    <div class="settings-submenu-heading">
      <span class="settings-submenu-caption">{{ caption }}</span>
      <strong>{{ title }}</strong>
    </div>
    <div class="settings-submenu-list">
      <button
        v-for="item in items"
        :key="item.value"
        type="button"
        class="settings-submenu-item"
        :class="{ active: modelValue === item.value }"
        :aria-current="modelValue === item.value ? 'page' : undefined"
        @click="select(item.value)"
      >
        <span>{{ item.label }}</span>
      </button>
    </div>
  </nav>
</template>

<script setup>
import { submenuProps, useSubmenu } from './SettingsSubmenu.js'

const props = defineProps(submenuProps)
const emit = defineEmits(['select'])
const { select } = useSubmenu(props, emit)
</script>

<style scoped src="./SettingsSubmenu.css"></style>
