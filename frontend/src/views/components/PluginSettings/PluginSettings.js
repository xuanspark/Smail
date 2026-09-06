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

  const pluginEntries = computed(() => {
    const order = {
      spam_block: 1,
      telegram_push: 2,
      wechat_push: 3
    }
    const labels = {
      spam_block: {
        label: '垃圾邮件拦截',
        description: '模型识别与过滤策略'
      },
      telegram_push: {
        label: 'Telegram 推送',
        description: '邮件通知与消息推送'
      },
      wechat_push: {
        label: '微信推送',
        description: '插件页面与推送参数'
      }
    }

    return Object.entries(pluginList)
      .map(([name, src]) => {
        const logicalID = pluginLogicalID(name)
        const metadata = labels[logicalID] || {
          label: name,
          description: '插件配置页面'
        }

        return {
          name,
          src,
          logicalID,
          label: metadata.label,
          description: metadata.description
        }
      })
      .sort((left, right) => (order[left.logicalID] || 99) - (order[right.logicalID] || 99))
  })

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
