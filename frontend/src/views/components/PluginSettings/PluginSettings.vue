<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.extensions }}</h3>
      <p class="settings-desc">{{ lang.extensions_desc }}</p>
    </div>

    <div class="plugin-container">
      <div v-if="pluginEntries.length" class="plugin-list">
        <section
          v-for="(entry, index) in pluginEntries"
          :key="entry.src"
          class="plugin-section"
        >
          <header class="plugin-section-header">
            <div class="plugin-section-title">
              <span class="plugin-section-index">{{ String(index + 1).padStart(2, '0') }}</span>
              <div>
                <h4>{{ entry.label }}</h4>
                <p>{{ entry.description }}</p>
              </div>
            </div>
            <span class="plugin-section-status is-active">已启用</span>
          </header>

          <div class="plugin-section-content">
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
          </div>
        </section>
      </div>
      <div v-else class="empty-state">
        <el-empty :description="lang.no_plugins_installed" :image-size="120" />
      </div>
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
