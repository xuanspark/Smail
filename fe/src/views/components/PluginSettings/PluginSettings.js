import {onBeforeUnmount, onMounted, reactive, ref} from 'vue'

import {http} from "@/utils/axios";

import lang from '../../../i18n/i18n';

export function usePage() {
  const pluginList = reactive({})
  
  const pluginFrames = ref([])
  
  function normalizePluginPayload(payload) {
    if (!payload || Object.getPrototypeOf(payload) !== Object.prototype) {
      return null
    }
    const entries = Object.entries(payload)
    if (entries.length > 20) {
      return null
    }
    const normalized = {}
    for (const [key, value] of entries) {
      if (!/^[a-zA-Z0-9_-]{1,64}$/.test(key) || typeof value !== 'string' || value.length > 4096) {
        return null
      }
      normalized[key] = value
    }
    return normalized
  }
  
  function isKnownPluginFrame(source, plugin) {
    return pluginFrames.value.some((frame) => frame?.dataset?.plugin === plugin && frame.contentWindow === source)
  }
  
  function handlePluginMessage(event) {
    const message = event.data
    if (!message || message.type !== 'smail-plugin-settings-save' || typeof message.plugin !== 'string') {
      return
    }
    if (!Object.prototype.hasOwnProperty.call(pluginList, message.plugin) || !isKnownPluginFrame(event.source, message.plugin)) {
      return
    }
    const payload = normalizePluginPayload(message.payload)
    if (!payload) {
      return
    }
    http.post(`/api/plugin/settings/${encodeURIComponent(message.plugin)}/save`, payload)
      .then(() => event.source.postMessage({type: 'smail-plugin-settings-result', plugin: message.plugin, ok: true}, '*'))
      .catch(() => event.source.postMessage({type: 'smail-plugin-settings-result', plugin: message.plugin, ok: false}, '*'))
  }
  
  onMounted(() => window.addEventListener('message', handlePluginMessage))
  
  onBeforeUnmount(() => window.removeEventListener('message', handlePluginMessage))
  
  http.get('/api/plugin/list').then(res => {
    if (res.data != null && res.data.length > 0) {
      for (let i = 0; i < res.data.length; i++) {
        let name = res.data[i];
        pluginList[name] = "/api/plugin/settings/" + encodeURIComponent(name) + "/index.html";
      }
    }
  })

  return {
    onBeforeUnmount,
    onMounted,
    reactive,
    ref,
    http,
    lang,
    pluginList,
    pluginFrames,
    normalizePluginPayload,
    isKnownPluginFrame,
    handlePluginMessage
  }
}
