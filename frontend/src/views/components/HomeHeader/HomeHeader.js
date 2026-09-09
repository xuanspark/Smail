import {EpMenu} from "vue-icons-plus/ep";
import { Lock, Connection, Setting, FolderOpened, Filter, User, Grid, Document } from '@element-plus/icons-vue';

import {computed, ref, onMounted, onUnmounted, defineAsyncComponent} from 'vue'
import SettingsPageStatus from '../SettingsPageStatus/SettingsPageStatus.vue'

import lang from '../../../i18n/i18n';

import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";

import BrandLogo from '../BrandLogo/BrandLogo.vue';

// 分类首次打开时才下载页面，失败时保留可读提示，不能显示空白面板。
const lazySettings = loader => defineAsyncComponent({
  loader,
  loadingComponent: SettingsPageStatus,
  errorComponent: SettingsPageStatus,
  delay: 150,
  timeout: 30000
})
const SecuritySettings = lazySettings(() => import('../SecuritySettings/SecuritySettings.vue'))
const ThirdPartyAuthorization = lazySettings(() => import('../ThirdPartyAuthorization/ThirdPartyAuthorization.vue'))
const ConfigSettings = lazySettings(() => import('../ConfigSettings/ConfigSettings.vue'))
const GroupSettings = lazySettings(() => import('../GroupSettings/GroupSettings.vue'))
const RuleSettings = lazySettings(() => import('../RuleSettings/RuleSettings.vue'))
const UserManagement = lazySettings(() => import('../UserManagement/UserManagement.vue'))
const PluginSettings = lazySettings(() => import('../PluginSettings/PluginSettings.vue'))
const LogSettings = lazySettings(() => import('../LogSettings/LogSettings.vue'))

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
    { key: 'security', label: lang.security, hint: '账号与登录保护', icon: Lock, component: SecuritySettings, capability: 'password.change_own' },
    { key: 'third-party', label: '邮件客户端', hint: '当前账号协议授权', icon: Connection, component: ThirdPartyAuthorization, capability: 'user.read' },
    { key: 'config', label: '配置管理', hint: '服务与运行配置', icon: Setting, component: ConfigSettings, capability: 'config.read' },
    { key: 'group', label: lang.group_settings, hint: '邮箱分组管理', icon: FolderOpened, component: GroupSettings, capability: 'user.read' },
    { key: 'rule', label: lang.rule_setting, hint: '邮件处理规则', icon: Filter, component: RuleSettings, capability: 'user.read' },
    { key: 'users', label: lang.user_management, hint: '账号与权限管理', icon: User, component: UserManagement, capability: 'user.manage' },
    { key: 'plugins', label: lang.plugin_settings, hint: '扩展功能管理', icon: Grid, component: PluginSettings, capability: 'plugin.read' },
    { key: 'logs', label: '日志', hint: '操作、收发信与运行记录', icon: Document, component: LogSettings, capability: 'audit.read' },
  ].filter(item => {
    // 日志数据范围由服务端强制过滤；菜单只决定入口是否可见。
    return globalStatus.hasCapability(item.capability) ||
      (item.key === 'plugins' && globalStatus.hasCapability('plugin.read_global'))
  }))

  const activeSettings = computed(() => settingsMenu.value.find(item => item.key === globalStatus.settingsActiveTab))
  const settingsPages = new Map()
  const setSettingsPage = (key, instance) => {
    if (instance) settingsPages.set(key, instance)
    else settingsPages.delete(key)
  }
  let checkingLeave = false
  const beforeSettingsLeave = async () => {
    if (checkingLeave) return false
    checkingLeave = true
    try {
      return await (settingsPages.get(globalStatus.settingsActiveTab)?.beforeLeave?.() ?? true)
    } finally {
      checkingLeave = false
    }
  }
  const closeSettings = async done => {
    if (await beforeSettingsLeave()) done()
  }

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
    settingsMenu,
    activeSettings,
    setSettingsPage,
    beforeSettingsLeave,
    closeSettings
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
