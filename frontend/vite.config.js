/* eslint-env node */
import { readFileSync } from 'node:fs'
import { createServer as createHTTPServer } from 'node:http'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import vue from '@vitejs/plugin-vue'
import { readDevelopmentRuntime } from '../deploy/runtime-config.mjs'

const developmentHTTPRedirect = (frontendOrigin, host = '127.0.0.1', port = 80) => ({
  name: 'smail-development-http-redirect',
  apply: 'serve',
  async configureServer(server) {
    const redirectServer = createHTTPServer((request, response) => {
      const requestURL = new URL(request.url || '/', 'http://127.0.0.1')
      const location = new URL(`${requestURL.pathname}${requestURL.search}`, frontendOrigin)
      response.writeHead(308, {
        Location: location.toString(),
        'Cache-Control': 'no-store'
      })
      response.end()
    })

    await new Promise((resolve, reject) => {
      redirectServer.once('error', reject)
      redirectServer.listen(port, host, resolve)
    })
    server.httpServer?.once('close', () => redirectServer.close())
  }
})

// https://vitejs.dev/config/
export default defineConfig(({ mode, command }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const backendUrl = (env.VITE_DEV_BACKEND_URL || 'http://127.0.0.1:5201').replace(/\/$/, '')
  const runtime = command === 'serve' ? readDevelopmentRuntime() : undefined
  const backendOrigin = runtime?.origin
  const frontendOrigin = runtime?.origin

  return {
    plugins: [
      developmentHTTPRedirect(frontendOrigin),
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
      host: '127.0.0.1',
      port: 443,
      strictPort: true,
      // 生产构建不依赖本机开发证书，只有开发服务器启动时才读取私钥。
      https: command === 'serve' ? {
        cert: readFileSync(runtime.certificatePath),
        key: readFileSync(runtime.privateKeyPath)
      } : undefined,
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
