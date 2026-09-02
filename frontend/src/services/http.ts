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
  'invalid_credentials',
  'username_conflict',
  'post_not_found',
  'notification_not_found',
  'search_unavailable',
  'internal_error',
])

type UnauthorizedHandler = () => void | Promise<void>
let unauthorizedHandler: UnauthorizedHandler = () => undefined

export function setUnauthorizedHandler(handler: UnauthorizedHandler): void {
  unauthorizedHandler = handler
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
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
        ...(init.body === undefined ? {} : { 'Content-Type': 'application/json' }),
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
        if (response.status === 401 && code === 'authentication_required') {
          await unauthorizedHandler()
        }
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

export async function requestPage<T>(path: string, init: RequestInit = {}): Promise<Page<T>> {
  const response = await request(path, init)
  const body = await parseJSON(response)
  if (!isRecord(body) || !Array.isArray(body.data) || !isRecord(body.meta)) {
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
  if (!isRecord(body) || !Array.isArray(body.data) || !body.data.every(validateItem) || !isRecord(body.meta)) {
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
