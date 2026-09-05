import type { Page } from '../types/api'

interface DataEnvelope<T> {
  data: T
}

interface PageEnvelope<T> extends DataEnvelope<T[]> {
  meta: { next_cursor: string | null }
}

interface ErrorEnvelope {
  error: { code: string; message: string }
}

export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number | null,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

const knownErrorCodes = new Set([
  'validation_failed',
  'authentication_required',
  'permission_denied',
  'invalid_credentials',
  'username_conflict',
  'post_not_found',
  'notification_not_found',
  'search_unavailable',
  'metrics_unavailable',
  'logs_unavailable',
  'events_unavailable',
  'plugin_package_invalid',
  'plugin_not_found',
  'plugin_conflict',
  'plugin_operation_in_progress',
  'plugin_operation_failed',
  'monitor_unavailable',
  'internal_error',
])

type UnauthorizedHandler = () => void | Promise<void>
type ForbiddenHandler = () => void | Promise<void>
let forbiddenHandler: ForbiddenHandler = () => undefined
let unauthorizedHandler: UnauthorizedHandler = () => undefined

export function setForbiddenHandler(handler: ForbiddenHandler): void { forbiddenHandler = handler }

export function setUnauthorizedHandler(handler: UnauthorizedHandler): void {
  unauthorizedHandler = handler
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
function hasExactKeys(value: Record<string, unknown>, keys: string[]): boolean {
  const actual = Object.keys(value).sort(); const expected = [...keys].sort()
  return actual.length === expected.length && actual.every((key, index) => key === expected[index])
}

async function parseJSON(response: Response): Promise<unknown> {
  try {
    return await response.json()
  } catch {
    throw new ApiError('invalid_response', '服务器返回了无法识别的响应。', response.status)
  }
}

async function request(path: string, init: RequestInit = {}): Promise<Response> {
  let response: Response
  try {
    response = await fetch(`/api/v1${path}`, {
      ...init,
      credentials: 'include',
      headers: {
        ...(init.body === undefined || (typeof FormData !== 'undefined' && init.body instanceof FormData) ? {} : { 'Content-Type': 'application/json' }),
        ...init.headers,
      },
    })
  } catch {
    throw new ApiError('network_error', '网络连接失败，请稍后重试。', null)
  }

  if (!response.ok) {
    const body = await parseJSON(response)
    if (isRecord(body) && isRecord(body.error)) {
      const code = body.error.code
      const message = body.error.message
      if (typeof code === 'string' && typeof message === 'string') {
        if (response.status === 401 && code === 'authentication_required') await unauthorizedHandler()
        if (response.status === 403 && code === 'permission_denied') await forbiddenHandler()
        throw new ApiError(
          code,
          knownErrorCodes.has(code) ? message : '操作失败，请稍后重试。',
          response.status,
        )
      }
    }
    throw new ApiError('invalid_response', '请求失败，请稍后重试。', response.status)
  }
  return response
}

export async function requestData<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await request(path, init)
  const body = await parseJSON(response)
  if (!isRecord(body) || !('data' in body)) {
    throw new ApiError('invalid_response', '服务器返回了无法识别的数据。', response.status)
  }
  return (body as unknown as DataEnvelope<T>).data
}

export async function requestValidatedData<T>(
  path: string,
  validate: (value: unknown) => value is T,
  init: RequestInit = {},
): Promise<T> {
  const response = await request(path, init)
  const body = await parseJSON(response)
  if (!isRecord(body) || !hasExactKeys(body, ['data']) || !validate(body.data)) {
    throw new ApiError('invalid_response', '服务器返回了无法识别的数据。', response.status)
  }
  return body.data
}

export async function requestPage<T>(path: string, init: RequestInit = {}): Promise<Page<T>> {
  const response = await request(path, init)
  const body = await parseJSON(response)
  if (!isRecord(body) || !hasExactKeys(body, ['data', 'meta']) || !Array.isArray(body.data) || !isRecord(body.meta) || !hasExactKeys(body.meta, ['next_cursor'])) {
    throw new ApiError('invalid_response', '服务器返回了无法识别的分页数据。', response.status)
  }
  const cursor = body.meta.next_cursor
  if (cursor !== null && typeof cursor !== 'string') {
    throw new ApiError('invalid_response', '服务器返回了无效的分页游标。', response.status)
  }
  const page = body as unknown as PageEnvelope<T>
  return { data: page.data, nextCursor: page.meta.next_cursor }
}

export async function requestValidatedPage<T>(
  path: string,
  validateItem: (value: unknown) => value is T,
  init: RequestInit = {},
): Promise<Page<T>> {
  const response = await request(path, init)
  const body = await parseJSON(response)
  if (!isRecord(body) || !hasExactKeys(body, ['data', 'meta']) || !Array.isArray(body.data) || !body.data.every(validateItem) || !isRecord(body.meta) || !hasExactKeys(body.meta, ['next_cursor'])) {
    throw new ApiError('invalid_response', '服务器返回了无法识别的分页数据。', response.status)
  }
  const cursor = body.meta.next_cursor
  if (cursor !== null && typeof cursor !== 'string') {
    throw new ApiError('invalid_response', '服务器返回了无效的分页游标。', response.status)
  }
  return { data: body.data, nextCursor: cursor }
}

export async function requestVoid(path: string, init: RequestInit = {}): Promise<void> {
  const response = await request(path, init)
  if (response.status !== 204) {
    throw new ApiError('invalid_response', '服务器返回了非预期状态。', response.status)
  }
}
