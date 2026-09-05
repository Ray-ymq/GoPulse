<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive } from 'vue'
import { exporterApi } from '../services/exporters'
import { ApiError } from '../services/http'
import { observabilityApi } from '../services/observability'
import type { ExporterStatus } from '../types/exporter'
import type { EventEntry, LogEntry, MetricResult } from '../types/observability'

type Region<T> = { loading:boolean; error:string; updated:string; value:T | null }
const regions = reactive({
  metrics:{loading:false,error:'',updated:'',value:null as MetricResult|null}, logs:{loading:false,error:'',updated:'',value:null as LogEntry[]|null},
  events:{loading:false,error:'',updated:'',value:null as EventEntry[]|null}, exporter:{loading:false,error:'',updated:'',value:null as ExporterStatus|null},
})
const controllers: AbortController[] = []
function errorText(error: unknown, dependency: string): string { return error instanceof ApiError && error.code === 'permission_denied' ? '当前账号已无管理员权限。' : `${dependency} 暂时不可用，其他区域不受影响。` }
async function settle<T>(region: Region<T>, dependency: string, loader: (signal:AbortSignal)=>Promise<T>): Promise<void> {
  const controller = new AbortController(); controllers.push(controller); region.loading=true; region.error=''
  try { region.value=await loader(controller.signal); region.updated=new Date().toLocaleString() } catch (error) { if (!controller.signal.aborted) region.error=errorText(error,dependency) } finally { region.loading=false }
}
function loadMetrics(){ return settle(regions.metrics,'Metrics',(signal)=>observabilityApi.metrics('gopulse_redis_up','15m',signal)) }
function loadLogs(){ return settle(regions.logs,'Logs',async(signal)=>(await observabilityApi.logs({range:'15m',service:'',module:'',level:'',message:'',request_id:'',event_id:'',error_code:''},undefined,signal)).data.slice(0,5)) }
function loadEvents(){ return settle(regions.events,'Events',async(signal)=>(await observabilityApi.events({range:'15m',source:'',event_name:'',severity:'',plugin_id:'',operation:'',error_code:''},undefined,signal)).data.slice(0,5)) }
function loadExporter(){ return settle(regions.exporter,'Monitor',async(signal)=>(await exporterApi.list(signal))[0] ?? null) }
function latestMetric(): string { const points=regions.metrics.value?.series.flatMap((series)=>series.points.slice(-1)) ?? []; return points.length ? String(points.at(-1)?.value) : '暂无样本' }
onMounted(()=>{ void loadMetrics(); void loadLogs(); void loadEvents(); void loadExporter() })
onBeforeUnmount(()=>controllers.forEach((controller)=>controller.abort()))
</script>
<template>
  <section>
    <div class="admin-title"><div><p class="admin-eyebrow">INDEPENDENT LIVE QUERIES</p><h2>可观测总览</h2><p>四个区域独立查询与恢复；历史样本不代表 Exporter 当前事实。</p></div></div>
    <div class="overview-grid">
      <article class="overview-card" data-testid="metrics-region"><header><div><span>Metrics</span><h3>Redis 可用状态</h3></div><RouterLink to="/admin/observability/metrics">查看详情</RouterLink></header><p v-if="regions.metrics.loading">加载中…</p><p v-else-if="regions.metrics.error" class="notice">{{ regions.metrics.error }}</p><strong v-else class="overview-value">{{ latestMetric() }}</strong><footer>最近 15 分钟 · {{ regions.metrics.updated || '尚未更新' }} <button :disabled="regions.metrics.loading" @click="loadMetrics">重试</button></footer></article>
      <article class="overview-card" data-testid="logs-region"><header><div><span>Logs</span><h3>最近日志</h3></div><RouterLink to="/admin/observability/logs">查看详情</RouterLink></header><p v-if="regions.logs.loading">加载中…</p><p v-else-if="regions.logs.error" class="notice">{{ regions.logs.error }}</p><p v-else class="overview-value">{{ regions.logs.value?.length ?? 0 }} 条</p><ul v-if="regions.logs.value?.length"><li v-for="entry in regions.logs.value.slice(0,3)" :key="entry.timestamp+entry.message">{{ entry.level }} · {{ entry.message }}</li></ul><footer>{{ regions.logs.updated || '尚未更新' }} <button :disabled="regions.logs.loading" @click="loadLogs">重试</button></footer></article>
      <article class="overview-card" data-testid="events-region"><header><div><span>Events</span><h3>最近事件</h3></div><RouterLink to="/admin/observability/events">查看详情</RouterLink></header><p v-if="regions.events.loading">加载中…</p><p v-else-if="regions.events.error" class="notice">{{ regions.events.error }}</p><p v-else class="overview-value">{{ regions.events.value?.length ?? 0 }} 条</p><ul v-if="regions.events.value?.length"><li v-for="entry in regions.events.value.slice(0,3)" :key="entry.timestamp+entry.event_name">{{ entry.severity }} · {{ entry.event_name }}</li></ul><footer>best-effort · {{ regions.events.updated || '尚未更新' }} <button :disabled="regions.events.loading" @click="loadEvents">重试</button></footer></article>
      <article class="overview-card" data-testid="exporter-region"><header><div><span>Exporter</span><h3>当前事实</h3></div><RouterLink to="/admin/observability/exporters">管理 Exporter</RouterLink></header><p v-if="regions.exporter.loading">加载中…</p><p v-else-if="regions.exporter.error" class="notice">{{ regions.exporter.error }}</p><template v-else-if="regions.exporter.value"><strong class="overview-value">{{ regions.exporter.value.observed_state }}</strong><p>期望 {{ regions.exporter.value.desired_state }} · v{{ regions.exporter.value.version }}</p></template><p v-else class="overview-value">未安装</p><footer>{{ regions.exporter.updated || '尚未更新' }} <button :disabled="regions.exporter.loading" @click="loadExporter">重试</button></footer></article>
    </div>
  </section>
</template>
