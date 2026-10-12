import { computed, ref, type Ref } from 'vue'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'
import type { CreatePostInput, Post, PostCommandContext, PostCommandMeta } from '../types/api'

const storagePrefix = 'gopulse.post-command.v1:'

interface StoredDraft {
  user_id: number
  data_epoch: string
  idempotency_key: string
  title: string
  content: string
  digest: string
  first_sent_at: string
  retry_window_until: string
}

function storageKey(userID: number): string { return `${storagePrefix}${userID}` }

function storage(): Storage | null {
  try { return typeof sessionStorage === 'undefined' ? null : sessionStorage } catch { return null }
}

function readDraft(userID: number | null): StoredDraft | null {
  if (!userID) return null
  const store = storage()
  if (!store) return null
  try {
    const raw = store.getItem(storageKey(userID))
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<StoredDraft>
    if (parsed.user_id !== userID || typeof parsed.data_epoch !== 'string' || typeof parsed.idempotency_key !== 'string'
      || typeof parsed.title !== 'string' || typeof parsed.content !== 'string' || typeof parsed.digest !== 'string'
      || typeof parsed.first_sent_at !== 'string' || typeof parsed.retry_window_until !== 'string') return null
    return parsed as StoredDraft
  } catch { return null }
}

function writeDraft(draft: StoredDraft | null): void {
  if (!draft) return
  const store = storage()
  if (!store) return
  try { store.setItem(storageKey(draft.user_id), JSON.stringify(draft)) } catch { /* session storage is best effort */ }
}

function deleteDraft(userID: number | null): void {
  if (!userID) return
  const store = storage()
  if (!store) return
  try { store.removeItem(storageKey(userID)) } catch { /* session storage is best effort */ }
}

function randomUUID(): string {
  const candidate = globalThis.crypto?.randomUUID
  if (candidate) return candidate.call(globalThis.crypto)
  const bytes = Array.from({ length: 16 }, () => Math.floor(Math.random() * 256))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = bytes.map(value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

async function digestInput(input: CreatePostInput): Promise<string> {
  const payload = `${input.title.length}:${input.title}${input.content.length}:${input.content}`
  const subtle = globalThis.crypto?.subtle
  if (subtle) {
    const bytes = new TextEncoder().encode(payload)
    const digest = await subtle.digest('SHA-256', bytes)
    return Array.from(new Uint8Array(digest)).map(value => value.toString(16).padStart(2, '0')).join('')
  }
  return payload
}

function normalized(input: CreatePostInput): CreatePostInput {
  return { title: input.title.trim(), content: input.content.trim() }
}

export function clearPostCommandDrafts(userID?: number): void {
  const store = storage()
  if (!store) return
  try {
    if (userID) store.removeItem(storageKey(userID))
    else for (let index = store.length - 1; index >= 0; index--) {
      const key = store.key(index)
      if (key?.startsWith(storagePrefix)) store.removeItem(key)
    }
  } catch { /* session storage is best effort */ }
}

export function usePostCommand(userID: Ref<number | null>) {
  const draft = ref<StoredDraft | null>(readDraft(userID.value))
  const loading = ref(false)
  const outcomeUnknown = ref(Boolean(draft.value?.first_sent_at))
  const expired = computed(() => Boolean(draft.value && Date.parse(draft.value.retry_window_until) <= Date.now()))
  const locked = computed(() => Boolean(draft.value?.first_sent_at))

  function syncUser(): void {
    if (draft.value?.user_id !== userID.value) {
      draft.value = readDraft(userID.value)
      outcomeUnknown.value = Boolean(draft.value?.first_sent_at)
    }
  }

  async function context(): Promise<PostCommandContext> {
    syncUser()
    return postApi.commandContext()
  }

  async function ensure(input: CreatePostInput): Promise<StoredDraft> {
    syncUser()
    if (!userID.value) throw new ApiError('authentication_required', 'authentication is required', 401)
    const value = normalized(input)
    const digest = await digestInput(value)
    if (draft.value) {
      if (draft.value.digest && draft.value.digest !== digest) {
        throw new ApiError('idempotency_conflict', '内容已改变，请开始新的发帖。', 409)
      }
      return draft.value
    }
    const current = await context()
    const next: StoredDraft = {
      user_id: userID.value,
      data_epoch: current.data_epoch,
      idempotency_key: randomUUID(),
      title: value.title,
      content: value.content,
      digest,
      first_sent_at: '',
      retry_window_until: current.retry_window_until,
    }
    draft.value = next
    writeDraft(next)
    return next
  }

  async function submit(input: CreatePostInput): Promise<{ data: Post | null; meta: PostCommandMeta }> {
    const value = normalized(input)
    const next = await ensure(value)
    if (expired.value) throw new ApiError('data_epoch_mismatch', '重试窗口已结束，请开始新的发帖。', 409)
    if (!next.first_sent_at) {
      next.first_sent_at = new Date().toISOString()
      next.title = value.title
      next.content = value.content
      writeDraft(next)
    }
    loading.value = true
    try {
      const result = await postApi.createWithCommand(value, next.data_epoch, next.idempotency_key)
      outcomeUnknown.value = false
      deleteDraft(next.user_id)
      draft.value = null
      return result
    } catch (error) {
      outcomeUnknown.value = true
      writeDraft(next)
      throw error
    } finally { loading.value = false }
  }

  async function check(): Promise<{ data: Post | null; meta: PostCommandMeta }> {
    syncUser()
    if (!draft.value || expired.value) throw new ApiError('command_result_unknown', '提交结果不可用，请开始新的发帖。', 404)
    loading.value = true
    try {
      const result = await postApi.commandResult(draft.value.data_epoch, draft.value.idempotency_key)
      deleteDraft(draft.value.user_id)
      draft.value = null
      outcomeUnknown.value = false
      return result
    } finally { loading.value = false }
  }

  function startNew(): void {
    syncUser()
    deleteDraft(userID.value)
    draft.value = null
    outcomeUnknown.value = false
  }

  function restoreInput(): CreatePostInput | null {
    syncUser()
    return draft.value && draft.value.first_sent_at ? { title: draft.value.title, content: draft.value.content } : null
  }

  return { draft, loading, locked, expired, outcomeUnknown, ensure, submit, check, context, startNew, restoreInput }
}
