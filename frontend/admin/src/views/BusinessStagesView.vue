<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AdminIcon from '../components/AdminIcon.vue'
import ProductTime from '../../../shared/ProductTime.vue'
import { ApiError } from '../services/http'
import { businessStagesApi, validateBusinessStageQuery, type BusinessStageQuery } from '../services/businessStages'
import type { BusinessStageResponse, BusinessStageSource } from '../types/observability'

type SelectorKind = 'command_id' | 'event_id' | 'post_id'

const epoch = ref('')
const currentEpoch = ref('')
const contextLoading = ref(true)
const contextMessage = ref('')
const selectorKind = ref<SelectorKind>('command_id')
const selectorValues = ref<Record<SelectorKind, string>>({ command_id: '', event_id: '', post_id: '' })
const checkVisibility = ref(true)
const includeLogs = ref(true)
const includeTrace = ref(true)
const loading = ref(false)
const message = ref('')
const result = ref<BusinessStageResponse | null>(null)
const copiedTraceID = ref('')
const requestSerial = ref(0)
let requestController: AbortController | null = null
let contextController: AbortController | null = null
let queryWindow = { from: '', to: '' }

const selectorLabel: Record<SelectorKind, string> = { command_id: 'Command ID', event_id: 'Event ID', post_id: 'Post ID' }
const selectorPlaceholder: Record<SelectorKind, string> = {
  command_id: '例如 123e4567-e89b-12d3-a456-426614174001',
  event_id: '例如 123e4567-e89b-12d3-a456-426614174002',
  post_id: '例如 42',
}

const selectedValue = computed(() => selectorValues.value[selectorKind.value])
const hasContext = computed(() => currentEpoch.value !== '')
const logsLink = computed(() => {
  if (!result.value) return '/logs'
  const params = new URLSearchParams({ data_epoch: result.value.requested_epoch, from: queryWindow.from, to: queryWindow.to })
  const original = selectorValues.value[selectorKind.value]
  if (selectorKind.value === 'post_id') params.set('post_id', original)
  else params.set(selectorKind.value, original)
  return `/logs?${params.toString()}`
})

const traceIDs = computed(() => result.value?.evidence.trace.trace_ids ?? [])

function qualityLabel(value: string): string {
  return ({
    known: '已知依据', no_evidence: '暂无依据', legacy_unknown: '历史记录，代际未知', stale_epoch: '代际不匹配',
    expired: '依据已过期', not_requested: '未请求', not_configured: 'Trace 查询未配置', backend_error: '来源读取失败', sampling: '采样未观察到',
  } as Record<string, string>)[value] ?? '状态未知'
}

function qualityClass(value: string): string {
  if (value === 'known') return 'stage-quality stage-quality--known'
  if (value === 'backend_error') return 'stage-quality stage-quality--error'
  if (value === 'no_evidence' || value === 'not_requested') return 'stage-quality stage-quality--neutral'
  return 'stage-quality stage-quality--warning'
}

function statusText(source: BusinessStageSource): string {
  if (source.source === 'search_visibility') {
    if (source.quality !== 'known') return '当前可见性未知'
    return source.matched === true ? '当前查询可见' : source.matched === false ? '当前查询不可见' : '当前可见性未知'
  }
  if (source.source === 'logs') return source.entries?.length ? `已关联 ${source.entries.length} 条线索` : '没有匹配日志'
  if (source.source === 'trace') return source.trace_ids?.length ? `已观察 ${source.trace_ids.length} 个 Trace ID` : qualityLabel(source.quality)
  if (source.source === 'command') return source.status === 'committed' ? '命令已提交' : source.status || '提交结果未知'
  if (source.source === 'broker') return source.status === 'published' ? 'Broker 确认记录已写入' : source.status || 'Broker 结果未知'
  return source.status || source.result || '结果未知'
}

function metadata(source: BusinessStageSource): string[] {
  return [
    source.data_epoch ? `epoch ${source.data_epoch}` : '',
    source.projection_generation ? `generation ${source.projection_generation}` : '',
    source.routing_contract_revision ? `revision ${source.routing_contract_revision}` : '',
    source.consumer ? `consumer ${source.consumer}` : '',
  ].filter(Boolean)
}

