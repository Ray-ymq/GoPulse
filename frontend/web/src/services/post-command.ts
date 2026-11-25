import { ApiError, request } from './http'
import type { CommandResult, Data, PendingPost, PostInput } from '../types/api'
const storageKey = 'gopulse.pending-post.v1'
let pending: PendingPost | null = null
try { const value = JSON.parse(localStorage.getItem(storageKey) || 'null'); if (value && Number.isSafeInteger(value.owner) && typeof value.key === 'string' && typeof value.epoch === 'string' && typeof value.input?.title === 'string' && typeof value.input?.content === 'string') pending = value } catch { /* 存储不可用时，在当前页面保留发布记录。 */ }
function persist() { try { if (pending) localStorage.setItem(storageKey, JSON.stringify(pending)); else localStorage.removeItem(storageKey) } catch { /* 不因浏览器存储故障丢弃当前页面中的发布记录。 */ } }
export function pendingPost(owner: number) { return pending?.owner === owner ? pending : null }
function headers(p: PendingPost) { return { 'Idempotency-Key': p.key, 'X-GoPulse-Data-Epoch': p.epoch } }
export async function publish(owner: number, input: PostInput): Promise<CommandResult> {
  if (pending && pending.owner !== owner) throw new ApiError(409, 'pending_other_user', '此浏览器保留着另一个账号的未确认发布，请切回原账号查证。')
  if (!pending) {
    const context = await request<Data<{ data_epoch: string }>>('/post-commands/context')
    pending = { owner, key: crypto.randomUUID(), epoch: context.data.data_epoch, input: { ...input } }; persist()
  }
  if (pending.input.title !== input.title || pending.input.content !== input.content) throw new ApiError(409, 'pending_changed', '请先查证上次发布，或使用原内容重试。')
  try {
    const result = await request<CommandResult>('/posts', { method: 'POST', body: pending.input, headers: headers(pending) })
    pending = null; persist(); return result
  } catch (error) {
    if (error instanceof ApiError && [400, 403, 409, 429].includes(error.status) && error.code !== 'idempotency_conflict') { pending = null; persist() }
    throw error
  }
}
export async function resolvePost(owner: number): Promise<CommandResult> {
  const p = pendingPost(owner); if (!p) throw new Error('没有需要查证的发布记录。')
  const result = await request<CommandResult>('/post-commands/result', { headers: headers(p) })
  pending = null; persist(); return result
}
