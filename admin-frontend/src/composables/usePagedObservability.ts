import { onBeforeUnmount, ref, type Ref } from 'vue'
import type { Page } from '../types/api'
import { ApiError } from '../services/http'

export function usePagedObservability<T, F extends object>(filters: Ref<F>, request: (filters: F, cursor?: string, signal?: AbortSignal) => Promise<Page<T>>, unavailableCode: string) {
  const items = ref<T[]>([]) as Ref<T[]>
  const cursor = ref<string | null>(null)
  const loading = ref(false)
  const loadingMore = ref(false)
  const message = ref('')
  const updatedAt = ref('')
  let sequence = 0
  let controller: AbortController | null = null
  function safeMessage(error: unknown): string {
    if (error instanceof ApiError && error.code === unavailableCode) return '查询依赖暂时不可用，已保留上次成功结果。'
    if (error instanceof ApiError && error.code === 'validation_failed') return '筛选条件无效，请调整后重试。'
    if (error instanceof ApiError && error.code === 'permission_denied') return '当前账号已无管理员权限。'
    return '查询失败，请稍后重试。'
  }
  async function load(reset = true): Promise<void> {
    if (loading.value || loadingMore.value) return
    controller?.abort(); controller = new AbortController(); const current = ++sequence
    if (reset) loading.value = true; else loadingMore.value = true
    message.value = ''
    try {
      const page = await request(filters.value, reset ? undefined : cursor.value ?? undefined, controller.signal)
      if (current !== sequence) return
      items.value = reset ? page.data : [...items.value, ...page.data]
      cursor.value = page.nextCursor; updatedAt.value = new Date().toLocaleString()
      if (reset && page.data.length === 0) message.value = '所选条件下暂无数据。'
    } catch (error) {
      if (current !== sequence || controller.signal.aborted) return
      if (!reset && error instanceof ApiError && error.code === 'validation_failed') {
        cursor.value = null; message.value = '分页游标已失效，请刷新首页结果。'
      } else message.value = safeMessage(error)
    } finally { if (current === sequence) { loading.value = false; loadingMore.value = false } }
  }
  onBeforeUnmount(() => { sequence++; controller?.abort(); items.value=[]; cursor.value=null })
  return { items, cursor, loading, loadingMore, message, updatedAt, load }
}
