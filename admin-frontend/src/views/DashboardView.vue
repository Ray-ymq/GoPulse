<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getOverview, type Overview } from '../services/overview'
const data = ref<Overview | null>(null), busy = ref(false), error = ref(false)
const links: Record<string, string> = { components:'/metrics', key_metrics:'/metrics', logs:'/logs', events:'/events', plugins:'/plugins', alerts:'/alerts' }
const titles: Record<string, string> = { components:'关键组件健康度', key_metrics:'关键指标快照', logs:'日志 · 15 分钟', events:'最近事件 · 15 分钟', plugins:'六插件', alerts:'告警概览' }
const order = ['components', 'alerts', 'key_metrics', 'events', 'logs', 'plugins'] as const
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
  if (Array.isArray(v)) return v.length ? v.map(r => `rule ${r.rule_id} / revision ${r.revision} · ${r.severity} · ${new Date(r.recovered_at).toLocaleString()}`).join('；') : '暂无近期恢复'
  return v === null ? '未知' : typeof v === 'string' && /^\d{4}-\d\d-\d\dT/.test(v) ? new Date(v).toLocaleString() : String(v)
}
async function load() {
  if (busy.value) return
  busy.value = true; error.value = false; data.value = null
  try { data.value = await getOverview() } catch { error.value = true } finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section :aria-busy="busy">
    <div class="admin-title"><div><p class="admin-eyebrow">SYSTEM OVERVIEW</p><h2>管理大屏</h2><p>快速确认业务基础组件、采集链路与告警状态。</p></div><button class="button" :disabled="busy" @click="load">刷新大屏</button></div>
    <p v-if="busy" role="status">正在加载大屏…</p><p v-if="error" class="notice notice--error" role="alert">大屏加载失败，请重试。</p>
    <template v-if="data">
      <div class="summary-grid"><div v-for="summary in summaries" :key="summary.name"><span>{{ summary.title }}</span><strong>{{ summary.value }}</strong><span>{{ summary.status }}</span></div></div>
      <p class="time-window">生成时间：{{ new Date(data.generated_at).toLocaleString() }} · {{ timezone }} · {{ data.status }}</p>
      <div class="dashboard-grid">
        <article v-for="name in order" :key="name" class="dashboard-section" :data-section="name">
          <header><h3>{{ titles[name] }}</h3><RouterLink :to="links[name]!">查看详情</RouterLink></header>
          <p><span class="status-label" :class="`status-label--${data.sections[name]!.status}`">{{ data.sections[name]!.status }}</span> · {{ data.sections[name]!.reason_code }}</p>
          <p v-if="data.sections[name]!.reason_code === 'invalid_response'">该分区响应无效，已隐藏未经验证的数据。</p>
          <template v-else>
            <template v-if="Array.isArray(data.sections[name]!.items)">
              <p v-if="!data.sections[name]!.items.length">暂无可用数据</p>
              <dl v-for="(row, i) in data.sections[name]!.items" :key="i" class="overview-row">
                <dt>{{ row.id ?? row.severity }}</dt>
                <dd><span v-if="'value' in row" class="overview-number">{{ display(row.value) }} {{ row.unit ?? '' }}</span><span class="status-label" :class="`status-label--${row.status}`">{{ row.status }}</span>
                  <details><summary>采样详情</summary><div v-for="(v, k) in row" :key="k">{{ k }}：{{ display(v) }}</div></details>
                </dd>
              </dl>
            </template>
            <dl v-else><template v-for="(v, k) in data.sections[name]!.items" :key="k"><dt>{{ k }}</dt><dd>{{ display(v) }}</dd></template></dl>
            <p v-if="name === 'key_metrics'">当前 DTO 提供指标快照；历史趋势请查看 Metrics。</p>
            <p v-if="name === 'logs' || name === 'events'">计数 0 表示此时间窗无记录；未知不代表 0。</p>
            <p v-if="name === 'alerts'">firing 为 0 表示无当前告警；unknown 不代表已恢复。</p>
          </template>
        </article>
      </div>
    </template>
  </section>
</template>
