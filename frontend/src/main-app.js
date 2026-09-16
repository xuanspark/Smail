import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { useGlobalStatusStore } from './stores/useGlobalStatusStore'

import './styles/element-services.css'
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

// Hash History 会将 /reset#/ 解析为普通首页，启动前先切断这个回退路径。
if (isReservedResetPath(window.location.pathname)) {
  window.history.replaceState({}, document.title, '/')
  window.location.hash = '#/reset-blocked'
}

const app = createApp(App)
const pinia = createPinia()
const globalStatus = useGlobalStatusStore(pinia)

app.use(pinia)

router.onError((error) => {
  console.error('Router navigation failed', error)
  globalStatus.setStartupError('页面加载失败，请刷新后重试')
})

app.use(router)
app.mount('#app')
