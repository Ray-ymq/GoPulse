<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AdminIcon from '../components/AdminIcon.vue'
import AdminStat from '../components/AdminStat.vue'
import AdminTrend from '../components/AdminTrend.vue'
import { observabilityApi } from '../services/observability'
import type { MetricResult, MetricName } from '../types/observability'
import { getOverview, type Overview } from '../services/overview'
const data = ref<Overview | null>(null), busy = ref(false), error = ref(false)
const links: Record<string, string> = { components:'/metrics', key_metrics:'/metrics', logs:'/logs', events:'/events', plugins:'/plugins', alerts:'/alerts' }
const titles: Record<string, string> = { components:'关键组件健康度', key_metrics:'关键指标快照', logs:'日志 · 15 分钟', events:'最近事件 · 15 分钟', plugins:'六插件', alerts:'告警概览' }
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
const summaries = computed(() => ['components', 'plugins', 'logs', 'alerts'].map(name => {
  const section = data.value?.sections[name]
  const rows = section?.items
  let value = '未知'
  if (section?.status === 'healthy' && Array.isArray(rows) && rows.length) {
    if (name === 'components') value = `${rows.filter(row => row.status === 'healthy').length} / ${rows.length}`
    if (name === 'plugins' && rows.every(row => typeof row.installed === 'boolean')) value = String(rows.filter(row => row.installed).length)
    if (name === 'logs' && rows.every(row => typeof row.value === 'number')) value = String(rows.reduce((sum, row) => sum + Number(row.value), 0))
  }
  if (name === 'alerts' && section?.status === 'healthy' && rows && !Array.isArray(rows)) value = String(Number(rows.warning_firing) + Number(rows.critical_firing))
  return { name, title: { components:'健康组件 / 已返回组件', plugins:'已安装 Exporter', logs:'日志数量 · 15 分钟', alerts:'当前触发告警' }[name], value, status: section?.status ?? 'unknown' }
}))
function display(v: unknown) {
  if (Array.isArray(v)) return v.length ? v.map(r => `rule ${r.rule_id} / revision ${r.revision} · ${r.severity} · ${new Date(r.recovered_at).toLocaleString('zh-CN', { hour12:false })}`).join('；') : '暂无近期恢复'
  return v === null ? '未知' : typeof v === 'string' && /^\d{4}-\d\d-\d\dT/.test(v) ? new Date(v).toLocaleString('zh-CN', { hour12:false }) : String(v)
}
const trendMetrics: { metric: MetricName; title: string }[] = [{metric:'gopulse_backend_outbox_pending',title:'Backend 待发送消息'},{metric:'gopulse_monitor_event_queue_length',title:'Monitor 事件队列'}]
const trends=ref<(MetricResult|null)[]>([null,null]),trendLoading=ref(false)
let controller:AbortController|null=null
function rows(name:string){const items=data.value?.sections[name]?.items;return Array.isArray(items)?items:[]}
function alertValue(key:string){const items=data.value?.sections.alerts?.items;return items&&!Array.isArray(items)?items[key]:null}
const componentTypes:Record<string,string>={backend:'业务 API', 'business-worker':'异步任务','search-indexer':'搜索索引',monitor:'监控服务',router:'消息路由',marshaller:'日志处理'}
async function load() {
  if (busy.value) return
  busy.value=true;error.value=false;data.value=null;trends.value=[null,null];trendLoading.value=true
  controller?.abort();controller=new AbortController()
  const requests=Promise.allSettled(trendMetrics.map(item=>observabilityApi.metrics(item.metric,'15m',controller!.signal)))
  try {data.value=await getOverview()}catch{error.value=true}finally{busy.value=false}
  const results=await requests
  if(controller.signal.aborted)return
  trends.value=results.map(result=>result.status==='fulfilled'?result.value:null);trendLoading.value=false
}
onMounted(load)
onBeforeUnmount(()=>controller?.abort())
</script>
<template>
  <section class="overview-page" :aria-busy="busy">
    <div class="admin-title"><div><h2>系统概览</h2><p>欢迎使用 GoPulse 管理中心，实时掌握系统运行状态。</p></div><div class="title-actions"><span class="range-label"><AdminIcon name="clock" />最近 15 分钟</span><button class="icon-button" aria-label="刷新大屏" :disabled="busy" @click="load"><AdminIcon name="refresh" /></button></div></div>
    <p v-if="busy" role="status">正在加载大屏…</p><p v-if="error" class="notice notice--error" role="alert">大屏加载失败，请重试。</p>
    <template v-if="data">
      <div class="summary-grid"><AdminStat v-for="(summary,index) in summaries" :key="summary.name" :label="summary.title!" :value="summary.value" :note="summary.status" :icon="['box','box','logs','alerts'][index]!" :tone="['green','blue','purple','red'][index]!" /></div>
      <article class="panel dashboard-section components-panel" data-section="components"><header class="panel-heading"><h3>核心组件状态</h3><RouterLink to="/metrics">查看组件指标 <AdminIcon name="arrow" :size="16" /></RouterLink></header><p class="section-status"><span class="status-label" :class="`status-label--${data.sections.components!.status}`">{{data.sections.components!.status}}</span><span>{{data.sections.components!.reason_code}}</span></p>
        <p v-if="data.sections.components!.reason_code==='invalid_response'">该分区响应无效，已隐藏未经验证的数据。</p>
        <div v-else class="table-scroll"><table><thead><tr><th>组件名称</th><th>类型</th><th>状态</th><th>观测时间</th><th>诊断原因</th></tr></thead><tbody><tr v-for="row in rows('components')" :key="String(row.id)"><td><span class="component-name"><AdminIcon :name="row.id==='monitor'?'pulse':'box'" />{{row.id}}</span></td><td>{{componentTypes[String(row.id)]}}</td><td><span class="status-label" :class="`status-label--${row.status}`">{{row.status}}</span></td><td>{{row.observed_at?new Date(String(row.observed_at)).toLocaleTimeString('zh-CN', { hour12:false }):'未知'}}</td><td>{{row.reason_code}}</td></tr></tbody></table></div>
      </article>
      <div class="overview-trends"><article v-for="(item,index) in trendMetrics" :key="item.metric" class="panel"><header class="panel-heading"><h3>{{item.title}}</h3><span class="muted">真实采样 · 15 分钟</span></header><p v-if="trendLoading" role="status">正在查询趋势…</p><AdminTrend v-else-if="trends[index]" :series="trends[index]!.series" :unit="trends[index]!.unit"/><div v-else class="chart-empty" role="status">趋势暂不可用，组件状态请以上方查询结果为准。</div></article></div>
      <div class="overview-bottom">
        <article class="panel dashboard-section" data-section="alerts"><header class="panel-heading"><h3><AdminIcon name="alerts" />告警概览</h3><RouterLink to="/alerts">查看全部 <AdminIcon name="arrow" :size="16"/></RouterLink></header><p class="section-status">{{data.sections.alerts!.status}} · {{data.sections.alerts!.reason_code}}</p><div class="table-scroll"><table><thead><tr><th>状态口径</th><th>数量</th><th>说明</th></tr></thead><tbody><tr v-for="key in ['warning_firing','critical_firing','recently_recovered']" :key="key"><td>{{key}}</td><td>{{alertValue(key)??'未知'}}</td><td>{{key==='recently_recovered'?'近期恢复':'当前触发'}}</td></tr></tbody></table></div><p class="panel-footnote">unknown / stale 不代表恢复。</p></article>
        <article class="panel dashboard-section" data-section="events"><header class="panel-heading"><h3><AdminIcon name="events" />最近事件</h3><RouterLink to="/events">查看全部 <AdminIcon name="arrow" :size="16"/></RouterLink></header><p class="section-status">{{data.sections.events!.status}} · {{data.sections.events!.reason_code}}</p><div class="table-scroll"><table><thead><tr><th>严重程度</th><th>数量</th><th>统计时间窗</th></tr></thead><tbody><tr v-for="row in rows('events')" :key="String(row.severity)"><td><span class="level" :class="`level--${row.severity}`">{{row.severity}}</span></td><td>{{row.value??'未知'}}</td><td>最近 15 分钟</td></tr></tbody></table></div><p class="panel-footnote">概览仅提供计数，事件详情请进入 Events。</p></article>
      </div>
      <details class="overview-extra"><summary>更多运行快照 · 关键指标、插件与日志</summary><div class="overview-extra-grid"><article v-for="name in ['key_metrics','plugins','logs']" :key="name" class="panel dashboard-section" :data-section="name"><header class="panel-heading"><h3>{{titles[name]}}</h3><RouterLink :to="links[name]!">查看详情</RouterLink></header><p>{{data.sections[name]!.status}} · {{data.sections[name]!.reason_code}}</p><p v-if="data.sections[name]!.reason_code==='invalid_response'">该分区响应无效，已隐藏未经验证的数据。</p><dl v-for="(row,index) in rows(name)" :key="index" class="overview-row"><dt>{{row.id??row.severity}}</dt><dd><span v-if="'value' in row">{{display(row.value)}} {{row.unit??''}}</span><span class="status-label" :class="`status-label--${row.status}`">{{row.status}}</span><details><summary>采样详情</summary><div v-for="(value,key) in row" :key="key">{{key}}：{{display(value)}}</div></details></dd></dl></article></div></details>
      <p class="last-updated">生成时间：{{new Date(data.generated_at).toLocaleString('zh-CN', { hour12:false })}} · {{timezone}} · {{data.status}}</p>
    </template>
  </section>
</template>
