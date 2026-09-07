import { computed, nextTick, ref } from 'vue'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import BaseConfig from './BaseConfig/BaseConfig.vue'
import EmailConfig from './EmailConfig/EmailConfig.vue'
import SslConfig from './SslConfig/SslConfig.vue'
import PluginsConfig from './PluginsConfig/PluginsConfig.vue'
import ConfigMaintenance from './ConfigMaintenance/ConfigMaintenance.vue'

export function useNavigation() {
  const store = useGlobalStatusStore()
  const selectedSection = ref('base')
  const currentPage = ref(null)
  const pageHost = ref(null)
  const switching = ref(false)
  const pages = [
    { value: 'base', label: '基础配置', component: BaseConfig },
    { value: 'mail', label: '邮件服务', component: EmailConfig },
    { value: 'ssl', label: 'SSL 配置', component: SslConfig },
    { value: 'plugins', label: '插件配置', children: [
      { value: 'spam_block', label: '垃圾邮件拦截', component: PluginsConfig },
      { value: 'telegram_push', label: 'Telegram 推送', component: PluginsConfig },
      { value: 'wechat_push', label: '微信推送', component: PluginsConfig }
    ] }
  ]
  const tools = [
    { value: 'migration', label: '数据库迁移', capability: 'database.migrate' },
    { value: 'restart', label: '服务重启', capability: 'service.restart' },
    { value: 'backups', label: '备份与回滚', capability: 'config.rollback' },
    { value: 'audit', label: '配置审计', capability: 'audit.read' }
  ]
  const sectionOptions = computed(() => store.hasCapability('config.read') ? pages : [])
  const maintenanceOptions = computed(() => store.hasCapability('config.read')
    ? tools.filter(item => store.hasCapability(item.capability))
      .map(item => ({ ...item, component: ConfigMaintenance }))
    : [])
  const visiblePages = () => [...sectionOptions.value.flatMap(item => item.children || [item]), ...maintenanceOptions.value]
  const activePage = computed(() => visiblePages()
    .find(item => item.value === selectedSection.value))

  const beforeLeave = () => currentPage.value?.beforeLeave?.() ?? true
  const selectSection = async value => {
    const parent = sectionOptions.value.find(item => item.value === value && item.children)
    if (parent) value = parent.children.find(item => item.value === selectedSection.value)?.value || parent.children[0].value
    const visible = () => visiblePages().some(item => item.value === value)
    if (switching.value || value === selectedSection.value || !visible()) return
    switching.value = true
    try {
      if (!await beforeLeave() || !visible()) return
      selectedSection.value = value
      await nextTick()
      pageHost.value?.closest('.el-tabs__content')?.scrollTo({ top: 0 })
      pageHost.value?.focus({ preventScroll: true })
    } finally {
      switching.value = false
    }
  }

  return { sectionOptions, maintenanceOptions, selectedSection, activePage, currentPage, pageHost, selectSection, beforeLeave }
}
