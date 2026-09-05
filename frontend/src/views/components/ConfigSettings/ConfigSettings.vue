<template>
  <div class="config-settings">
    <div class="config-workspace">
      <aside class="config-navigation">
        <SettingsSubmenu
          title="配置管理"
          caption="配置页面"
          :items="sectionOptions"
          :model-value="selectedSection"
          @select="selectSection"
        />
        <SettingsSubmenu
          v-if="maintenanceOptions.length"
          title="维护与记录"
          caption="独立工具"
          :items="maintenanceOptions"
          :model-value="selectedSection"
          @select="selectSection"
        />
      </aside>
      <div ref="pageHost" class="config-main" :aria-label="activePage?.label" tabindex="-1">
        <component
          :is="activePage.component"
          v-if="activePage"
          :key="selectedSection"
          ref="currentPage"
          :page-key="selectedSection"
          :page-title="activePage.label"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import SettingsSubmenu from '../SettingsSubmenu/SettingsSubmenu.vue'
import { useNavigation } from './ConfigNavigation.js'

const {
  sectionOptions, maintenanceOptions, selectedSection, activePage,
  currentPage, pageHost, selectSection, beforeLeave
} = useNavigation()

defineExpose({ beforeLeave })
</script>

<style scoped src="./ConfigSettings.css"></style>
