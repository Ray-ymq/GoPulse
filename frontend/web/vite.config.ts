import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '../..', '')
  return {
    plugins: [vue()],
    server: {
      port: Number(env.FRONTEND_PORT || 5173),
      strictPort: true,
      proxy: {
        '/api': {
          target: env.GOPULSE_API_TARGET || `http://127.0.0.1:${env.HTTP_PORT || 8080}`,
          changeOrigin: false,
        },
      },
    },
    build: { sourcemap: false },
  }
})
