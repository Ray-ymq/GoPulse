<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { usePagedObservability } from '../composables/usePagedObservability'
import { eventErrorCodes, eventNames, eventOperations, eventSeverities, observabilityApi, ranges } from '../services/observability'
import type { EventFilters } from '../types/observability'

const labels: Record<string,string> = {
  exporter_plugin_installed:'Exporter 已安装', exporter_plugin_started:'Exporter 已启动', exporter_plugin_stopped:'Exporter 已停止', exporter_plugin_updated:'Exporter 已更新',
  exporter_plugin_failed:'Exporter 操作失败', exporter_plugin_exited:'Exporter 异常退出', metrics_collection_failed:'指标采集失败', metrics_collection_recovered:'指标采集恢复', metrics_target_unavailable:'指标目标不可用', metrics_target_recovered:'指标目标恢复',
}
const filters = ref<EventFilters>({ range:'15m', source:'', event_name:'', severity:'', plugin_id:'', operation:'', error_code:'' })
const operations = computed(() => filters.value.event_name ? eventOperations[filters.value.event_name] ?? [] : ['install','start','stop','update','recover','scrape','publish'])
watch(() => filters.value.event_name, (name) => { filters.value.operation=''; filters.value.error_code=''; if (name) filters.value.severity=eventSeverities[name] ?? '' })
const query = usePagedObservability(filters, observabilityApi.events, 'events_unavailable')
function apply(): void { query.load(true) }
onMounted(apply)
</script>
<template>
  <section>
    <div class="admin-title"><div><p class="admin-eyebrow">MONITOR EVENTS</p><h2>运行事件</h2><p>事件是有界的 best-effort / at-least-once 可观测记录；未查到事件不代表操作绝对未发生。</p></div><button class="button" :disabled="query.loading.value" @click="apply">刷新</button></div>
    <form class="filter-grid" @submit.prevent="apply">
      <label>范围<select v-model="filters.range"><option v-for="item in ranges" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
      <label>来源<select v-model="filters.source"><option value="">全部</option><option value="monitor">monitor</option></select></label>
      <label>事件<select v-model="filters.event_name"><option value="">全部</option><option v-for="item in eventNames" :key="item" :value="item">{{ labels[item] }} · {{ item }}</option></select></label>
      <label>严重度<select v-model="filters.severity"><option value="">全部</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></select></label>
      <label>Plugin ID<input v-model.trim="filters.plugin_id" maxlength="128" placeholder="redis-exporter"></label>
      <label>操作<select v-model="filters.operation"><option value="">全部</option><option v-for="item in operations" :key="item">{{ item }}</option></select></label>
      <label>错误码<select v-model="filters.error_code"><option value="">全部</option><option v-for="item in eventErrorCodes" :key="item">{{ item }}</option></select></label>
      <button class="button" type="submit" :disabled="query.loading.value">应用筛选</button>
    </form>
    <p v-if="query.message.value" class="notice" role="status">{{ query.message.value }}</p>
    <p v-if="query.updatedAt.value" class="last-updated">最近成功更新：{{ query.updatedAt.value }}</p>
    <div class="record-list" :aria-busy="query.loading.value">
      <article v-for="(entry,index) in query.items.value" :key="`${entry.timestamp}-${index}`" class="record-card">
        <div class="record-card__header"><time>{{ entry.timestamp }}</time><span :class="`level level--${entry.severity}`">{{ entry.severity }}</span></div>
        <h3>{{ labels[entry.event_name] }} <code>{{ entry.event_name }}</code></h3><p>{{ entry.message }}</p>
        <dl class="metadata-list"><div><dt>source</dt><dd>{{ entry.source }}</dd></div><template v-for="(value,key) in entry.metadata" :key="key"><div><dt>{{ key }}</dt><dd>{{ value }}</dd></div></template></dl>
      </article>
    </div>
    <button v-if="query.cursor.value" class="button button--secondary load-more" :disabled="query.loadingMore.value" @click="query.load(false)">{{ query.loadingMore.value ? '加载中…' : '加载更多' }}</button>
  </section>
</template>
