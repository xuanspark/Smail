<template>
  <div :inert="callbackWorking || undefined">
    <ConfigEditor />
  </div>
  <TelegramCallbackSettings
    v-if="pageKey === 'telegram_push' && store.role === 'super_admin'"
    @working="callbackWorking = $event"
  />
</template>

<script setup>
import { provide, ref } from 'vue'
import { useGlobalStatusStore } from '@/stores/useGlobalStatusStore'
import { usePage } from '../ConfigSettings.js'
import ConfigEditor from '../ConfigEditor/ConfigEditor.vue'
import TelegramCallbackSettings from './TelegramCallbackSettings/TelegramCallbackSettings.vue'

const props = defineProps({ pageKey: { type: String, required: true } })
const page = usePage(props.pageKey)
const store = useGlobalStatusStore()
const callbackWorking = ref(false)
provide('smail-config-page', page)
defineExpose({ beforeLeave: () => callbackWorking.value ? false : page.beforeLeave() })
</script>
