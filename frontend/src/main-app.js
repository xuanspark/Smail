import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { useGlobalStatusStore } from './stores/useGlobalStatusStore'
import { ElMessage } from 'element-plus'
import { createRouteAssetRecovery, isReadOnlyRoute, isRouteAssetLoadError } from './utils/navigation.mjs'

import './styles/element-services.css'
import './assets/main.css'
import './styles/action-layout.css'

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

const recoverRouteAsset = createRouteAssetRecovery({
  buildID: import.meta.url,
  getStorage: () => window.sessionStorage,
  reload: fullPath => {
    // 只恢复页面导航，不重放发送邮件或保存配置等业务请求。
    window.history.replaceState(window.history.state, '', `#${fullPath}`)
    window.location.reload()
  }
})

router.onError((error, to, from) => {
  console.error('Router navigation failed', error)
  const editing = globalStatus.settingsDrawerVisible || globalStatus.userInfos.securitySetupRequired
  if (recoverRouteAsset(error, to, from, {editing, online: window.navigator.onLine})) {
    return
  }
  if (isRouteAssetLoadError(error) && (editing || !isReadOnlyRoute(from)) && from?.name) {
    // 保留编辑器或配置表单，不能用错误页卸载尚未保存的内容。
    ElMessage.error('页面资源加载失败，请先保存当前内容，再刷新页面。')
    return
  }
  globalStatus.setStartupError('页面加载失败，请刷新后重试')
})

app.use(router)
app.mount('#app')
