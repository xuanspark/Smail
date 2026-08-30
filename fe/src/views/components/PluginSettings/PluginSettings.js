import { computed, reactive, ref } from 'vue'

import { http } from '@/utils/axios'

import lang from '../../../i18n/i18n'

export function usePage() {
  const pluginList = reactive({})
  const pluginFrames = ref([])

  const pluginLogicalID = name => {
    const aliases = {
      SpamBlock: 'spam_block',
      WechatPush: 'wechat_push',
      TelegramPush: 'telegram_push'
    }
    return aliases[name] || name
  }

  const pluginEntries = computed(() => Object.entries(pluginList).map(([name, src]) => ({
    name,
    src,
    logicalID: pluginLogicalID(name)
  })))

  http.get('/api/v1/plugin/list').then(response => {
    if (Array.isArray(response.data) && response.data.length > 0) {
      for (const name of response.data) {
        if (typeof name !== 'string' || !/^[a-zA-Z0-9_-]{1,64}$/.test(name)) {
          continue
        }
        pluginList[name] = `/api/v1/plugin/settings/${encodeURIComponent(name)}/index.html`
      }
    }
  })

  return {
    ref,
    reactive,
    http,
    lang,
    pluginList,
    pluginEntries,
    pluginFrames
  }
}