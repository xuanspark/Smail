import { computed, nextTick, onMounted, onBeforeUnmount, ref } from 'vue'
import { http } from '@/utils/axios'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'

export function usePage() {
  const store = useGlobalStatusStore()
  const selectedSection = ref(store.hasCapability('plugin.read_global') ? 'spam_block' : 'telegram-user')
  const currentPage = ref(null)
  const pageHost = ref(null)
  const switching = ref(false)
  const externalPlugins = ref([])
  const loading = ref(false)
  const errorMessage = ref('')
  let disposed = false

  // 全局配置不依赖正在运行的插件列表，未启用的插件也必须可以配置。
  const sectionOptions = computed(() => store.hasCapability('plugin.read_global') ? [
    { value: 'spam_block', label: '垃圾邮件拦截', kind: 'config' },
    ...(store.role === 'super_admin' ? [
      { value: 'telegram_push', label: '系统 Telegram 推送', kind: 'config' },
      { value: 'wechat_push', label: '系统微信推送', kind: 'config' }
    ] : [])
  ] : [])
  const accountOptions = computed(() => store.hasCapability('plugin.read') && store.hasCapability('plugin.write_own')
    ? [{ value: 'telegram-user', label: 'Telegram 通知', kind: 'account' }]
    : [])
  const canReadExternal = () => store.hasCapability('config.read') && store.hasCapability('plugin.read')
  const externalOptions = computed(() => canReadExternal() ? externalPlugins.value : [])
  const visiblePages = () => [...sectionOptions.value, ...accountOptions.value, ...externalOptions.value]
  const activePage = computed(() => visiblePages().find(item => item.value === selectedSection.value))
  const beforeLeave = () => currentPage.value?.beforeLeave?.() ?? true
  const selectSection = async value => {
    const visible = () => visiblePages().some(item => item.value === value)
    if (disposed || switching.value || value === selectedSection.value || !visible()) return
    switching.value = true
    try {
      if (!await beforeLeave() || disposed || !visible()) return
      selectedSection.value = value
      await nextTick()
      pageHost.value?.closest('.el-tabs__content')?.scrollTo({ top: 0 })
      pageHost.value?.focus({ preventScroll: true })
    } finally {
      switching.value = false
    }
  }

  const load = async () => {
    if (disposed || loading.value || !canReadExternal()) return
    loading.value = true
    errorMessage.value = ''
    try {
      const response = await http.get('/api/v1/plugin/list')
      if (disposed || !canReadExternal()) return
      if (response?.errorNo !== 0 || !Array.isArray(response.data)) {
        throw new Error(response?.errorMsg || '其他插件列表读取失败')
      }
      const builtins = new Set([
        'spam_block', 'telegram_push', 'wechat_push',
        'SpamBlock', 'TelegramPush', 'WechatPush', 'WeChatPushHook'
      ])
      externalPlugins.value = [...new Set(response.data)]
        .filter(name => typeof name === 'string' && /^[a-zA-Z0-9_-]{1,64}$/.test(name) && !builtins.has(name))
        .map(name => ({
          value: `external:${name}`,
          label: name,
          kind: 'external',
          src: `/api/v1/plugin/settings/${encodeURIComponent(name)}/index.html`
        }))
    } catch (error) {
      if (!disposed && canReadExternal()) errorMessage.value = error.message || '其他插件列表读取失败'
    } finally {
      loading.value = false
    }
  }

  onMounted(load)
  onBeforeUnmount(() => { disposed = true })
  return {
    sectionOptions, accountOptions, externalOptions, selectedSection, activePage,
    currentPage, pageHost, selectSection, beforeLeave, load, loading, errorMessage
  }
}
