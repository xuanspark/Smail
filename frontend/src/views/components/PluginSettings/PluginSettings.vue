<template>
  <div class="config-settings plugin-settings">
    <div class="config-workspace">
      <aside class="config-navigation">
        <SettingsSubmenu
          v-if="sectionOptions.length"
          title="插件配置"
          caption="运行配置"
          :items="sectionOptions"
          :model-value="selectedSection"
          @select="selectSection"
        />
        <SettingsSubmenu
          v-if="accountOptions.length"
          title="账号通知"
          caption="当前账号"
          :items="accountOptions"
          :model-value="selectedSection"
          @select="selectSection"
        />
        <SettingsSubmenu
          v-if="externalOptions.length"
          title="其他插件"
          :items="externalOptions"
          :model-value="selectedSection"
          @select="selectSection"
        />
        <div v-if="errorMessage" class="plugin-list-error" role="alert">
          <span>{{ errorMessage }}</span>
          <el-button size="small" :loading="loading" @click="load">重试</el-button>
        </div>
      </aside>
      <div ref="pageHost" class="config-main" :aria-label="activePage?.label" tabindex="-1">
        <PluginsConfig
          v-if="activePage?.kind === 'config'"
          :key="selectedSection"
          ref="currentPage"
          :page-key="selectedSection"
        />
        <TelegramSettings v-else-if="activePage?.kind === 'account'" ref="currentPage" />
        <section v-else-if="activePage?.kind === 'external'" class="plugin-section">
          <header class="plugin-section-header"><h3>{{ activePage.label }}</h3></header>
          <div class="iframe-wrapper">
            <iframe
              :key="activePage.value"
              :src="activePage.src"
              sandbox="allow-scripts"
              referrerpolicy="no-referrer"
              :title="activePage.label"
            />
          </div>
        </section>
        <el-empty v-else description="当前账号没有可访问的插件配置" :image-size="100" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { usePage } from './PluginSettings.js'
import SettingsSubmenu from '../SettingsSubmenu/SettingsSubmenu.vue'
import PluginsConfig from '../ConfigSettings/PluginsConfig/PluginsConfig.vue'
import TelegramSettings from './TelegramSettings/TelegramSettings.vue'

const {
  sectionOptions, accountOptions, externalOptions, selectedSection, activePage,
  currentPage, pageHost, selectSection, beforeLeave, load, loading, errorMessage
} = usePage()

defineExpose({ beforeLeave })
</script>

<style scoped src="../ConfigSettings/ConfigSettings.css"></style>
<style scoped src="./PluginSettings.css"></style>
