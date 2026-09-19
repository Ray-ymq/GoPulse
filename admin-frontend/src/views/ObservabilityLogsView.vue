<script setup lang="ts">
import AdminIcon from "../components/AdminIcon.vue"
import AdminStat from "../components/AdminStat.vue"
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
watch(query.items, items => { if (!query.loadingMore.value && window.innerWidth > 1000) selectedIndex.value = items.length ? 0 : null })
function apply(): void { selectedIndex.value = null; query.load(true) }
onMounted(apply)
</script>
<template>
  <section class="logs-page">
    <div class="admin-title"><div><p class="admin-eyebrow">ELASTICSEARCH LOGS</p><h2>Logs</h2><p>集中查询与分析系统日志，快速定位服务问题。</p></div><button class="button" :disabled="query.loading.value" @click="apply">刷新</button></div>
    <form class="logs-query" @submit.prevent="apply">
      <div class="logs-query-main"><label class="search-field"><AdminIcon name="search"/><input v-model.trim="filters.request_id" aria-label="Request ID" maxlength="32" placeholder="精确查询 Request ID…"></label><label>日志级别<select v-model="filters.level"><option value="">全部</option><option>info</option><option>warn</option><option>error</option></select></label><label>日志来源<select v-model="filters.service"><option value="">全部</option><option v-for="item in services" :key="item">{{item}}</option></select></label><label>时间范围<select aria-label="范围" v-model="filters.range"><option v-for="item in ranges" :key="item.value" :value="item.value">{{item.label}}</option></select></label><button class="icon-button" type="submit" aria-label="应用筛选" :disabled="query.loading.value"><AdminIcon name="search"/></button></div>
      <details class="advanced-filters"><summary>更多精确筛选</summary><div class="filter-grid"><label>模块<select v-model="filters.module"><option value="">全部</option><option v-for="item in modules" :key="item">{{item}}</option></select></label><label>固定消息<select v-model="filters.message"><option value="">全部</option><option v-for="item in messages" :key="item">{{item}}</option></select></label><label>Event ID<input v-model.trim="filters.event_id" maxlength="36"></label><label>错误码<input v-model.trim="filters.error_code" maxlength="64"></label></div><p class="muted">仅支持固定字段精确匹配，不执行全文检索。</p></details>
    </form>
    <ProductState v-if="query.loading.value" state="loading" />
    <ProductState v-else-if="query.message.value" :state="query.state.value" :message="query.message.value" />
    <div v-if="query.updatedAt.value" class="summary-grid" aria-label="已加载日志摘要">
      <AdminStat label="日志总数" :value="query.items.value.length" note="已加载记录，非窗口总量" icon="logs" />
      <AdminStat label="错误日志" :value="query.items.value.filter(entry=>entry.level==='error').length" note="已加载记录中的 error" icon="warning" tone="red" />
      <AdminStat label="警告日志" :value="query.items.value.filter(entry=>entry.level==='warn').length" note="已加载记录中的 warn" icon="warning" tone="orange" />
      <AdminStat label="日志来源" :value="new Set(query.items.value.map(entry=>entry.service)).size" note="已加载记录中的服务数" icon="box" tone="green" />
    </div>
    <div class="list-detail-layout" :class="{ 'has-detail': selectedEntry }">
    <div class="panel logs-stream"><header class="panel-heading"><h3>日志列表 <small>已加载 {{query.items.value.length}} 条</small></h3><span class="muted">最新在上</span></header>
      <div class="table-scroll"><table :aria-busy="query.loading.value"><thead><tr><th>时间</th><th>级别</th><th>来源</th><th>日志内容</th><th>Request ID</th></tr></thead><tbody>
        <tr v-for="(entry,index) in query.items.value" :key="`${entry.timestamp}-${index}`" class="record-card" :class="{ 'is-selected': selectedIndex === index }">
          <td :title="entry.timestamp">{{new Date(entry.timestamp).toLocaleTimeString('zh-CN', { hour12:false })}}</td><td><span :class="`level level--${entry.level}`">{{ entry.level }}</span></td><td>{{ entry.service }}</td>
          <td class="message-cell"><button class="row-select" :title="entry.message" :aria-pressed="selectedIndex === index" @click="selectEntry(index, $event)">{{ entry.message }}</button></td><td class="id-cell" :title="entry.request_id">{{entry.request_id || '—'}}</td>
        </tr>
      </tbody></table></div>
      <button v-if="query.cursor.value" class="button button--secondary load-more" :disabled="query.loadingMore.value" @click="query.load(false)">{{ query.loadingMore.value ? '加载中…' : '加载更多' }}</button>
    </div>
    <aside v-if="selectedEntry" ref="detail" class="panel detail-panel" tabindex="-1" aria-label="日志详情" @keydown.esc="closeDetail">
      <header><h3>日志详情</h3><button class="icon-button" aria-label="关闭详情" @click="closeDetail"><AdminIcon name="close" /></button></header>
      <p><span :class="`level level--${selectedEntry.level}`">{{ selectedEntry.level }}</span> <ProductTime :value="selectedEntry.timestamp" /></p>
      <pre class="safe-message">{{ selectedEntry.message }}</pre><h4>基本信息</h4>
      <dl class="detail-fields"><template v-for="(value,key) in selectedEntry" :key="key"><dt>{{ key }}</dt><dd>{{ value ?? '未知' }}</dd></template></dl>
      <p class="muted">仅展示服务端允许的安全字段，不提供原始日志或关联 Trace。</p>
    </aside>
    </div>
    <p v-if="query.updatedAt.value" class="last-updated">最近成功更新：{{query.updatedAt.value}}</p>
  </section>
</template>
