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
      rollupOptions: {
        output: {
          manualChunks: {
            'vue-core': ['vue', 'vue-router', 'pinia'],
            'element-plus': ['element-plus'],
            'editor': ['@wangeditor/editor', '@wangeditor/editor-for-vue'],
            'icons': ['@element-plus/icons-vue', 'vue-icons-plus']
          }
        }
      }
    }
  }
})
