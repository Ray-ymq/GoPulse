import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchHealth, fetchReadiness, REQUEST_TIMEOUT_MS } from './connectivity'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('connectivity service', () => {
  it('aborts a request after the three second client timeout', async () => {
    vi.useFakeTimers()
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('Aborted', 'AbortError'))
          })
        }),
      ),
    )

    const resultPromise = fetchHealth()
    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS)

    await expect(resultPromise).resolves.toEqual({
      type: 'unreachable',
      message: '/health 请求超过 3 秒',
    })
  })

  it('accepts 503 as a valid readiness transport status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          jsonResponse(
            {
              status: 'not_ready',
              service: 'backend',
              checks: { mysql: 'up', redis: 'down', rabbitmq: 'up' },
            },
            503,
          ),
        ),
      ),
    )

    await expect(fetchReadiness()).resolves.toMatchObject({
      type: 'success',
      data: {
        status: 'not_ready',
        checks: { redis: 'down' },
      },
    })
  })

  it('rejects an inconsistent readiness status and body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          jsonResponse({
            status: 'not_ready',
            service: 'backend',
            checks: { mysql: 'up', redis: 'up', rabbitmq: 'up' },
          }),
        ),
      ),
    )

    await expect(fetchReadiness()).resolves.toEqual({
      type: 'invalid',
      message: '/ready 响应不符合约定',
    })
  })
})
