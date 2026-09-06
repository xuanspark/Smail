<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.extensions }}</h3>
      <p class="settings-desc">{{ lang.extensions_desc }}</p>
    </div>

    <div class="plugin-container">
      <el-tabs class="custom-tabs">
        <el-tab-pane v-for="entry in pluginEntries" :key="entry.src" :label="entry.name">
          <SpamBlockSettings v-if="entry.logicalID === 'spam_block'" />
          <TelegramSettings v-else-if="entry.logicalID === 'telegram_push'" />
          <div v-else class="iframe-wrapper">
            <iframe
              :src="entry.src"
              sandbox="allow-scripts"
              referrerpolicy="no-referrer"
              ref="pluginFrames"
              :data-plugin="entry.name"
              :title="entry.name"
            />
          </div>
        </el-tab-pane>
        <el-tab-pane v-if="pluginEntries.length === 0" :label="lang.no_plugins">
          <div class="empty-state">
            <el-empty :description="lang.no_plugins_installed" :image-size="120" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup>
import { usePage } from './PluginSettings.js'
import SpamBlockSettings from './SpamBlockSettings/SpamBlockSettings.vue'
import TelegramSettings from './TelegramSettings/TelegramSettings.vue'

const {
  lang,
  pluginEntries
} = usePage()
</script>

<style scoped src="./PluginSettings.css"></style>
