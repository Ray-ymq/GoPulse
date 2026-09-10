<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { exporterApi, pluginConfigApi, validateExporterPackage } from '../services/exporters'
import { ApiError } from '../services/http'
import type { PluginCatalogItem } from '../services/exporters'
import { RouterLink } from 'vue-router'
import type { ExporterStatus } from '../types/exporter'

const catalog = ref<PluginCatalogItem[]>([])
const selected = ref('redis-exporter')
const selectedItem = computed(() => catalog.value.find(item => item.id === selected.value))
const configuration = reactive<Record<string, string | number>>({ host: 'redis', port: 6379, database: 0, connect_timeout: '1s', scrape_timeout: '2s' })
const password = ref('')
function selectPlugin(id: string): void { selected.value = id; password.value = ''; message.value = '' }
async function configure(kind: 'check' | 'install' | 'save'): Promise<void> {
  if (busy.value) return
  if (kind === 'save' && !window.confirm('替换配置将试启动并验证 Redis，失败时恢复原配置。是否继续？')) return
  operation.value = kind; message.value = ''
  const secrets: Record<string, string> = password.value ? { password: password.value } : {}
  try {
    if (kind === 'check') { await pluginConfigApi.check(configuration, secrets); message.value = '连接测试成功；尚未保存候选配置。' }
    else { status.value = await pluginConfigApi.save(configuration, secrets, kind === 'install'); catalog.value = await pluginConfigApi.catalog(); message.value = '配置已验证并保存。' }
  } catch (error) { message.value = errorMessage(error) }
  finally { password.value = ''; operation.value = '' }
}
const status = ref<ExporterStatus | null>(null)
const loading = ref(false)
const loaded = ref(false)
const operation = ref('')
const message = ref('')
const updatedAt = ref('')
const packageFile = ref<File | null>(null)
const packageInput = ref<HTMLInputElement | null>(null)
let controller: AbortController | null = null
const transitioning = computed(() => status.value !== null && ['installing','starting','stopping','updating'].includes(status.value.observed_state))
const busy = computed(() => loading.value || operation.value !== '' || transitioning.value)
const canStart = computed(() => status.value !== null && !busy.value && status.value.observed_state !== 'running' && status.value.desired_state !== 'running')
const canStop = computed(() => status.value !== null && !busy.value && (status.value.observed_state === 'running' || status.value.desired_state === 'running'))
function formatTime(value: string | null | undefined): string { return value ? new Date(value).toLocaleString() : '—' }
function errorMessage(error: unknown): string {
  if (!(error instanceof ApiError)) return 'Exporter 操作失败，请稍后重试。'
  return ({
    plugin_package_invalid:'安装包无效，请重新生成后再试。', plugin_not_found:'Exporter 尚未安装。', plugin_conflict:'当前状态不允许执行该操作。',
    plugin_operation_in_progress:'已有操作正在进行，请稍后刷新。', plugin_operation_failed:'Exporter 操作未完成，请查看安全错误后重试。',
    monitor_unavailable:'Monitor 暂时不可用，已保留上次成功状态。', permission_denied:'当前账号已无管理员权限。',
  } as Record<string,string>)[error.code] ?? 'Exporter 操作失败，请稍后重试。'
}
async function load(): Promise<void> {
  controller?.abort(); controller = new AbortController(); loading.value = true; message.value = ''
  try { const [items, descriptors] = await Promise.all([exporterApi.list(controller.signal), pluginConfigApi.catalog(controller.signal)]); catalog.value = descriptors; status.value = items.find(item => item.id === 'redis-exporter') ?? null; loaded.value = true; updatedAt.value = new Date().toLocaleString(); if (!items.length) message.value = '尚未安装 Redis Exporter，请配置目标并安装官方包。' }
  catch (error) { if (!controller.signal.aborted) message.value = errorMessage(error) }
  finally { loading.value = false }
}
function clearPackage(): void { packageFile.value = null; if (packageInput.value) packageInput.value.value = '' }
function selectPackage(event: Event): void { packageFile.value = (event.target as HTMLInputElement).files?.[0] ?? null; message.value = validateExporterPackage(packageFile.value) }
async function run(kind: 'install'|'update'|'start'|'stop'): Promise<void> {
  if (operation.value) return
  if ((kind === 'install' || kind === 'update')) { const issue = validateExporterPackage(packageFile.value); if (issue) { message.value = issue; return } }
  if ((kind === 'stop' && !window.confirm('停止 Exporter 将暂停新的 Redis 指标采集，是否继续？')) || (kind === 'update' && !window.confirm('更新会替换当前 Exporter 包并可能短暂中断采集，是否继续？'))) return
  operation.value = kind; message.value = ''
  try {
    const next = kind === 'install' ? await exporterApi.install(packageFile.value!) : kind === 'update' ? await exporterApi.update(packageFile.value!) : kind === 'start' ? await exporterApi.start() : await exporterApi.stop()
    status.value = next; updatedAt.value = new Date().toLocaleString()
    message.value = `${kind === 'install' ? '安装' : kind === 'update' ? '更新' : kind === 'start' ? '启动' : '停止'}请求已完成；当前状态以此处 DTO 为准，Events 记录可能稍后到达。`
  } catch (error) { message.value = errorMessage(error) }
  finally { if (kind === 'install' || kind === 'update') clearPackage(); operation.value = '' }
}
onMounted(load)
onBeforeUnmount(() => { controller?.abort(); clearPackage(); password.value = '' })
</script>
<template>
  <section :aria-busy="busy">
    <div class="admin-title"><div><p class="admin-eyebrow">MONITOR PLUGIN MANAGER</p><h2>官方 Exporter 插件</h2><p>当前状态来自 Monitor 的严格公共 DTO；Events 与历史 Metrics 仅用于后续核对。</p></div><button class="button" :disabled="busy" @click="load">{{ loading ? '刷新中…' : '刷新状态' }}</button></div>
    <p v-if="message" class="notice" role="status">{{ message }}</p>
    <div class="summary-grid" aria-label="官方插件目录">
      <button v-for="item in catalog" :key="item.id" class="panel" :disabled="busy" :aria-pressed="selected === item.id" @click="selectPlugin(item.id)">
        <strong>{{ item.name }}</strong><span>{{ item.available ? (item.configured ? '已配置' : '未安装') : '未交付' }}</span>
      </button>
    </div>
    <p v-if="selectedItem && !selectedItem.available" class="notice">此类型尚未交付，没有已安装或运行中的实例。</p>
    <div v-if="selectedItem?.available" class="panel exporter-configuration">
      <h3>Redis 目标配置</h3><p>连接测试不会保存候选配置；密码不会回填，提交后清空。配置替换留空密码表示保留。</p>
      <p v-if="selectedItem.summary === 'upgrade_required'">旧版包保持原状态，请先显式更新到 v2，再修改配置。</p>
      <label v-for="field in selectedItem.schema.fields" :key="field.name" class="field">
        {{ field.name }}
        <input v-if="field.secret" v-model="password" type="password" autocomplete="new-password" :aria-label="field.name" :disabled="busy">
        <input v-else-if="field.type === 'port' || field.type === 'integer'" v-model.number="configuration[field.name]" type="number" :min="field.minimum" :max="field.maximum" :aria-label="field.name" :disabled="busy">
        <input v-else v-model="configuration[field.name]" :aria-label="field.name" :disabled="busy">
      </label>
      <div class="exporter-actions">
        <button class="button" :disabled="busy || !password" @click="configure('check')">{{ operation === 'check' ? '测试中…' : '连接测试' }}</button>
        <button v-if="!status" class="button" :disabled="busy || !password" @click="configure('install')">安装并启动</button>
        <button v-else class="button" :disabled="busy || selectedItem.summary === 'upgrade_required'" @click="configure('save')">替换配置</button>
        <RouterLink to="/admin/observability/metrics?source=redis">查询 Redis 指标</RouterLink>
      </div>
    </div>
    <template v-if="status && selected === 'redis-exporter'">
      <div class="panel exporter-status">
        <div class="exporter-status__heading"><div><span class="state-pill" :class="`state-pill--${status.observed_state}`">{{ status.observed_state }}</span><h3>{{ status.name }}</h3><code>{{ status.id }} · v{{ status.version }}</code></div><div><span>期望状态</span><strong>{{ status.desired_state }}</strong></div></div>
        <div class="summary-grid exporter-details">
          <div><span>安装时间</span><strong>{{ formatTime(status.installed_at) }}</strong></div><div><span>更新时间</span><strong>{{ formatTime(status.updated_at) }}</strong></div>
          <div><span>启动时间</span><strong>{{ formatTime(status.started_at) }}</strong></div><div><span>页面刷新</span><strong>{{ updatedAt || '—' }}</strong></div>
          <div><span>最近采集</span><strong>{{ formatTime(status.last_scrape_at) }}</strong></div><div><span>最近成功</span><strong>{{ formatTime(status.last_success_at) }}</strong></div>
          <div><span>类型</span><strong>{{ status.kind }}</strong></div><div><span>来源</span><strong>{{ status.source }}</strong></div>
        </div>
        <div v-if="status.last_error" class="safe-error" role="alert"><strong>{{ status.last_error.code }}</strong><span>{{ status.last_error.message }}</span><time>{{ formatTime(status.last_error.at) }}</time></div>
        <div class="exporter-actions"><button class="button" :disabled="!canStart" @click="run('start')">{{ operation === 'start' ? '启动中…' : '启动' }}</button><button class="button button--secondary" :disabled="!canStop" @click="run('stop')">{{ operation === 'stop' ? '停止中…' : '停止' }}</button></div>
      </div>
      <div class="panel exporter-update"><div><h3>更新安装包</h3><p>更新会保留服务端安全校验与回滚语义。</p></div><label class="file-field">新的 .tar.gz 包<input ref="packageInput" type="file" accept=".tar.gz,application/gzip" @change="selectPackage"></label><button class="button" :disabled="busy" @click="run('update')">{{ operation === 'update' ? '更新中…' : '确认更新' }}</button></div>
    </template>
  </section>
</template>
