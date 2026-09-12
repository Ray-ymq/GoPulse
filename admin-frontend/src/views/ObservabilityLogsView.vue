<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
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
function apply(): void { query.load(true) }
onMounted(apply)
</script>
<template>
  <section>
    <div class="admin-title"><div><p class="admin-eyebrow">ELASTICSEARCH LOGS</p><h2>应用日志</h2><p>使用固定字段精确筛选，不提供全文检索或原始文档。</p></div><button class="button" :disabled="query.loading.value" @click="apply">刷新</button></div>
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
    <p v-if="query.message.value" class="notice" role="status">{{ query.message.value }}</p>
    <p v-if="query.updatedAt.value" class="last-updated">最近成功更新：{{ query.updatedAt.value }}</p>
    <div class="record-list" :aria-busy="query.loading.value">
      <article v-for="(entry,index) in query.items.value" :key="`${entry.timestamp}-${index}`" class="record-card">
        <div class="record-card__header"><time>{{ entry.timestamp }}</time><span :class="`level level--${entry.level}`">{{ entry.level }}</span></div>
        <h3>{{ entry.message }}</h3><p>{{ entry.service }} / {{ entry.module }}</p>
        <dl class="metadata-list"><template v-for="(value,key) in entry" :key="key"><div v-if="!['timestamp','level','service','module','message'].includes(String(key))"><dt>{{ key }}</dt><dd>{{ value }}</dd></div></template></dl>
      </article>
    </div>
    <button v-if="query.cursor.value" class="button button--secondary load-more" :disabled="query.loadingMore.value" @click="query.load(false)">{{ query.loadingMore.value ? '加载中…' : '加载更多' }}</button>
  </section>
</template>
