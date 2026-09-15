<script setup lang="ts">
import AdminTrend from '../components/AdminTrend.vue'
import AdminStat from '../components/AdminStat.vue'
import AdminIcon from '../components/AdminIcon.vue'
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
    result.value = next; updatedAt.value = new Date().toLocaleString('zh-CN', { hour12:false }); if (!next.series.length) message.value = '所选时间范围内暂无指标数据。'
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
  <section class="metrics-page">
    <div class="admin-title"><div><h2>Metrics</h2><p>指标查询与时间序列分析，了解组件和采集目标的运行表现。</p></div></div>
    <form class="filter-bar metrics-filter" @submit.prevent="load">
      <label>指标<select v-model="metric"><option v-for="item in options" :key="item.value" :value="item.value">{{ item.label }} · {{ item.value }}</option></select></label>
      <label>时间范围<select aria-label="范围" v-model="range"><option v-for="item in ranges" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
      <button class="button" type="submit" :disabled="loading"><AdminIcon name="refresh" />{{loading?'查询中…':'应用'}}</button>
      <button class="icon-button" type="button" aria-label="刷新" :disabled="loading" @click="load"><AdminIcon name="refresh" /></button>
    </form>
    <p v-if="loading" role="status">正在查询指标…</p><p v-if="message" class="notice" role="status">{{message}}</p>
    <template v-if="result">
      <div class="summary-grid">
        <AdminStat label="当前值" :value="latest[0]?.point?.value ?? '未知'" :note="`首条非空序列 · ${result.unit}`" icon="database" tone="green"><span class="metric-value">{{latest[0]?.point?.value ?? '未知'}}</span></AdminStat>
        <AdminStat label="最大值" :value="maximum" note="返回时间窗内全部采样" icon="metrics" />
        <AdminStat label="平均值" :value="average" note="返回采样的算术平均值" icon="pulse" tone="purple" />
        <AdminStat label="Series" :value="result.series.length" note="当前查询时间序列数" icon="redis" tone="orange" />
      </div>
      <div class="panel metric-main"><header class="panel-heading"><h3>{{result.metric}}</h3><span class="muted">{{result.kind}} · {{result.step_seconds}}s 步长</span></header><AdminTrend :series="result.series" :unit="result.unit" /></div>
      <div class="metrics-secondary">
        <div class="panel"><header class="panel-heading"><h3>最近采样</h3><span class="muted">首条非空序列</span></header><div class="table-scroll"><table><thead><tr><th>采样时间</th><th>值（{{result.unit}}）</th></tr></thead><tbody><tr v-for="point in latest[0]?.series.points.slice(-5).reverse() ?? []" :key="point.timestamp"><td>{{new Date(point.timestamp).toLocaleTimeString('zh-CN', { hour12:false })}}</td><td>{{point.value}}</td></tr></tbody></table></div></div>
        <div class="panel"><header class="panel-heading"><h3>时间序列（Series）</h3><span class="muted">{{result.series.length}} 条</span></header><div class="table-scroll"><table><thead><tr><th>安全标签</th><th>当前值</th><th>采样时间</th></tr></thead><tbody><tr v-for="(item,index) in latest" :key="index"><td>{{Object.entries(item.labels).map(([key,value])=>`${key}=${value}`).join(', ') || '默认序列'}}</td><td>{{item.point?.value}}</td><td>{{item.point?new Date(item.point.timestamp).toLocaleTimeString('zh-CN', { hour12:false }):'未知'}}</td></tr></tbody></table></div></div>
      </div>
      <div class="panel query-contract"><h3>查询信息</h3><dl><div><dt>返回时间窗</dt><dd>{{new Date(result.from).toLocaleString('zh-CN', { hour12:false })}} — {{new Date(result.to).toLocaleString('zh-CN', { hour12:false })}}</dd></div><div><dt>最近成功更新</dt><dd>{{updatedAt}}</dd></div></dl><p class="muted">仅展示当前指标的真实返回值，不将累计 CPU 时间解释为 CPU 使用率，也不构造同比或其他指标曲线。</p></div>
    </template>
  </section>
</template>