function displayError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'business_stage_busy') return '查询超过当前预算，请稍后手动刷新。'
    if (error.code === 'business_stage_unavailable') return '阶段来源暂时不可用，请手动刷新。'
    if (error.code === 'authentication_required' || error.status === 401) return '管理会话已失效，请重新登录。'
    return error.message
  }
  return '阶段查询失败，请手动刷新。'
}

function chooseSelector(kind: SelectorKind): void {
  selectorKind.value = kind
  for (const key of Object.keys(selectorValues.value) as SelectorKind[]) if (key !== kind) selectorValues.value[key] = ''
  message.value = ''
}

function selector(): BusinessStageQuery['selector'] {
  const value = selectedValue.value.trim()
  if (selectorKind.value === 'post_id') return { post_id: Number(value) }
  return selectorKind.value === 'command_id' ? { command_id: value } : { event_id: value }
}

function abortQuery(): void {
  requestController?.abort()
  requestController = null
}

async function queryStage(): Promise<void> {
  abortQuery()
  const serial = ++requestSerial.value
  message.value = ''
  result.value = null
  const to = new Date().toISOString()
  const from = new Date(Date.now() - 15 * 60_000).toISOString()
  const query: BusinessStageQuery = {
    epoch: epoch.value.trim(),
    selector: selector(),
    check_visibility: checkVisibility.value,
    include_logs: includeLogs.value,
    include_trace: includeTrace.value,
    ...(includeLogs.value || includeTrace.value ? { from, to } : {}),
  }
  try {
    validateBusinessStageQuery(query)
  } catch (error) {
    message.value = error instanceof Error ? error.message : '查询条件无效。'
    return
  }
  const controller = new AbortController()
  requestController = controller
  loading.value = true
  try {
    const value = await businessStagesApi.query(query, controller.signal)
    if (serial !== requestSerial.value || controller.signal.aborted) return
    result.value = value
    queryWindow = { from, to }
  } catch (error) {
    if (controller.signal.aborted || serial !== requestSerial.value) return
    message.value = displayError(error)
  } finally {
    if (serial === requestSerial.value) loading.value = false
  }
}

async function loadContext(): Promise<void> {
  contextController?.abort()
  const controller = new AbortController()
  contextController = controller
  contextLoading.value = true
  contextMessage.value = ''
  try {
    const context = await businessStagesApi.context(controller.signal)
    if (controller.signal.aborted) return
    currentEpoch.value = context.data_epoch
    if (!epoch.value) epoch.value = context.data_epoch
  } catch (error) {
    if (!controller.signal.aborted) contextMessage.value = displayError(error)
  } finally {
    if (!controller.signal.aborted) contextLoading.value = false
  }
}

function useCurrentEpoch(): void { epoch.value = currentEpoch.value }

async function copyTrace(id: string): Promise<void> {
  if (!navigator.clipboard) return
  try {
    await navigator.clipboard.writeText(id)
    copiedTraceID.value = id
    window.setTimeout(() => { if (copiedTraceID.value === id) copiedTraceID.value = '' }, 1500)
  } catch { /* Clipboard access is optional; no synthetic trace link is created. */ }
}

function formatReason(source: BusinessStageSource): string {
  return source.reason ? `说明：${source.reason}` : ''
}

onMounted(loadContext)
onBeforeUnmount(() => { abortQuery(); contextController?.abort() })
</script>

