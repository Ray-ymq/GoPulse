<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError } from '../services/http'
import { metricCatalog, observabilityApi, ranges } from '../services/observability'
import type { MetricName, MetricResult, QueryRange } from '../types/observability'

const metric = ref<MetricName>('gopulse_redis_up')
const range = ref<QueryRange>('15m')
const result = ref<MetricResult | null>(null)
const loading = ref(false)
const message = ref('')
const updatedAt = ref('')
let sequence = 0
let controller: AbortController | null = null
const latest = computed(() => result.value?.series.map((series) => ({ series, labels: series.labels, point: series.points.at(-1) })).filter((item) => item.point) ?? [])
function errorMessage(error: unknown): string {
  if (error instanceof ApiError && error.code === 'metrics_unavailable') return 'Metrics 服务暂时不可用，已保留上次成功结果。'
  if (error instanceof ApiError && error.code === 'permission_denied') return '当前账号已无管理员权限。'
  return '指标查询失败，请稍后重试。'
}
async function load(): Promise<void> {
  controller?.abort(); controller = new AbortController(); const current = ++sequence
  loading.value = true; message.value = ''
  try {
    const next = await observabilityApi.metrics(metric.value, range.value, controller.signal)
    if (current !== sequence) return
    result.value = next; updatedAt.value = new Date().toLocaleString(); if (!next.series.length) message.value = '所选时间范围内暂无指标数据。'
  } catch (error) { if (current === sequence && !controller.signal.aborted) message.value = errorMessage(error) }
  finally { if (current === sequence) loading.value = false }
}
onMounted(load)
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
<template>
  <section>
    <div class="admin-title"><div><p class="admin-eyebrow">FIXED RANGE QUERY</p><h2>Redis Metrics</h2><p>仅查询固定指标目录与服务器生成的时间窗。</p></div><button class="button" :disabled="loading" @click="load">{{ loading ? '查询中…' : '刷新' }}</button></div>
    <form class="filter-bar" @submit.prevent="load">
      <label>指标<select v-model="metric"><option v-for="item in metricCatalog" :key="item.value" :value="item.value">{{ item.label }} · {{ item.value }}</option></select></label>
      <label>范围<select v-model="range"><option v-for="item in ranges" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
      <button class="button" type="submit" :disabled="loading">应用</button>
    </form>
    <p v-if="message" class="notice" role="status">{{ message }}</p>
    <div v-if="result" class="panel">
      <div class="summary-grid"><div><span>类型</span><strong>{{ result.kind }}</strong></div><div><span>单位</span><strong>{{ result.unit }}</strong></div><div><span>步长</span><strong>{{ result.step_seconds }}s</strong></div><div><span>更新时间</span><strong>{{ updatedAt }}</strong></div></div>
      <p class="time-window">{{ result.from }} — {{ result.to }}</p>
      <div v-for="(item,index) in latest" :key="index" class="series-card">
        <div><strong>{{ Object.entries(item.labels).map(([k,v]) => `${k}=${v}`).join(', ') || '默认时序' }}</strong><span class="metric-value">{{ item.point?.value }}</span></div>
        <div class="sparkline" aria-hidden="true"><i v-for="(point,p) in item.series.points.slice(-40)" :key="p" :style="{height: `${Math.max(4, Math.min(42, 4 + Math.abs(point.value) % 38))}px`}" /></div>
        <table><thead><tr><th>最近时间</th><th>最新值</th></tr></thead><tbody><tr><td>{{ item.point?.timestamp }}</td><td>{{ item.point?.value }}</td></tr></tbody></table>
      </div>
    </div>
  </section>
</template>
