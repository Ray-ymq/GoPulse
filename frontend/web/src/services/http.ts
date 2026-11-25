export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public requestId?: string) { super(message) }
}
const messages: Record<string, string> = {
  bootstrap_super_admin_protected: '初始超级管理员的权限不能撤销。',
  management_setup_unavailable: '管理员初始化尚未完成或状态不可用，请联系系统维护者。',
  authentication_required: '登录状态已失效，请重新登录。', invalid_credentials: '用户名或密码不正确。',
  username_conflict: '这个用户名已被使用。', permission_denied: '当前账号没有执行此操作的权限。',
  post_not_found: '这篇帖子已被删除或不存在。', user_not_found: '这位用户不存在。',
  search_unavailable: '搜索暂时不可用，请稍后重试。', backend_busy: '服务繁忙，请稍后重试。',
  rate_limited: '操作过于频繁，请稍后重试。', internal_error: '服务暂时不可用，请稍后重试。',
  data_epoch_mismatch: '服务数据已更新，本次发布未能确认，请重新检查内容后发布。',
  idempotency_conflict: '发布记录发生冲突，请查证已有发布结果。',
  command_result_unknown: '暂未查到发布结果。可以继续查证，或使用原记录重试。',
  command_outcome_unknown: '发布结果尚未确认，请查证结果后再继续。',
  business_async_backlog: '服务正在处理积压任务，请稍后重试。',
}
export async function request<T>(path: string, options: { method?: string; body?: unknown; headers?: Record<string, string>; signal?: AbortSignal } = {}): Promise<T> {
  const timeout = AbortSignal.timeout(12000)
  let response: Response
  try {
    response = await fetch(`/api/v1${path}`, {
      method: options.method || 'GET', credentials: 'include', cache: 'no-store',
      headers: { Accept: 'application/json', ...(options.body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...options.headers },
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal ? AbortSignal.any([timeout, options.signal]) : timeout,
    })
  } catch (error) {
    if (options.signal?.aborted) throw error
    throw new ApiError(0, 'network_error', '无法连接服务或请求超时。请检查网络后重试。')
  }
  if (response.status === 204) return undefined as T
  let payload: unknown
  try { payload = await response.json() } catch { throw new ApiError(response.status, 'invalid_response', '服务返回了无法识别的响应，请稍后重试。') }
  if (!response.ok) {
    const detail = (payload as { error?: { code?: string; message?: string; request_id?: string } })?.error
    const code = detail?.code || 'unknown_error'
    if (code === 'authentication_required') window.dispatchEvent(new Event('gopulse:unauthorized'))
    throw new ApiError(response.status, code, messages[code] || (code === 'validation_failed' ? '提交内容不符合要求，请检查输入。' : '操作未完成，请稍后重试。'), detail?.request_id)
  }
  if (!payload || typeof payload !== 'object' || !('data' in payload)) throw new ApiError(response.status, 'invalid_response', '服务响应缺少数据，请稍后重试。')
  return payload as T
}
export function errorMessage(error: unknown): string { return error instanceof Error ? error.message : '操作未完成，请稍后重试。' }
