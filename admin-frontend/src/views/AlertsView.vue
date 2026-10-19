<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import AdminIcon from '../components/AdminIcon.vue'
import AdminStat from '../components/AdminStat.vue'
import ProductState from '../../../frontend-shared/ProductState.vue'
import ProductStatus from '../../../frontend-shared/ProductStatus.vue'
import { ApiError, requestValidatedData, requestValidatedPage, requestVoid } from '../services/http'
import { isCatalog, isIncident, isRule, type Catalog, type Incident, type Rule } from '../services/management'

type Input = {
  name: string
  source: string
  severity: string
  selector: { metric: string; labels: Record<string, string> }
  reducer: string
  operator: string
  threshold: number
  window: string
  for: string
  enabled?: boolean
  revision?: number
}

const catalog = ref<Catalog | null>(null)
const rules = ref<Rule[]>([])
const incidents = ref<Incident[]>([])
const tab = ref('current')
const busy = ref(false)
const message = ref('')
const error = ref(false)
const next = ref<string | null>(null)
const editing = ref<Rule | null>(null)
const showForm = ref(false)
const selectedId = ref<number | null>(null)
const detail = ref<HTMLElement | null>(null)
const drawer = ref<HTMLElement | null>(null)
const formTrigger = ref<HTMLElement | null>(null)
const returnFocus = ref<HTMLElement | null>(null)
const severityFilter = ref('')
const statusFilter = ref('')
const search = ref('')
const loaded = ref(false)
let selectionButton: HTMLElement | null = null

const tabs = [
  { value: 'current', label: '当前告警' },
  { value: 'history', label: '历史告警' },
  { value: 'rules', label: 'rules' },
]
const selectedIncident = computed(() => incidents.value.find(row => row.id === selectedId.value))
const visibleIncidents = computed(() => incidents.value.filter(row =>
  (!severityFilter.value || row.severity === severityFilter.value)
  && (!statusFilter.value || row.status === statusFilter.value)
  && (!search.value || row.name.includes(search.value)),
))

async function selectIncident(id: number, event: Event): Promise<void> {
  selectionButton = event.currentTarget as HTMLElement
  selectedId.value = id
  await nextTick()
  detail.value?.focus()
}

function closeDetail(): void {
  selectedId.value = null
  selectionButton?.focus()
}

watch([severityFilter, statusFilter, search], () => { selectedId.value = null })

const source = ref('metrics')
const metric = ref('')
const tuple = ref(0)
const name = ref('')
const severity = ref('warning')
const reducer = ref('last')
const operator = ref('gt')
const threshold = ref(0)
const windowSize = ref('1m')
const forSize = ref('0s')
const definition = computed(() => catalog.value?.metrics.find(item => item.metric === metric.value))
const selectors = computed<Record<string, string>[]>(() => {
  if (!catalog.value) return []
  if (source.value === 'metrics') {
    const current = definition.value
    return current ? current.allowed_tuples.map(values => Object.fromEntries(current.label_keys.map((key, index) => [key, values[index]!]))) : []
  }
  const current = source.value === 'logs' ? catalog.value.logs : catalog.value.events
  return current.allowed_combinations.length
    ? current.allowed_combinations
    : Object.entries(current.fields).flatMap(([key, values]) => values.map(value => ({ [key]: value })))
})
const reducers = computed(() => source.value === 'metrics' ? definition.value?.reducers ?? [] : ['count'])
const durations = computed(() => catalog.value?.for.filter(value => value === '0s' || value === '1m' || windowSize.value !== '1m') ?? [])

watch(source, () => { tuple.value = 0; reducer.value = source.value === 'metrics' ? 'last' : 'count' })
watch(metric, () => { tuple.value = 0; reducer.value = definition.value?.reducers[0] ?? 'last' })
watch(windowSize, () => { if (!durations.value.some(value => value === forSize.value)) forSize.value = '0s' })

async function load(more = false): Promise<void> {
  if (busy.value) return
  busy.value = true
  error.value = false
  if (!more) {
    message.value = ''
    selectedId.value = null
  }
  try {
    if (!catalog.value) {
      catalog.value = await requestValidatedData('/alerts/catalog', isCatalog)
      metric.value = catalog.value.metrics[0]?.metric ?? ''
    }
    const query = more && next.value ? `?cursor=${encodeURIComponent(next.value)}` : ''
    const page = await requestValidatedPage<Rule | Incident>(`/alerts/${tab.value}${query}`, tab.value === 'rules' ? isRule : isIncident)
    if (tab.value === 'rules') rules.value = more ? [...rules.value, ...page.data as Rule[]] : page.data as Rule[]
    else incidents.value = more ? [...incidents.value, ...page.data as Incident[]] : page.data as Incident[]
    next.value = page.nextCursor
    loaded.value = true
    if (!more && tab.value !== 'rules' && window.innerWidth > 1000) selectedId.value = incidents.value[0]?.id ?? null
  } catch {
    error.value = true
    message.value = '告警读取失败，请检查查询依赖后重试。'
  } finally {
    busy.value = false
  }
}

