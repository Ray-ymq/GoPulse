import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'

const healthyResponse = {
  status: 'ready',
  service: 'backend',
  checks: { mysql: 'up', redis: 'up', rabbitmq: 'up' },
}

const healthResponse = { status: 'ok', service: 'backend' }

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function pathOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.pathname : input.url
}

function statusOf(wrapper: ReturnType<typeof mount>, service: string): string | undefined {
  return wrapper.get(`[data-testid="status-${service}"]`).attributes('data-status')
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App connectivity dashboard', () => {
  it('shows loading while the initial requests are pending', async () => {
    let resolveHealth!: (response: Response) => void
    let resolveReadiness!: (response: Response) => void
    const healthPromise = new Promise<Response>((resolve) => {
      resolveHealth = resolve
    })
    const readinessPromise = new Promise<Response>((resolve) => {
      resolveReadiness = resolve
    })

    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        pathOf(input) === '/health' ? healthPromise : readinessPromise,
      ),
    )

    const wrapper = mount(App)
    await wrapper.vm.$nextTick()

    expect(statusOf(wrapper, 'backend')).toBe('loading')
    expect(statusOf(wrapper, 'mysql')).toBe('loading')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button').text()).toContain('正在刷新')

    resolveHealth(jsonResponse(healthResponse))
    resolveReadiness(jsonResponse(healthyResponse))
    await flushPromises()
    wrapper.unmount()
  })

  it('shows all services as up when both contracts are healthy', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          pathOf(input) === '/health'
            ? jsonResponse(healthResponse)
            : jsonResponse(healthyResponse),
        ),
      ),
    )

    const wrapper = mount(App)
    await flushPromises()

    expect(statusOf(wrapper, 'backend')).toBe('up')
    expect(statusOf(wrapper, 'mysql')).toBe('up')
    expect(statusOf(wrapper, 'redis')).toBe('up')
    expect(statusOf(wrapper, 'rabbitmq')).toBe('up')
    expect(wrapper.find('.diagnostics').exists()).toBe(false)
  })

  it('parses a 503 readiness response and marks each failed dependency', async () => {
    const notReadyResponse = {
      status: 'not_ready',
      service: 'backend',
      checks: { mysql: 'down', redis: 'up', rabbitmq: 'down' },
    }

    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          pathOf(input) === '/health'
            ? jsonResponse(healthResponse)
            : jsonResponse(notReadyResponse, 503),
        ),
      ),
    )

    const wrapper = mount(App)
    await flushPromises()

    expect(statusOf(wrapper, 'backend')).toBe('up')
    expect(statusOf(wrapper, 'mysql')).toBe('down')
    expect(statusOf(wrapper, 'redis')).toBe('up')
    expect(statusOf(wrapper, 'rabbitmq')).toBe('down')
  })

  it('does not trust readiness when the backend health endpoint is unreachable', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        pathOf(input) === '/health'
          ? Promise.reject(new TypeError('connection refused'))
          : Promise.resolve(jsonResponse(healthyResponse)),
      ),
    )

    const wrapper = mount(App)
    await flushPromises()

    expect(statusOf(wrapper, 'backend')).toBe('unreachable')
    expect(statusOf(wrapper, 'mysql')).toBe('unknown')
    expect(statusOf(wrapper, 'redis')).toBe('unknown')
    expect(statusOf(wrapper, 'rabbitmq')).toBe('unknown')
    expect(wrapper.get('.diagnostics').text()).toContain('无法访问 /health')
    expect(wrapper.get('.diagnostics').text()).not.toContain('connection refused')
  })

  it.each([
    ['network failure', () => Promise.reject(new TypeError('offline'))],
    ['unexpected status', () => Promise.resolve(jsonResponse({ error: 'failure' }, 500))],
    ['invalid contract', () => Promise.resolve(jsonResponse({ status: 'ready', checks: {} }))],
  ])(
    'keeps backend up and marks dependencies unknown for readiness %s',
    async (_name, readinessRequest) => {
      vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL) =>
          pathOf(input) === '/health'
            ? Promise.resolve(jsonResponse(healthResponse))
            : readinessRequest(),
        ),
      )

      const wrapper = mount(App)
      await flushPromises()

      expect(statusOf(wrapper, 'backend')).toBe('up')
      expect(statusOf(wrapper, 'mysql')).toBe('unknown')
      expect(statusOf(wrapper, 'redis')).toBe('unknown')
      expect(statusOf(wrapper, 'rabbitmq')).toBe('unknown')
      expect(wrapper.find('.diagnostics').exists()).toBe(true)
    },
  )

  it('marks a malformed health response as invalid', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          pathOf(input) === '/health'
            ? jsonResponse({ status: 'ok' })
            : jsonResponse(healthyResponse),
        ),
      ),
    )

    const wrapper = mount(App)
    await flushPromises()

    expect(statusOf(wrapper, 'backend')).toBe('invalid')
    expect(statusOf(wrapper, 'mysql')).toBe('unknown')
    expect(wrapper.get('.diagnostics').text()).toContain('/health 响应不符合约定')
  })

  it('refreshes statuses, disables duplicate submission, and applies the new result', async () => {
    let healthCalls = 0
    let readinessCalls = 0
    let resolveHealth!: (response: Response) => void
    let resolveReadiness!: (response: Response) => void

    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (pathOf(input) === '/health') {
          healthCalls += 1
          if (healthCalls === 1) {
            return Promise.resolve(jsonResponse(healthResponse))
          }
          return new Promise<Response>((resolve) => {
            resolveHealth = resolve
          })
        }

        readinessCalls += 1
        if (readinessCalls === 1) {
          return Promise.resolve(
            jsonResponse(
              {
                status: 'not_ready',
                service: 'backend',
                checks: { mysql: 'up', redis: 'down', rabbitmq: 'up' },
              },
              503,
            ),
          )
        }
        return new Promise<Response>((resolve) => {
          resolveReadiness = resolve
        })
      }),
    )

    const wrapper = mount(App)
    await flushPromises()
    expect(statusOf(wrapper, 'redis')).toBe('down')

    await wrapper.get('button').trigger('click')

    expect(statusOf(wrapper, 'backend')).toBe('loading')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.get('button').trigger('click')
    expect(healthCalls).toBe(2)
    expect(readinessCalls).toBe(2)

    resolveHealth(jsonResponse(healthResponse))
    resolveReadiness(jsonResponse(healthyResponse))
    await flushPromises()

    expect(statusOf(wrapper, 'backend')).toBe('up')
    expect(statusOf(wrapper, 'redis')).toBe('up')
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
  })
})
