import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const xzTarget = env.VITE_XZ_TARGET || 'http://localhost:8080'
  const aisaasTarget = env.VITE_AISAAS_TARGET || 'http://localhost:8190'

  return {
    base: '/xiaozhi-admin/',
    plugins: [vue()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    server: {
      port: 8082,
      host: '0.0.0.0',
      proxy: {
        '/api/xz': {
          target: xzTarget,
          changeOrigin: true,
          rewrite: (p) => p.replace(/^\/api\/xz/, '')
        },
        '/api/aisaas': {
          target: aisaasTarget,
          changeOrigin: true,
          rewrite: (p) => p.replace(/^\/api\/aisaas/, '')
        },
        '/internal/api': {
          target: aisaasTarget,
          changeOrigin: true
        },
        '/v1': {
          target: aisaasTarget,
          changeOrigin: true
        },
        '/firmware': {
          target: xzTarget,
          changeOrigin: true
        },
        '/healthz': { target: xzTarget, changeOrigin: true },
        '/readyz': { target: xzTarget, changeOrigin: true },
        '/metrics': { target: xzTarget, changeOrigin: true }
      }
    }
  }
})