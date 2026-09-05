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
      <div class="settings-btn" @click="openSettings">
        <el-icon><Setting /></el-icon>
        <span class="settings-label">{{ lang.settings }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { usePage, BrandLogo, Setting } from './HomeAside.js'

const {
  searchQuery,
  lang,
  activeGroup,
  handleMenuSelect,
  data,
  isLogin,
  openSettings
} = usePage()
</script>

<style scoped src="./HomeAside.css"></style>
