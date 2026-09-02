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
  <el-drawer v-model="globalStatus.settingsDrawerVisible" :size="isMobile ? '100%' : '600px'" :title="lang.settings" class="settings-drawer" :with-header="true" direction="rtl">
    <el-tabs :tab-position="isMobile ? 'top' : 'left'" class="settings-tabs">
      <el-tab-pane :label="lang.security">
        <SecuritySettings/>
      </el-tab-pane>
      <el-tab-pane label="第三方授权">
        <ThirdPartyAuthorization />
      </el-tab-pane>
      <el-tab-pane v-if="canReadConfig" label="配置管理">
        <ConfigSettings />
      </el-tab-pane>
      <el-tab-pane :label="lang.group_settings">
        <GroupSettings/>
      </el-tab-pane>
      <el-tab-pane :label="lang.rule_setting">
        <RuleSettings/>
      </el-tab-pane>
      <el-tab-pane v-if="canManageUsers" :label="lang.user_management">
        <UserManagement/>
      </el-tab-pane>
      <el-tab-pane v-if="canReadPlugins" :label="lang.plugin_settings">
        <PluginSettings/>
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
  canReadConfig,
  canManageUsers,
  canReadPlugins
} = usePage()
</script>

<style scoped src="./HomeHeader.css"></style>