<template>
  <section class="business-stages-page">
    <div class="admin-title">
      <div><p class="admin-eyebrow">BUSINESS STAGE EVIDENCE</p><h2>业务阶段查证</h2><p>按一个受限身份查看命令提交、Broker 记录、搜索应用和当前查询可见性。</p></div>
      <button class="button" :disabled="loading || contextLoading" @click="queryStage">{{ loading ? '查询中…' : '查证' }}</button>
    </div>

    <div class="panel stage-query-panel">
      <div class="stage-query-heading"><h3><AdminIcon name="search" />只读查询条件</h3><span class="muted">默认手动刷新，无持续轮询</span></div>
      <p v-if="contextLoading" class="muted" role="status">正在读取当前 epoch…</p>
      <p v-if="contextMessage" class="notice notice--error" role="alert">{{ contextMessage }}</p>
      <form class="stage-query-form" @submit.prevent="queryStage">
        <label>Data epoch<input v-model.trim="epoch" maxlength="36" required aria-label="Data epoch"><button type="button" class="button button--secondary stage-inline-button" :disabled="!hasContext" @click="useCurrentEpoch">使用当前</button></label>
        <fieldset><legend>身份（只能选择一种）</legend><div class="stage-selector-tabs"><button v-for="(label, kind) in selectorLabel" :key="kind" type="button" :class="['button', 'button--secondary', { 'is-selected': selectorKind === kind }]" @click="chooseSelector(kind as SelectorKind)">{{ label }}</button></div><label>{{ selectorLabel[selectorKind] }}<input v-model.trim="selectorValues[selectorKind]" :placeholder="selectorPlaceholder[selectorKind]" maxlength="36" required :aria-label="selectorLabel[selectorKind]"></label></fieldset>
        <div class="stage-options"><label><input v-model="checkVisibility" type="checkbox">检查当前搜索可见性</label><label><input v-model="includeLogs" type="checkbox">关联 Logs</label><label><input v-model="includeTrace" type="checkbox">关联 Trace 线索</label></div>
        <button class="button" type="submit" :disabled="loading || contextLoading">查证阶段</button>
      </form>
      <p class="muted stage-safe-note">页面不显示正文、命令键、request digest、Cookie、凭证或原始 SQL；来源失败和无依据会分别保留。</p>
    </div>

    <p v-if="message" class="notice notice--error" role="alert">{{ message }}</p>
    <div v-if="loading" class="panel"><p role="status">正在读取受限阶段依据…</p></div>
    <template v-if="result">
      <div class="summary-grid stage-summary" aria-label="阶段身份摘要">
        <div><span>请求 epoch</span><strong>{{ result.requested_epoch }}</strong></div><div><span>当前 epoch</span><strong>{{ result.current_epoch }}</strong></div><div><span>Command ID</span><strong>{{ result.resolved.command_id || '未知' }}</strong></div><div><span>Post ID</span><strong>{{ result.resolved.post_id || '未知' }}</strong></div>
      </div>

      <div class="stage-evidence-grid">
        <article v-for="source in [result.evidence.command, result.evidence.broker]" :key="source.source" class="panel stage-evidence-card">
          <header><div><p class="stage-card-kicker">{{ source.source === 'command' ? '命令提交' : 'Broker 确认记录' }}</p><h3>{{ statusText(source) }}</h3></div><span :class="qualityClass(source.quality)">{{ qualityLabel(source.quality) }}</span></header>
          <p v-if="formatReason(source)" class="stage-reason">{{ formatReason(source) }}</p><p v-if="source.read_at" class="muted">读取：<ProductTime :value="source.read_at" /></p><div class="stage-metadata"><code v-for="item in metadata(source)" :key="item">{{ item }}</code></div>
        </article>

        <article class="panel stage-evidence-card stage-evidence-card--wide">
          <header><div><p class="stage-card-kicker">搜索应用完成</p><h3>{{ result.evidence.consumers?.length ? '已记录必要消费者状态' : '必要消费者完成未知' }}</h3></div><span v-if="!result.evidence.consumers?.length" class="stage-quality stage-quality--warning">暂无完成依据</span></header>
          <div v-if="result.evidence.consumers?.length" class="stage-consumer-list"><div v-for="consumer in result.evidence.consumers" :key="`${consumer.consumer}-${consumer.routing_contract_revision}`" class="stage-consumer"><div><strong>{{ consumer.consumer }}</strong><span :class="qualityClass(consumer.quality)">{{ consumer.status === 'completed' ? '应用完成' : consumer.status || qualityLabel(consumer.quality) }}</span></div><p v-if="consumer.completed_at" class="muted">完成：<ProductTime :value="consumer.completed_at" /></p><div class="stage-metadata"><code v-if="consumer.projection_generation">generation {{ consumer.projection_generation }}</code><code>revision {{ consumer.routing_contract_revision }}</code><code v-if="consumer.result">result {{ consumer.result }}</code></div><p v-if="consumer.failure" class="stage-reason">重试 {{ consumer.failure.retry_count || 0 }} 次；{{ consumer.failure.last_reason || '失败原因未知' }}</p></div></div>
          <p v-else class="stage-unknown">已提交不等于搜索已应用；当前没有持久完成依据，不能推断消费者未执行。</p>
        </article>

        <article class="panel stage-evidence-card">
          <header><div><p class="stage-card-kicker">当前搜索可见</p><h3>{{ statusText(result.evidence.visibility) }}</h3></div><span :class="qualityClass(result.evidence.visibility.quality)">{{ qualityLabel(result.evidence.visibility.quality) }}</span></header>
          <p v-if="formatReason(result.evidence.visibility)" class="stage-reason">{{ formatReason(result.evidence.visibility) }}</p><p v-if="result.evidence.visibility.read_at" class="muted">读取：<ProductTime :value="result.evidence.visibility.read_at" /></p><div class="stage-metadata"><code v-for="item in metadata(result.evidence.visibility)" :key="item">{{ item }}</code></div>
        </article>

        <article class="panel stage-evidence-card">
          <header><div><p class="stage-card-kicker">日志线索</p><h3>{{ statusText(result.evidence.logs) }}</h3></div><span :class="qualityClass(result.evidence.logs.quality)">{{ qualityLabel(result.evidence.logs.quality) }}</span></header>
          <p v-if="formatReason(result.evidence.logs)" class="stage-reason">{{ formatReason(result.evidence.logs) }}</p><p v-if="result.evidence.logs.read_at" class="muted">读取：<ProductTime :value="result.evidence.logs.read_at" /></p><div class="stage-metadata"><code v-for="item in metadata(result.evidence.logs)" :key="item">{{ item }}</code></div><RouterLink class="button button--secondary stage-link" :to="logsLink">在 Logs 中查看精确关联</RouterLink>
        </article>

        <article class="panel stage-evidence-card stage-evidence-card--wide">
          <header><div><p class="stage-card-kicker">Trace 线索</p><h3>{{ traceIDs.length ? '已观察关联 ID' : qualityLabel(result.evidence.trace.quality) }}</h3></div><span :class="qualityClass(result.evidence.trace.quality)">{{ qualityLabel(result.evidence.trace.quality) }}</span></header>
          <p v-if="formatReason(result.evidence.trace)" class="stage-reason">{{ formatReason(result.evidence.trace) }}</p><p v-if="result.evidence.trace.quality === 'not_configured'" class="stage-unknown">后端未配置 Trace 查询能力；页面不会生成假链接。</p><ul v-if="traceIDs.length" class="trace-id-list"><li v-for="id in traceIDs" :key="id"><code>{{ id }}</code><button class="button button--secondary" type="button" @click="copyTrace(id)">{{ copiedTraceID === id ? '已复制' : '复制 ID' }}</button></li></ul>
        </article>
      </div>

      <div class="panel stage-budget"><header><h3>查证预算</h3><span :class="result.budget.at_eighty_percent ? 'stage-quality stage-quality--warning' : 'stage-quality stage-quality--known'">{{ result.budget.at_eighty_percent ? '接近水位' : '在预算内' }}</span></header><p>当前未闭合义务 {{ result.budget.current_open_requirements }} / {{ result.budget.current_open_limit }}；保留组 {{ result.budget.retained_groups }} / {{ result.budget.retained_groups_limit }}；计费字节 {{ result.budget.retained_bytes }} / {{ result.budget.retained_bytes_limit }}。</p></div>
    </template>
  </section>
</template>
