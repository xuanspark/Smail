/* eslint-env node */
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const backendUrl = (env.VITE_DEV_BACKEND_URL || 'https://127.0.0.1:443').replace(/\/$/, '')
  const backendOrigin = (env.VITE_DEV_BACKEND_ORIGIN || 'https://xuans.com').replace(/\/$/, '')

  const rewriteDevelopmentCookies = (proxyResponse) => {
    const setCookies = proxyResponse.headers['set-cookie']
    if (!Array.isArray(setCookies)) {
      return
    }

    proxyResponse.headers['set-cookie'] = setCookies.map((cookie) => (
      cookie
        .replace(/;\s*Secure/gi, '')
        .replace(/;\s*Domain=[^;]*/gi, '')
    ))
  }

  return {
    plugins: [
      vue(),
      AutoImport({
        resolvers: [ElementPlusResolver()],
      }),
      Components({
        resolvers: [ElementPlusResolver()],
      }),
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      },
    },
    server: {
      cors: true,
      proxy: {
        "/api": {
          target: backendUrl,
          changeOrigin: true,
          secure: false,
          configure: (proxy) => {
            proxy.on('proxyReq', (proxyRequest) => {
              const originURL = new URL(backendOrigin)
              proxyRequest.setHeader('origin', backendOrigin)
              proxyRequest.setHeader('host', originURL.host)
              proxyRequest.setHeader('x-forwarded-proto', originURL.protocol.replace(':', ''))
              proxyRequest.removeHeader('referer')
            })
            proxy.on('proxyRes', rewriteDevelopmentCookies)
          }
        },
        "/attachments": {
          target: backendUrl,
          changeOrigin: true,
          secure: false
        },
        "/__smail_setup_bootstrap": {
          target: backendUrl,
          changeOrigin: true,
          secure: false,
          rewrite: () => "/"
        }
      }
    },
    build: {
      outDir: fileURLToPath(new URL('../out/frontend', import.meta.url)),
      emptyOutDir: false,
      manifest: true,
      rollupOptions: {
        output: {
          manualChunks(id) {
            const modulePath = id.replaceAll('\\', '/')
            if (!modulePath.includes('/node_modules/')) return
            if (/\/node_modules\/(?:@vue\/|vue\/|vue-router\/|pinia\/)/.test(modulePath)) {
              return 'vue-core'
            }
            // 编辑器仅在写信页面加载；按包拆分其依赖，避免单个异步块超过浏览器缓存阈值。
            const packagePath = modulePath.split('/node_modules/')[1]
            const packageParts = packagePath.split('/')
            const packageName = packageParts[0].startsWith('@')
              ? `${packageParts[0]}-${packageParts[1]}`
              : packageParts[0]
            if (packageName.startsWith('@wangeditor-')) {
              return `editor-${packageName.slice('@wangeditor-'.length)}`
            }
            if (['@uppy-core', '@uppy-xhr-upload', 'slate', 'snabbdom'].includes(packageName)) {
              return `editor-${packageName.replace(/^@uppy-/, '')}`
            }
          }
        }
      }
    }
  }
})
