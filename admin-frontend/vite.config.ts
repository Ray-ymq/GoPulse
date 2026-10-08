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

export function frontendPort(environment: Record<string, string | undefined>): number {
  const rawPort = environment.FRONTEND_PORT?.trim() || '5174'
  if (!/^\d+$/.test(rawPort)) {
    throw new Error('FRONTEND_PORT must be an integer from 1 to 65535')
  }
  const port = Number(rawPort)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error('FRONTEND_PORT must be an integer from 1 to 65535')
  }
  return port
}

export function backendProxyConfig(environment: Record<string, string | undefined>) {
  const target = backendTarget(environment)
  const proxy = () => ({ target, changeOrigin: false })
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
    FRONTEND_PORT: process.env.ADMIN_FRONTEND_PORT ?? process.env.FRONTEND_PORT ?? loadedEnvironment.ADMIN_FRONTEND_PORT ?? loadedEnvironment.FRONTEND_PORT,
  }

  return {
    base: '/admin/',
    plugins: [vue()],
    resolve: { dedupe: ['vue'], alias: { vue: resolve(configDirectory, 'node_modules/vue') } },
    server: {
      host: 'localhost',
      port: frontendPort(environment),
      strictPort: true,
      proxy: backendProxyConfig(environment),
    },
    test: {
      environment: 'jsdom',
      globals: true,
      clearMocks: true,
      restoreMocks: true,
      include: ['src/**/*.test.ts', 'vite.config.test.ts'],
    },
  }
})
