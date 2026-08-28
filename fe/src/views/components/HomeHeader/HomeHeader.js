import {EpMenu} from "vue-icons-plus/ep";

import {computed, ref, onMounted, onUnmounted} from 'vue'

import SecuritySettings from '@/views/components/SecuritySettings/SecuritySettings.vue'

import ConfigSettings from '@/views/components/ConfigSettings/ConfigSettings.vue'

import lang from '../../../i18n/i18n';

import GroupSettings from '../GroupSettings/GroupSettings.vue';

import RuleSettings from '../RuleSettings/RuleSettings.vue';

import UserManagement from '../UserManagement/UserManagement.vue';

import PluginSettings from '../PluginSettings/PluginSettings.vue';

import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";

import BrandLogo from '../BrandLogo/BrandLogo.vue';

export function usePage() {
  const globalStatus = useGlobalStatusStore();
  
  const userInfos = globalStatus.userInfos;
  
  const isMobile = ref(window.innerWidth <= 768);
  
  const handleResize = () => {
    isMobile.value = window.innerWidth <= 768;
  };
  
  onMounted(() => {
    window.addEventListener('resize', handleResize);
  });
  
  onUnmounted(() => {
    window.removeEventListener('resize', handleResize);
  });

  return {
    EpMenu,
    ref,
    onMounted,
    onUnmounted,
    SecuritySettings,
    ConfigSettings,
    lang,
    GroupSettings,
    RuleSettings,
    UserManagement,
    PluginSettings,
    useGlobalStatusStore,
    BrandLogo,
    globalStatus,
    userInfos,
    isMobile,
    handleResize,
    canReadConfig: computed(() => globalStatus.hasCapability("config.read")),
    canManageUsers: computed(() => globalStatus.hasCapability("user.manage")),
    canReadPlugins: computed(() => globalStatus.hasCapability("plugin.read"))
  }
}

export {
  EpMenu,
  BrandLogo,
  SecuritySettings,
  ConfigSettings,
  GroupSettings,
  RuleSettings,
  UserManagement,
  PluginSettings
}
