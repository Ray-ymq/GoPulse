import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import { loadEnv, type Plugin } from 'vite'
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

function configuredPort(environment: Record<string, string | undefined>, key: string, fallback: number): number {
  const rawPort = environment[key]?.trim() || String(fallback)
  if (!/^\d+$/.test(rawPort)) {
    throw new Error(`${key} must be an integer from 1 to 65535`)
  }
  const port = Number(rawPort)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`${key} must be an integer from 1 to 65535`)
  }
  return port
}

export function frontendPort(environment: Record<string, string | undefined>): number {
  return configuredPort(environment, 'FRONTEND_PORT', 5173)
}

export function adminTarget(environment: Record<string, string | undefined>): string {
  return `http://localhost:${configuredPort(environment, 'ADMIN_FRONTEND_PORT', 5174)}`
}

function adminRootRedirectPlugin(): Plugin {
  return {
    name: 'gopulse-admin-root-redirect',
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const rawURL = request.url ?? ''
        const [path, query] = rawURL.split('?', 2)
        const redirects: Record<string, string> = {
          '/admin': '/admin/',
          '/admin/observability': '/admin/',
          '/admin/observability/': '/admin/',
          '/admin/observability/metrics': '/admin/metrics',
          '/admin/observability/metrics/': '/admin/metrics',
          '/admin/observability/logs': '/admin/logs',
          '/admin/observability/logs/': '/admin/logs',
          '/admin/observability/events': '/admin/events',
          '/admin/observability/events/': '/admin/events',
          '/admin/observability/exporters': '/admin/plugins',
          '/admin/observability/exporters/': '/admin/plugins',
        }
        const target = redirects[path]
        if (request.method === 'GET' && target) {
          response.statusCode = 308
          response.setHeader('Location', `${target}${query ? `?${query}` : ''}`)
          response.end()
          return
        }
        next()
      })
    },
  }
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
    FRONTEND_PORT: process.env.FRONTEND_PORT ?? loadedEnvironment.FRONTEND_PORT,
    ADMIN_FRONTEND_PORT: process.env.ADMIN_FRONTEND_PORT ?? loadedEnvironment.ADMIN_FRONTEND_PORT,
  }

  return {
    plugins: [vue(), adminRootRedirectPlugin()],
    resolve: { dedupe: ['vue'], alias: { vue: resolve(configDirectory, 'node_modules/vue') } },
    server: {
      host: 'localhost',
      port: frontendPort(environment),
      strictPort: true,
      proxy: { ...backendProxyConfig(environment), '/admin': { target: adminTarget(environment), changeOrigin: false } },
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
