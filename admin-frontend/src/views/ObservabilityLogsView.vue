<script setup lang="ts">
import ProductTime from "../../../frontend-shared/ProductTime.vue"
import ProductState from "../../../frontend-shared/ProductState.vue"
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { usePagedObservability } from '../composables/usePagedObservability'
import { logCatalog, observabilityApi, ranges } from '../services/observability'
import type { LogFilters } from '../types/observability'

const filters = ref<LogFilters>({ range:'15m', service:'', module:'', level:'', message:'', request_id:'', event_id:'', error_code:'' })
const services = Object.keys(logCatalog)
const modules = computed(() => filters.value.service ? Object.keys(logCatalog[filters.value.service] ?? {}) : [...new Set(Object.values(logCatalog).flatMap((service) => Object.keys(service)))] )
const messages = computed(() => {
  if (filters.value.service && filters.value.module) return [...(logCatalog[filters.value.service]?.[filters.value.module] ?? [])]
  if (filters.value.service) return [...new Set(Object.values(logCatalog[filters.value.service] ?? {}).flat())]
  return [...new Set(Object.values(logCatalog).flatMap((service) => Object.values(service).flat()))]
})
watch(() => filters.value.service, () => { filters.value.module=''; filters.value.message='' })
watch(() => filters.value.module, () => { filters.value.message='' })
const query = usePagedObservability(filters, observabilityApi.logs, 'logs_unavailable')
const selectedIndex = ref<number | null>(null)
const selectedEntry = computed(() => selectedIndex.value === null ? null : query.items.value[selectedIndex.value])
const detail = ref<HTMLElement | null>(null)
let selectionButton: HTMLElement | null = null
async function selectEntry(index: number, event: Event) {
  selectionButton = event.currentTarget as HTMLElement
  selectedIndex.value = index
  await nextTick(); detail.value?.focus()
}
function closeDetail() { selectedIndex.value = null; selectionButton?.focus() }
function apply(): void { selectedIndex.value = null; query.load(true) }
onMounted(apply)
</script>
<template>
  <section>
    <div class="admin-title"><div><p class="admin-eyebrow">ELASTICSEARCH LOGS</p><h2>Logs</h2><p>使用固定字段精确筛选，不提供全文检索或原始文档。</p></div><button class="button" :disabled="query.loading.value" @click="apply">刷新</button></div>
    <form class="filter-grid" @submit.prevent="apply">
      <label>范围<select v-model="filters.range"><option v-for="item in ranges" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
      <label>服务<select v-model="filters.service"><option value="">全部</option><option v-for="item in services" :key="item">{{ item }}</option></select></label>
      <label>模块<select v-model="filters.module"><option value="">全部</option><option v-for="item in modules" :key="item">{{ item }}</option></select></label>
      <label>级别<select v-model="filters.level"><option value="">全部</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></select></label>
      <label>固定消息<select v-model="filters.message"><option value="">全部</option><option v-for="item in messages" :key="item">{{ item }}</option></select></label>
      <label>Request ID<input v-model.trim="filters.request_id" maxlength="32"></label>
      <label>Event ID<input v-model.trim="filters.event_id" maxlength="36"></label>
      <label>错误码<input v-model.trim="filters.error_code" maxlength="64"></label>
      <button class="button" type="submit" :disabled="query.loading.value">应用筛选</button>
    </form>
    <ProductState v-if="query.loading.value" state="loading" />
    <ProductState v-else-if="query.message.value" :state="query.state.value" :message="query.message.value" />
    <p v-if="query.updatedAt.value" class="last-updated">最近成功更新：{{ query.updatedAt.value }}</p>
    <div v-if="query.updatedAt.value" class="summary-grid" aria-label="已加载日志摘要">
      <div><span>已加载日志（非窗口总量）</span><strong>{{ query.items.value.length }}</strong></div>
      <div v-for="level in ['info', 'warn', 'error']" :key="level"><span>{{ level }}</span><strong>{{ query.items.value.filter(entry => entry.level === level).length }}</strong></div>
    </div>
    <div class="list-detail-layout" :class="{ 'has-detail': selectedEntry }">
    <div class="panel logs-stream"><h3>日志列表</h3>
      <div class="table-scroll"><table :aria-busy="query.loading.value"><thead><tr><th>时间</th><th>级别</th><th>来源</th><th>日志内容</th></tr></thead><tbody>
        <tr v-for="(entry,index) in query.items.value" :key="`${entry.timestamp}-${index}`" class="record-card" :class="{ 'is-selected': selectedIndex === index }">
          <td><ProductTime :value="entry.timestamp" /></td><td><span :class="`level level--${entry.level}`">{{ entry.level }}</span></td><td>{{ entry.service }} / {{ entry.module }}</td>
          <td><button class="row-select" :aria-pressed="selectedIndex === index" @click="selectEntry(index, $event)">{{ entry.message }}</button></td>
        </tr>
      </tbody></table></div>
      <button v-if="query.cursor.value" class="button button--secondary load-more" :disabled="query.loadingMore.value" @click="query.load(false)">{{ query.loadingMore.value ? '加载中…' : '加载更多' }}</button>
    </div>
    <aside v-if="selectedEntry" ref="detail" class="panel detail-panel" tabindex="-1" aria-label="日志详情" @keydown.esc="closeDetail">
      <header><h3>日志详情</h3><button class="button button--secondary" @click="closeDetail">关闭详情</button></header>
      <p><span :class="`level level--${selectedEntry.level}`">{{ selectedEntry.level }}</span> <ProductTime :value="selectedEntry.timestamp" /></p>
      <pre class="safe-message">{{ selectedEntry.message }}</pre><h4>基本信息与安全标签</h4>
      <dl class="detail-fields"><template v-for="(value,key) in selectedEntry" :key="key"><dt>{{ key }}</dt><dd>{{ value ?? '未知' }}</dd></template></dl>
      <p class="muted">仅展示服务端允许的安全字段，不提供原始日志或关联 Trace。</p>
    </aside>
    </div>
  </section>
</template>
