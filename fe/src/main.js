import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import {router} from './router'

import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import './assets/main.css'

const isReservedResetPath = (pathname) => {
  let decoded = pathname.trim()
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const next = decodeURIComponent(decoded)
      if (next === decoded) break
      decoded = next
    } catch {
      break
    }
  }
  const normalized = decoded.replace(/\/+$/, '').toLowerCase()
  return normalized === '/reset' || normalized.startsWith('/reset/')
}

// Hash History 会把 /reset#/ 解析成普通首页，启动前先切断这个回退路径。
if (isReservedResetPath(window.location.pathname)) {
  window.history.replaceState({}, document.title, '/')
  window.location.hash = '#/reset-blocked'
}

const app = createApp(App)
app.use(router)
app.use(createPinia())
app.use(ElementPlus) 
app.mount('#app')
