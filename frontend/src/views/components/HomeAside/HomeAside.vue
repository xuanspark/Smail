<template>
<div class="sidebar-main">
    <div class="brand">
      <router-link to="/" class="brand-link">
        <span class="brand-logo-shell">
          <BrandLogo variant="compact" surface />
        </span>
        <span class="brand-text">Smail</span>
      </router-link>
    </div>
    <div class="search-box">
      <el-input
        v-model="searchQuery"
        @keyup.enter="submitSearch"
        @clear="submitSearch"
        :placeholder="lang.search"
        prefix-icon="Search"
        clearable
        class="custom-search"
      />
    </div>
    
    <div class="menu-container">
      <el-menu
        :default-active="activeGroup"
        class="sidebar-menu"
        @select="handleMenuSelect"
      >
        <el-menu-item v-for="item in data" :key="item.tag" :index="item.tag">
          <span class="menu-label">{{ item.label }}</span>
        </el-menu-item>
      </el-menu>
    </div>
    <div class="sidebar-footer" v-if="isLogin">
      <div class="account-identity" aria-label="当前登录账号" aria-live="polite">
        <span class="account-caption">当前账号</span>
        <strong class="account-name" :title="currentAccount">{{ currentAccount }}</strong>
        <el-tag :type="currentRole.type" size="small" effect="light" class="account-role">
          {{ currentRole.label }}
        </el-tag>
        <el-tooltip :content="lang.logout" placement="top">
          <button
            type="button"
            class="logout-btn"
            :aria-label="lang.logout"
            :aria-busy="loggingOut"
            :disabled="loggingOut"
            @click="logout"
          >
            <el-icon><SwitchButton /></el-icon>
            <span>{{ lang.logout }}</span>
          </button>
        </el-tooltip>
      </div>
      <button type="button" class="settings-btn" @click="openSettings">
        <div class="settings-btn-main">
          <el-icon><Setting /></el-icon>
          <span class="settings-label">{{ lang.settings }}</span>
        </div>
        <span class="app-version-badge">V2.13.0</span>
      </button>
    </div>
  </div>
</template>

<script setup>
import { usePage, BrandLogo, Setting, SwitchButton } from './HomeAside.js'

const {
  searchQuery,
  submitSearch,
  lang,
  activeGroup,
  handleMenuSelect,
  data,
  isLogin,
  currentAccount,
  currentRole,
  loggingOut,
  logout,
  openSettings
} = usePage()
</script>

<style scoped src="./HomeAside.css"></style>
