<template>
  <nav class="settings-submenu" :aria-label="title">
    <div class="settings-submenu-heading">
      <span class="settings-submenu-caption">{{ caption }}</span>
      <strong>{{ title }}</strong>
    </div>
    <div class="settings-submenu-list">
      <div v-for="item in items" :key="item.value" class="settings-submenu-entry">
        <button
          type="button"
          class="settings-submenu-item"
          :class="{ active: modelValue === item.value || item.children?.some(child => child.value === modelValue) }"
          :aria-current="modelValue === item.value ? 'page' : undefined"
          :aria-expanded="item.children ? item.children.some(child => child.value === modelValue) : undefined"
          @click="select(item.value)"
        >
          <span>{{ item.label }}</span>
          <span v-if="item.children" class="settings-submenu-count">{{ item.children.length }}</span>
        </button>
        <div
          v-if="item.children?.some(child => child.value === modelValue)"
          class="settings-tertiary-list"
          role="group"
          :aria-label="`${item.label}三级菜单`"
        >
          <button
            v-for="child in item.children"
            :key="child.value"
            type="button"
            class="settings-tertiary-item"
            :class="{ active: modelValue === child.value }"
            :aria-current="modelValue === child.value ? 'page' : undefined"
            @click="select(child.value)"
          >
            {{ child.label }}
          </button>
        </div>
      </div>
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
