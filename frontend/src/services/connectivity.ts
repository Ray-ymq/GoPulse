import type {
  ApiResult,
  HealthResponse,
  ReadinessChecks,
  ReadinessResponse,
} from '../types/connectivity'

export const REQUEST_TIMEOUT_MS = 3_000

const healthPath = '/health'
const readinessPath = '/ready'

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isHealthResponse(value: unknown): value is HealthResponse {
  return isRecord(value) && value.status === 'ok' && value.service === 'backend'
}

function isDependencyStatus(value: unknown): value is ReadinessChecks[keyof ReadinessChecks] {
  return value === 'up' || value === 'down'
}

function isReadinessResponse(value: unknown, statusCode: number): value is ReadinessResponse {
  if (!isRecord(value) || !isRecord(value.checks)) {
    return false
  }

  const checks = value.checks
  if (
    value.service !== 'backend' ||
    (value.status !== 'ready' && value.status !== 'not_ready') ||
    !isDependencyStatus(checks.mysql) ||
    !isDependencyStatus(checks.redis) ||
    !isDependencyStatus(checks.rabbitmq)
  ) {
    return false
  }

  const allDependenciesUp =
    checks.mysql === 'up' && checks.redis === 'up' && checks.rabbitmq === 'up'

  if (statusCode === 200) {
    return value.status === 'ready' && allDependenciesUp
  }

  return statusCode === 503 && value.status === 'not_ready' && !allDependenciesUp
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

async function requestJson<T>(
  path: string,
  acceptedStatuses: readonly number[],
  validate: (value: unknown, statusCode: number) => value is T,
): Promise<ApiResult<T>> {
  const controller = new AbortController()
  const timeout = globalThis.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)

  try {
    const response = await fetch(path, {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      signal: controller.signal,
    })

    if (!acceptedStatuses.includes(response.status)) {
      return {
        type: 'invalid',
        message: `${path} 返回了非预期状态码 ${response.status}`,
      }
    }

    let payload: unknown
    try {
      payload = await response.json()
    } catch {
      return {
        type: 'invalid',
        message: `${path} 未返回有效 JSON`,
      }
    }

    if (!validate(payload, response.status)) {
      return {
        type: 'invalid',
        message: `${path} 响应不符合约定`,
      }
    }

    return { type: 'success', data: payload }
  } catch (error) {
    if (isAbortError(error)) {
      return {
        type: 'unreachable',
        message: `${path} 请求超过 ${REQUEST_TIMEOUT_MS / 1_000} 秒`,
      }
    }

    return {
      type: 'unreachable',
      message: `无法访问 ${path}`,
    }
  } finally {
    globalThis.clearTimeout(timeout)
  }
}

export function fetchHealth(): Promise<ApiResult<HealthResponse>> {
  return requestJson(healthPath, [200], isHealthResponse)
}

export function fetchReadiness(): Promise<ApiResult<ReadinessResponse>> {
  return requestJson(readinessPath, [200, 503], isReadinessResponse)
}
