<template>
<div class="header-container">
    <div class="header-left">
      <div class="mobile-menu-btn" @click="globalStatus.mobileDrawerVisible = true">
        <el-icon><EpMenu /></el-icon>
      </div>
      <div class="mobile-logo">
        <router-link to="/">
          <BrandLogo variant="compact" surface />
          <h1>Smail</h1>
        </router-link>
      </div>
    </div>
    
    <div class="header-right">
    </div>

  </div>

  <!-- 设置抽屉保留右侧导航，一级分类不影响内部标签页。 -->
  <el-drawer
    v-model="globalStatus.settingsDrawerVisible"
    :size="isMobile ? '100%' : '50%'"
    :title="lang.settings"
    :class="['settings-drawer', isMobile ? 'settings-drawer-mobile' : 'settings-drawer-desktop']"
    :with-header="true"
    direction="rtl"
    :before-close="closeSettings"
  >
    <template #header="{ titleId }">
      <div class="settings-drawer-heading">
        <h2 :id="titleId">{{ lang.settings }}</h2>
        <span class="settings-location" aria-live="polite">
          <span aria-hidden="true">/</span>
          {{ activeSettings?.label }}
        </span>
      </div>
    </template>
    <el-tabs
      v-model="globalStatus.settingsActiveTab"
      :tab-position="isMobile ? 'top' : 'right'"
      class="settings-tabs"
      :before-leave="beforeSettingsLeave"
    >
      <el-tab-pane
        v-for="item in settingsMenu"
        :key="item.key"
        :name="item.key"
        :label="item.label"
        lazy
      >
        <template #label>
          <span class="settings-category">
            <el-icon class="settings-category-icon" aria-hidden="true">
              <component :is="item.icon" />
            </el-icon>
            <span class="settings-category-copy">
              <span class="settings-category-label">{{ item.label }}</span>
              <span class="settings-category-hint" aria-hidden="true">{{ item.hint }}</span>
            </span>
          </span>
        </template>
        <component
          v-if="globalStatus.settingsDrawerVisible && globalStatus.settingsActiveTab === item.key"
          :is="item.component"
          :ref="instance => setSettingsPage(item.key, instance)"
        />
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<script setup>
import {
  usePage,
  EpMenu,
  BrandLogo,
  } from './HomeHeader.js'

const {
  globalStatus,
  isMobile,
  lang,
  settingsMenu,
  activeSettings,
  closeSettings,
  beforeSettingsLeave,
  setSettingsPage
} = usePage()
</script>

<style scoped src="./HomeHeader.css"></style>
