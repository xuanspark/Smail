import {EpMenu} from "vue-icons-plus/ep";

import {computed, ref, onMounted, onUnmounted} from 'vue'

import SecuritySettings from '@/views/components/SecuritySettings/SecuritySettings.vue'

import ThirdPartyAuthorization from '@/views/components/ThirdPartyAuthorization/ThirdPartyAuthorization.vue'

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

  const settingsMenu = computed(() => [
    { key: 'security', label: lang.security, component: SecuritySettings, capability: 'password.change_own' },
    { key: 'third-party', label: '第三方授权', component: ThirdPartyAuthorization, capability: 'plugin.write_own' },
    { key: 'config', label: '配置管理', component: ConfigSettings, capability: 'config.read' },
    { key: 'group', label: lang.group_settings, component: GroupSettings, capability: 'user.read' },
    { key: 'rule', label: lang.rule_setting, component: RuleSettings, capability: 'user.read' },
    { key: 'users', label: lang.user_management, component: UserManagement, capability: 'user.manage' },
    { key: 'plugins', label: lang.plugin_settings, component: PluginSettings, capability: 'plugin.read' },
  ].filter(item => globalStatus.hasCapability(item.capability)))

  return {
    EpMenu,
    ref,
    onMounted,
    onUnmounted,
    SecuritySettings,
    ThirdPartyAuthorization,
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
    settingsMenu
  }
}

export {
  EpMenu,
  BrandLogo,
  SecuritySettings,
  ThirdPartyAuthorization,
  ConfigSettings,
  GroupSettings,
  RuleSettings,
  UserManagement,
  PluginSettings
}
