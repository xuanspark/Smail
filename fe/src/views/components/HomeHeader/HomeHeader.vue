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
    <el-tabs :tab-position="isMobile ? 'top' : 'right'" class="settings-tabs">
      <el-tab-pane v-for="item in settingsMenu" :key="item.key" :label="item.label">
        <component :is="item.component" />
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<script setup>
import {
  usePage,
  EpMenu,
  BrandLogo,
  SecuritySettings,
  ThirdPartyAuthorization,
  ConfigSettings,
  GroupSettings,
  RuleSettings,
  UserManagement,
  PluginSettings,
} from './HomeHeader.js'

const {
  globalStatus,
  isMobile,
  lang,
  settingsMenu
} = usePage()
</script>

<style scoped src="./HomeHeader.css"></style>