async function selectTab(value: string): Promise<void> {
  tab.value = value
  loaded.value = false
  selectedId.value = null
  next.value = null
  incidents.value = []
  rules.value = []
  await load()
}

async function focusDrawer(): Promise<void> {
  await nextTick()
  drawer.value?.focus()
}

async function create(): Promise<void> {
  returnFocus.value = document.activeElement as HTMLElement | null
  editing.value = null
  name.value = ''
  showForm.value = true
  await focusDrawer()
}

async function edit(rule: Rule): Promise<void> {
  returnFocus.value = document.activeElement as HTMLElement | null
  editing.value = rule
  source.value = rule.source
  metric.value = rule.selector.metric
  await nextTick()
  tuple.value = Math.max(0, selectors.value.findIndex(item => JSON.stringify(Object.entries(item).sort()) === JSON.stringify(Object.entries(rule.selector.labels).sort())))
  name.value = rule.name
  severity.value = rule.severity
  reducer.value = rule.reducer
  operator.value = rule.operator
  threshold.value = rule.threshold
  windowSize.value = rule.window
  forSize.value = rule.for
  showForm.value = true
  await focusDrawer()
}

function closeForm(): void {
  if (busy.value) return
  showForm.value = false
  editing.value = null
  restoreFormFocus()
}

function restoreFormFocus(): void {
  const trigger = returnFocus.value ?? formTrigger.value
  returnFocus.value = null
  void nextTick(() => trigger?.focus())
}

async function failed(exception: unknown): Promise<void> {
  error.value = true
  if (exception instanceof ApiError && exception.status === 409) {
    showForm.value = false
    await load()
    message.value = '规则已被其他管理员修改，请重新读取后操作。'
  } else {
    message.value = '操作失败，请检查规则或稍后重试。'
  }
}

async function save(): Promise<void> {
  if (busy.value || !selectors.value[tuple.value]) return
  if (editing.value && !window.confirm('更新会关闭当前触发中的告警并重置评估状态，确认更新？')) return
  busy.value = true
  error.value = false
  try {
    const body: Input = {
      name: name.value,
      source: source.value,
      severity: severity.value,
      selector: { metric: source.value === 'metrics' ? metric.value : '', labels: selectors.value[tuple.value]! },
      reducer: reducer.value,
      operator: operator.value,
      threshold: threshold.value,
      window: windowSize.value,
      for: forSize.value,
    }
    if (editing.value) body.revision = editing.value.revision
    else body.enabled = true
    await requestValidatedData(`/alerts/rules${editing.value ? `/${editing.value.id}` : ''}`, isRule, {
      method: editing.value ? 'PUT' : 'POST',
      body: JSON.stringify(body),
    })
    showForm.value = false
    editing.value = null
    restoreFormFocus()
    await load()
    message.value = '规则已保存。'
  } catch (exception) {
    await failed(exception)
  } finally {
    busy.value = false
  }
}

async function mutate(rule: Rule, action: string): Promise<void> {
  if (busy.value) return
  if (action !== 'enable' && !window.confirm(`此操作会关闭当前触发中的告警，确认${action === 'delete' ? '删除' : action === 'disable' ? '停用' : action}？`)) return
  busy.value = true
  error.value = false
  try {
    const init = { method: action === 'delete' ? 'DELETE' : 'POST', body: JSON.stringify({ revision: rule.revision }) }
    const path = `/alerts/rules/${rule.id}${action === 'delete' ? '' : `/${action}`}`
    if (action === 'delete') await requestVoid(path, init)
    else await requestValidatedData(path, isRule, init)
    await load()
    message.value = '操作成功。'
  } catch (exception) {
    await failed(exception)
  } finally {
    busy.value = false
  }
}

onMounted(() => void load())
</script>

