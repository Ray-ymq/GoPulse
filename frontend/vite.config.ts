import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import { loadEnv } from 'vite'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

const configDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(configDirectory, '..')

export function backendTarget(environment: Record<string, string | undefined>): string {
  const rawPort = environment.HTTP_PORT?.trim() || '8080'
  if (!/^\d+$/.test(rawPort)) {
    throw new Error('HTTP_PORT must be an integer from 1 to 65535')
  }
  const port = Number(rawPort)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error('HTTP_PORT must be an integer from 1 to 65535')
  }
  return `http://localhost:${port}`
}

export function backendProxyConfig(environment: Record<string, string | undefined>) {
  const target = backendTarget(environment)
  const proxy = () => ({ target, changeOrigin: true })
  return {
    '/health': proxy(),
    '/ready': proxy(),
    '/api/v1': proxy(),
  }
}

export default defineConfig(({ mode }) => {
  const loadedEnvironment = loadEnv(mode, repositoryRoot, '')
  const environment = {
    HTTP_PORT: process.env.HTTP_PORT ?? loadedEnvironment.HTTP_PORT,
  }

  return {
    plugins: [vue()],
    server: {
      host: 'localhost',
      port: 5173,
      strictPort: true,
      proxy: backendProxyConfig(environment),
    },
    test: {
      environment: 'jsdom',
      globals: true,
      clearMocks: true,
      restoreMocks: true,
    },
  }
})
