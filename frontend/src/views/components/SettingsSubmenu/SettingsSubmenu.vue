<template>
  <nav class="settings-submenu" :aria-label="title">
    <div class="settings-submenu-heading">
      <span class="settings-submenu-caption">{{ caption }}</span>
      <strong>{{ title }}</strong>
    </div>
    <div class="settings-submenu-list">
      <div v-for="item in items" :key="item.value" class="settings-submenu-entry" :class="{ 'has-children': item.children?.length }">
        <button
          type="button"
          class="settings-submenu-item"
          :class="{ active: modelValue === item.value || item.children?.some(child => child.value === modelValue) }"
          :aria-current="modelValue === item.value ? 'page' : undefined"
          :aria-expanded="item.children?.length ? true : undefined"
          @click="select(item.value)"
        >
          <span>{{ item.label }}</span>
          <span v-if="item.children?.length" class="settings-submenu-chevron" aria-hidden="true"></span>
        </button>
        <div
          v-if="item.children?.length"
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
            <span class="settings-tertiary-icon" :data-plugin="child.value" aria-hidden="true">
              <svg v-if="child.value === 'spam_block'" viewBox="0 0 24 24" fill="none">
                <path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z" />
                <path d="m8 12 3 3 5-6" />
              </svg>
              <svg v-else-if="child.value === 'telegram_push'" viewBox="0 0 24 24" fill="none">
                <path d="m3 10 18-7-5 18-5-7-8-4Z" />
                <path d="m11 14 5-6" />
              </svg>
              <svg v-else viewBox="0 0 24 24" fill="none">
                <path d="M20 11a8 8 0 0 1-8 8H5l-3 2 1-6a8 8 0 1 1 17-4Z" />
                <path d="M7 10h.01M12 10h.01M17 10h.01" stroke-width="3" />
              </svg>
            </span>
            <span>{{ child.label }}</span>
            <span v-if="modelValue === child.value" class="settings-tertiary-current" aria-hidden="true"></span>
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
