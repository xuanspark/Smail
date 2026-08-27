<template>
<div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.extensions }}</h3>
      <p class="settings-desc">{{ lang.extensions_desc }}</p>
    </div>

    <div class="plugin-container">
      <el-tabs class="custom-tabs">
        <el-tab-pane v-for="(src, name) in pluginList" :key="src" :label="name">
          <div class="iframe-wrapper">
            <iframe :src="src" sandbox="allow-scripts" referrerpolicy="no-referrer" ref="pluginFrames" :data-plugin="name" :title="name"></iframe>
          </div>
        </el-tab-pane>
        <el-tab-pane v-if="Object.keys(pluginList).length === 0" :label="lang.no_plugins">
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

const {
  lang,
  pluginList
} = usePage()
</script>

<style scoped src="./PluginSettings.css"></style>
