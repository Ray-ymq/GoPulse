import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../services/http'
import { clearPostCommandDrafts, usePostCommand } from './usePostCommand'
import { postApi } from '../services/api'

vi.mock('../services/api', () => ({
  postApi: {
    commandContext: vi.fn(),
    createWithCommand: vi.fn(),
    commandResult: vi.fn(),
  },
}))

const epoch = '123e4567-e89b-12d3-a456-426614174000'
const input = { title: 'Title', content: 'Content' }
const result = {
  data: null,
  meta: {
    command_id: '123e4567-e89b-12d3-a456-426614174001',
    event_id: '123e4567-e89b-12d3-a456-426614174002',
    data_epoch: epoch,
    post_id: 9,
    committed_at: '2026-12-01T00:00:00Z',
    replayed: true,
    resource_state: 'unavailable' as const,
  },
}

function futureContext() {
  return { data_epoch: epoch, retry_window_seconds: 604800, retry_window_until: '2099-12-01T00:00:00Z' }
}

afterEach(() => {
  clearPostCommandDrafts()
  vi.clearAllMocks()
})

describe('usePostCommand', () => {
  it('creates one session draft per user and rejects body changes after identity allocation', async () => {
    vi.mocked(postApi.commandContext).mockResolvedValue(futureContext())
    const first = usePostCommand(ref<number | null>(7))
    const draft = await first.ensure(input)
    expect(draft.user_id).toBe(7)
    expect(draft.data_epoch).toBe(epoch)
    expect(draft.idempotency_key).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[4][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)

    const restored = usePostCommand(ref<number | null>(7))
    await expect(restored.ensure({ ...input, content: 'changed' })).rejects.toMatchObject({ code: 'idempotency_conflict', status: 409 })
    expect(restored.restoreInput()).toBeNull()
    expect(restored.draft.value?.idempotency_key).toBe(draft.idempotency_key)
  })

  it('isolates account drafts and clears a successful command identity', async () => {
    vi.mocked(postApi.commandContext).mockResolvedValue(futureContext())
    vi.mocked(postApi.createWithCommand).mockResolvedValue(result)
    const first = usePostCommand(ref<number | null>(7))
    await first.submit(input)
    expect(first.draft.value).toBeNull()

    await first.ensure(input)
    const other = usePostCommand(ref<number | null>(8))
    expect(other.draft.value).toBeNull()
  })

  it('keeps an unknown outcome for same-key verification and clears it only after a result', async () => {
    vi.mocked(postApi.commandContext).mockResolvedValue(futureContext())
    vi.mocked(postApi.createWithCommand).mockRejectedValue(new ApiError('network_error', 'offline', null))
    const command = usePostCommand(ref<number | null>(7))
    await expect(command.submit(input)).rejects.toMatchObject({ code: 'network_error' })
    expect(command.outcomeUnknown.value).toBe(true)
    expect(command.locked.value).toBe(true)
    expect(command.restoreInput()).toEqual(input)

    vi.mocked(postApi.commandResult).mockResolvedValue(result)
    await expect(command.check()).resolves.toEqual(result)
    expect(command.draft.value).toBeNull()
    expect(command.outcomeUnknown.value).toBe(false)
  })

  it('stops retry after the server-provided window expires', async () => {
    vi.mocked(postApi.commandContext).mockResolvedValue({ ...futureContext(), retry_window_until: '2020-01-01T00:00:00Z' })
    const command = usePostCommand(ref<number | null>(7))
    await expect(command.submit(input)).rejects.toMatchObject({ code: 'data_epoch_mismatch', status: 409 })
    expect(postApi.createWithCommand).not.toHaveBeenCalled()
  })
})
