<script setup>
import {RouterView, useRoute} from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import HomeHeader from '@/views/components/HomeHeader/HomeHeader.vue'
import HomeAside from '@/views/components/HomeAside/HomeAside.vue';
import SecuritySetupPrompt from '@/views/components/SecuritySetupPrompt/SecuritySetupPrompt.vue'
import StartupStatus from '@/views/components/StartupStatus/StartupStatus.vue'
import {computed, ref, watch} from 'vue'
import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";

const route = useRoute()
const pageName = ref(route.name)
const globalStatus = useGlobalStatusStore();
const standalonePages = ['login', 'login-verification', 'force-password-change', 'setup', 'forgot-password', 'reset-blocked']
const showStartupStatus = computed(() => globalStatus.startupBlocked && pageName.value !== 'setup')

watch(
    () => route.fullPath,
    () => {
      pageName.value = route.name
    }
)

</script>

<template>
  <el-config-provider :locale="zhCn">
  <StartupStatus v-if="showStartupStatus" />
  <div v-else id="main">
    <HomeHeader v-if="!standalonePages.includes(pageName)"/>
    <div id="content">
      <div id="aside" v-if="!standalonePages.includes(pageName)">
        <HomeAside/>
      </div>
      <el-drawer
          v-if="!standalonePages.includes(pageName)"
          v-model="globalStatus.mobileDrawerVisible"
          direction="ltr"
          size="243px"
          :with-header="false"
          class="mobile-aside-drawer"
      >
        <HomeAside/>
      </el-drawer>
      <div
        id="body"
        :class="{
          'full-bleed': standalonePages.includes(pageName),
          'recovery-fit': pageName === 'forgot-password'
        }"
      >
        <RouterView v-slot="{ Component }">
          <transition name="page-fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </RouterView>
      </div>
      <SecuritySetupPrompt v-if="!standalonePages.includes(pageName)" />
    </div>
  </div>
  </el-config-provider>
</template>


<style scoped>
#aside {
  width: 264px;
  min-width: 264px;
  max-width: 264px;
  margin: 16px 0 16px 16px;
  background: var(--pm-bg-sidebar);
  border: 1px solid var(--pm-border-color);
  border-radius: var(--pm-radius-xl);
  box-shadow: var(--pm-shadow-sm);
  overflow: hidden;
}

#body {
  width: 100%;
  max-width: 100%;
  min-width: 0;
  height: 100%;
  padding: 16px;
  box-sizing: border-box;
  overflow: hidden;
}

#body.full-bleed {
  width: 100vw;
  max-width: 100vw;
  min-width: 0;
  padding: 0;
}

#body.recovery-fit {
  overflow: hidden;
}

#content {
  display: flex;
  flex-grow: 1;
  width: 100%;
  min-width: 0;
  overflow: hidden;
  min-height: 0;
}

#main {
  width: 100%;
  min-width: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.mobile-aside-drawer :deep(.el-drawer__body) {
  padding: 0;
}

@media (max-width: 768px) {
  #aside {
    display: none !important;
  }
  #body {
    padding: 10px;
  }
  #body.full-bleed {
    padding: 0;
  }
}
</style>
