<script setup lang="ts">
import MetricTrend from '../components/MetricTrend.vue'
import { useRoute } from 'vue-router'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError } from '../services/http'
import { metricCatalog, loadMetricCatalog, observabilityApi, ranges } from '../services/observability'
import type { MetricName, MetricResult, QueryRange } from '../types/observability'

const route = useRoute()
const options = ref<typeof metricCatalog>([])
const metric = ref<MetricName>('gopulse_redis_up')
const range = ref<QueryRange>('15m')
const result = ref<MetricResult | null>(null)
const loading = ref(false)
const message = ref('')
const updatedAt = ref('')
let sequence = 0
let controller: AbortController | null = null
const samples = computed(() => result.value?.series.flatMap(series => series.points.map(point => point.value)) ?? [])
const maximum = computed(() => samples.value.length ? samples.value.reduce((a,b) => Math.max(a,b)) : '未知')
const average = computed(() => samples.value.length ? Number((samples.value.reduce((a,b) => a+b,0)/samples.value.length).toPrecision(6)) : '未知')
const latest = computed(() => result.value?.series.map((series) => ({ series, labels: series.labels, point: series.points.at(-1) })).filter((item) => item.point) ?? [])
function errorMessage(error: unknown): string {
  if (error instanceof ApiError && error.code === 'metrics_unavailable') return '指标存储或查询服务暂时不可用（VictoriaMetrics），已保留上次成功结果；这不代表所有 Exporter 目标均不可达。'
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
onMounted(async () => {
  try {
    const entries = await loadMetricCatalog()
    options.value = entries.map(entry => ({ value: entry.metric, label: metricCatalog.find(item => item.value === entry.metric)?.label ?? entry.metric }))
    const source = String(route.query.source ?? 'redis')
    const initial = entries.find(entry => entry.source === source)
    if (initial) metric.value = initial.metric
    await load()
  } catch (error) { message.value = errorMessage(error) }
})
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
<template>
  <section>
    <div class="admin-title"><div><p class="admin-eyebrow">FIXED RANGE QUERY</p><h2>Metrics</h2><p>仅查询固定指标目录与服务器生成的时间窗。</p></div><button class="button" :disabled="loading" @click="load">{{ loading ? '查询中…' : '刷新' }}</button></div>
    <form class="filter-bar" @submit.prevent="load">
      <label>指标<select v-model="metric"><option v-for="item in options" :key="item.value" :value="item.value">{{ item.label }} · {{ item.value }}</option></select></label>
      <label>范围<select v-model="range"><option v-for="item in ranges" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
      <button class="button" type="submit" :disabled="loading">应用</button>
    </form>
    <p v-if="loading" role="status">正在查询指标…</p>
    <p v-if="message" class="notice" role="status">{{ message }}</p>
    <template v-if="result">
      <div class="summary-grid"><div><span>当前值 · 首条非空 Series</span><strong class="metric-value">{{latest[0]?.point?.value ?? '未知'}}</strong><span>{{result.unit}}</span></div><div><span>最大值 · 返回窗口全部采样</span><strong>{{maximum}}</strong></div><div><span>平均值 · 返回窗口全部采样</span><strong>{{average}}</strong></div><div><span>Series · 当前查询</span><strong>{{result.series.length}}</strong></div></div>
      <div class="panel"><h3>{{result.metric}}</h3>
      <div class="metric-query-meta summary-grid"><div><span>类型</span><strong>{{ result.kind }}</strong></div><div><span>单位</span><strong>{{ result.unit }}</strong></div><div><span>步长</span><strong>{{ result.step_seconds }}s</strong></div><div><span>更新时间</span><strong>{{ updatedAt }}</strong></div></div>
      <p class="time-window">{{ result.from }} — {{ result.to }}</p>
      <div v-for="(item,index) in latest" :key="index" class="series-card">
        <div><strong>{{ Object.entries(item.labels).map(([k,v]) => `${k}=${v}`).join(', ') || '默认时序' }}</strong><span class="metric-value">{{ item.point?.value }}</span></div>
        <MetricTrend :points="item.series.points" :unit="result.unit" />
        <h3>最近采样点</h3>
        <table><thead><tr><th>采样时间</th><th>值（{{ result.unit }}）</th></tr></thead><tbody><tr v-for="(point, pointIndex) in item.series.points.slice(-5).reverse()" :key="pointIndex"><td>{{ point.timestamp }}</td><td>{{ point.value }}</td></tr></tbody></table>
      </div>
    </div>
    <div class="panel"><h3>时间序列（Series）</h3><div class="table-scroll"><table><thead><tr><th>安全标签</th><th>当前值</th><th>采样时间</th></tr></thead><tbody><tr v-for="(item,index) in latest" :key="index"><td>{{JSON.stringify(item.labels)}}</td><td>{{item.point?.value}}</td><td>{{item.point?.timestamp}}</td></tr></tbody></table></div><p class="muted">仅展示当前指标的真实序列；CPU、同比与其他关键指标未包含在此次查询中。</p></div>
    </template>
  </section>
</template>