<template>
  <section class="alerts-page" :aria-busy="busy">
    <div class="admin-title">
      <div>
        <h2>Alerts</h2>
        <p>告警监控与事件处理，及时发现并处理系统异常。</p>
      </div>
      <button class="icon-button" type="button" aria-label="刷新告警" :disabled="busy" @click="load()"><AdminIcon name="refresh" /></button>
    </div>

    <p v-if="message && !error" class="notice" role="status">{{ message }}</p>
    <p v-if="message && error" class="notice notice--error" role="alert">{{ message }} <button class="inline-action" type="button" @click="load()">重试</button></p>
    <ProductState v-if="error && !loaded" state="unavailable" :message="message" action-label="重试" @retry="load()" />

    <nav class="alert-tabs" aria-label="告警视图">
      <button v-for="item in tabs" :key="item.value" type="button" :disabled="busy" :aria-pressed="tab === item.value" @click="selectTab(item.value)">{{ item.label }}</button>
    </nav>

    <template v-if="tab === 'rules'">
      <button ref="formTrigger" class="button" type="button" :disabled="busy || !catalog" @click="create">创建规则</button>
      <div v-if="loaded && !rules.length && !busy && !error" class="alert-empty"><ProductState state="empty" message="还没有配置告警规则，可先创建一条规则。" /></div>
      <div class="alert-rule-list">
        <article v-for="rule in rules" :key="rule.id" class="panel" :data-rule-id="rule.id">
          <header class="panel-heading">
            <div><h3>{{ rule.name }}</h3><p class="muted">规则 #{{ rule.id }} · 修订 {{ rule.revision }}</p></div>
            <ProductStatus :status="rule.enabled ? rule.evaluation.state : 'disabled'" />
          </header>
          <p>{{ rule.source }} · <ProductStatus :status="rule.severity" /> · <ProductStatus :status="rule.evaluation.data_status" /></p>
          <p class="safe-message">{{ JSON.stringify(rule.selector) }}</p>
          <div class="exporter-actions">
            <button class="button button--secondary" type="button" :disabled="busy" @click="edit(rule)">编辑</button>
            <button class="button button--secondary" type="button" :disabled="busy" @click="mutate(rule, rule.enabled ? 'disable' : 'enable')">{{ rule.enabled ? '停用' : '启用' }}</button>
            <button class="button button--danger" type="button" :disabled="busy" @click="mutate(rule, 'delete')">删除</button>
          </div>
        </article>
      </div>

      <div v-if="showForm && catalog" class="alert-drawer-backdrop" @click.self="closeForm">
        <aside ref="drawer" class="panel alert-drawer" role="dialog" aria-modal="true" aria-labelledby="alert-rule-editor-title" tabindex="-1" @keydown.esc="closeForm">
          <header class="panel-heading">
            <div><h3 id="alert-rule-editor-title">{{ editing ? '编辑告警规则' : '创建告警规则' }}</h3><p class="muted">保存前确认规则的影响范围；更新规则会重置评估状态。</p></div>
            <button class="icon-button" type="button" aria-label="关闭规则编辑" @click="closeForm"><AdminIcon name="close" /></button>
          </header>
          <form class="filter-grid alert-rule-form" @submit.prevent="save">
            <label>名称<input v-model="name" required maxlength="80" /></label>
            <label>来源<select v-model="source"><option v-for="item in catalog.creatable_sources" :key="item" :value="item">{{ item }}</option></select></label>
            <label v-if="source === 'metrics'">指标<select v-model="metric"><option v-for="item in catalog.metrics" :key="item.metric" :value="item.metric">{{ item.metric }}</option></select></label>
            <label>固定选择器<select v-model="tuple"><option v-for="(item, index) in selectors" :key="index" :value="index">{{ JSON.stringify(item) }}</option></select></label>
            <label>聚合<select v-model="reducer"><option v-for="item in reducers" :key="item" :value="item">{{ item }}</option></select></label>
            <label>运算<select v-model="operator"><option v-for="item in catalog.operators" :key="item" :value="item">{{ item }}</option></select></label>
            <label>阈值<input v-model.number="threshold" type="number" step="any" required /></label>
            <label>窗口<select v-model="windowSize"><option v-for="item in catalog.windows" :key="item" :value="item">{{ item }}</option></select></label>
            <label>持续<select v-model="forSize"><option v-for="item in durations" :key="item" :value="item">{{ item }}</option></select></label>
            <label>严重度<select v-model="severity"><option v-for="item in catalog.severities" :key="item" :value="item">{{ item === 'critical' ? '严重' : '需要关注' }}</option></select></label>
            <div class="form-actions"><button class="button" type="submit" :disabled="busy">{{ busy ? '保存中…' : '保存规则' }}</button><button class="button button--secondary" type="button" :disabled="busy" @click="closeForm">取消</button></div>
          </form>
        </aside>
      </div>
    </template>

    <template v-else>
      <div class="summary-grid" aria-label="已加载告警摘要">
        <AdminStat label="触发中" :value="loaded ? incidents.filter(row => row.status === 'firing').length : '未知'" note="已加载的触发中告警" icon="alerts" tone="red" />
        <AdminStat label="需要关注" :value="loaded ? incidents.filter(row => row.severity === 'warning').length : '未知'" note="已加载的关注级别" icon="warning" tone="orange" />
        <AdminStat label="已恢复" :value="loaded ? incidents.filter(row => row.status === 'recovered').length : '未知'" note="已加载的已恢复告警" icon="check" tone="green" />
        <AdminStat label="已关闭" :value="loaded ? incidents.filter(row => row.status === 'closed').length : '未知'" note="已加载的已关闭告警" icon="stop" tone="purple" />
      </div>

      <div v-if="loaded && !visibleIncidents.length && !busy && !error" class="alert-empty"><ProductState state="empty" message="当前筛选条件下暂无告警记录。" /></div>
      <div class="list-detail-layout" :class="{ 'has-detail': selectedIncident }">
        <div class="panel incident-list">
          <header class="panel-heading"><h3>告警列表</h3></header>
          <form class="filter-bar alert-filters" @submit.prevent>
            <label>严重程度<select v-model="severityFilter"><option value="">全部</option><option value="warning">需要关注</option><option value="critical">严重</option></select></label>
            <label>生命周期状态<select v-model="statusFilter"><option value="">全部</option><option value="firing">触发中</option><option value="recovered">已恢复</option><option value="closed">已关闭</option></select></label>
            <label>名称（已加载记录）<input v-model.trim="search" /></label>
          </form>
          <div class="table-scroll"><table><thead><tr><th>严重程度</th><th>告警名称</th><th>来源</th><th>状态</th><th>最近触发</th></tr></thead><tbody>
            <tr v-for="incident in visibleIncidents" :key="incident.id" :class="{ 'is-selected': selectedId === incident.id }">
              <td><ProductStatus :status="incident.severity" /></td>
              <td class="message-cell"><button class="row-select" type="button" :title="incident.name" :aria-pressed="selectedId === incident.id" @click="selectIncident(incident.id, $event)">{{ incident.name }}</button></td>
              <td>{{ incident.source }}</td>
              <td><ProductStatus :status="incident.status" /><small v-if="incident.data_status !== 'ok'" class="muted"><ProductStatus :status="incident.data_status" /></small></td>
              <td>{{ new Date(incident.last_triggered_at).toLocaleTimeString('zh-CN', { hour12: false }) }}</td>
            </tr>
          </tbody></table></div>
        </div>

        <aside v-if="selectedIncident" ref="detail" class="panel detail-panel" aria-label="告警详情" tabindex="-1" @keydown.esc="closeDetail">
          <header><div class="detail-heading-copy"><h3>{{ selectedIncident.name }}</h3><ProductStatus :status="selectedIncident.status" /></div><button class="icon-button" type="button" aria-label="关闭详情" @click="closeDetail"><AdminIcon name="close" /></button></header>
          <div class="detail-section-label">详情</div>
          <h4>基本信息</h4>
          <dl class="detail-fields"><dt>严重程度</dt><dd><ProductStatus :status="selectedIncident.severity" /></dd><dt>生命周期状态</dt><dd><ProductStatus :status="selectedIncident.status" /></dd><dt>数据状态</dt><dd><ProductStatus :status="selectedIncident.data_status" /></dd><dt>规则 / 修订</dt><dd>{{ selectedIncident.rule_id }} / {{ selectedIncident.revision }}</dd><dt>来源</dt><dd>{{ selectedIncident.source }}</dd><dt>首次触发</dt><dd>{{ new Date(selectedIncident.first_triggered_at).toLocaleString('zh-CN', { hour12: false }) }}</dd><dt>最近触发</dt><dd>{{ new Date(selectedIncident.last_triggered_at).toLocaleString('zh-CN', { hour12: false }) }}</dd><dt>最近值</dt><dd>{{ selectedIncident.last_value ?? '未知' }}</dd><dt>评估次数</dt><dd>{{ selectedIncident.evaluation_count }}</dd><dt>恢复时间</dt><dd>{{ selectedIncident.recovered_at ? new Date(selectedIncident.recovered_at).toLocaleString('zh-CN', { hour12: false }) : '—' }}</dd><dt>关闭时间 / 原因</dt><dd>{{ selectedIncident.closed_at ? new Date(selectedIncident.closed_at).toLocaleString('zh-CN', { hour12: false }) : '—' }} {{ selectedIncident.resolution_reason }}</dd></dl>
          <h4>触发对象</h4><pre class="safe-message">{{ selectedIncident.object.metric || selectedIncident.source }}</pre>
          <h4>标签（Labels）</h4><dl class="safe-tags"><div v-for="(value, key) in selectedIncident.object.labels" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></div></dl>
          <p class="panel-footnote">状态来自实际评估记录；不提供静默、手工确认或关联 Trace。</p>
        </aside>
      </div>
    </template>

    <button v-if="next" class="button button--secondary" type="button" :disabled="busy" @click="load(true)">{{ busy ? '加载中…' : '更多告警' }}</button>
  </section>
</template>
