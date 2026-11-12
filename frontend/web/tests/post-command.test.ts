import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
const input = { title: '山野里的一天', content: '今天听到了风声。' }
const epoch = '11111111-1111-4111-8111-111111111111'
function response(data: unknown, status = 200) { return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } }) }
beforeEach(() => {
  vi.resetModules(); const values = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) })
})
afterEach(() => vi.unstubAllGlobals())
describe('retryable publishing', () => {
  it('retains the same command and content across a failed request and reload', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(response({ data: { data_epoch: epoch } })).mockRejectedValueOnce(new TypeError('lost response'))
    vi.stubGlobal('fetch', fetcher)
    const first = await import('../src/services/post-command'); await expect(first.publish(1, input)).rejects.toThrow()
    const command = first.pendingPost(1); expect(command?.key).toBeTruthy()
    vi.resetModules(); const second = await import('../src/services/post-command')
    fetcher.mockResolvedValueOnce(response({ data: { id: 9 }, meta: { resource_state: 'available' } }))
    await second.publish(1, input)
    expect(fetcher.mock.calls[2]?.[1]).toMatchObject({ body: JSON.stringify(input), headers: expect.objectContaining({ 'Idempotency-Key': command?.key, 'X-GoPulse-Data-Epoch': epoch }) })
    expect(second.pendingPost(1)).toBeNull()
  })
  it('keeps an unknown result pending and refuses to change its payload', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(response({ data: { data_epoch: epoch } })).mockRejectedValueOnce(new TypeError('timeout'))
    vi.stubGlobal('fetch', fetcher); const command = await import('../src/services/post-command')
    await expect(command.publish(1, input)).rejects.toThrow()
    fetcher.mockResolvedValueOnce(response({ error: { code: 'command_result_unknown' } }, 404))
    await expect(command.resolvePost(1)).rejects.toMatchObject({ code: 'command_result_unknown' })
    expect(command.pendingPost(1)).not.toBeNull()
    await expect(command.publish(1, { ...input, title: '修改后的内容' })).rejects.toMatchObject({ code: 'pending_changed' })
    expect(fetcher).toHaveBeenCalledTimes(3)
  })
  it('does not send a different account’s pending publication', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(response({ data: { data_epoch: epoch } })).mockRejectedValueOnce(new TypeError('timeout')))
    const command = await import('../src/services/post-command'); await expect(command.publish(1, input)).rejects.toThrow()
    expect(command.pendingPost(2)).toBeNull(); await expect(command.publish(2, input)).rejects.toMatchObject({ code: 'pending_other_user' })
  })
})
