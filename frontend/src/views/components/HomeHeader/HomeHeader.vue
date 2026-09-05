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

  <!-- Settings Drawer -->
  <el-drawer v-model="globalStatus.settingsDrawerVisible" :size="isMobile ? '100%' : '50vw'" :title="lang.settings" class="settings-drawer" :with-header="true" direction="rtl">
    <el-tabs v-model="globalStatus.settingsActiveTab" :tab-position="isMobile ? 'top' : 'right'" class="settings-tabs">
      <el-tab-pane v-for="item in settingsMenu" :key="item.key" :name="item.key" :label="item.label" lazy>
        <component v-if="globalStatus.settingsDrawerVisible && globalStatus.settingsActiveTab === item.key" :is="item.component" />
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
  settingsMenu
} = usePage()
</script>

<style scoped src="./HomeHeader.css"></style>
