<script setup lang="ts">
import AdminIcon from '../components/AdminIcon.vue'
import AdminStat from '../components/AdminStat.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { exporterApi, pluginConfigApi, validateExporterPackage } from '../services/exporters'
import { ApiError } from '../services/http'
import type { PluginCatalogItem } from '../services/exporters'
import { RouterLink } from 'vue-router'
import type { ExporterStatus } from '../types/exporter'

const catalog = ref<PluginCatalogItem[]>([])
const selected = ref('redis-exporter')
const selectedItem = computed(() => catalog.value.find(item => item.id === selected.value))
const configuration = reactive<Record<string, string | number>>({ host: '', port: 6379, database: 0, connect_timeout: '1s', scrape_timeout: '2s' })
const password = ref('')
const statuses = ref<ExporterStatus[]>([])
const pluginSearch=ref(''),pluginState=ref(''),showUpdate=ref(false)
function pluginStatus(id:string){return statuses.value.find(item=>item.id===id)}
function pluginIcon(source:string){return source==='mysql'?'database':source || 'box'}
function pluginName(source:string){return ({redis:'Redis',mysql:'MySQL',rabbitmq:'RabbitMQ',kafka:'Kafka',elasticsearch:'Elasticsearch',victoriametrics:'VictoriaMetrics'}[source] ?? source)+' Exporter'}
function fieldLabel(name:string){return ({host:'目标地址',port:'端口',password:'密码',username:'用户名',database:'数据库',connect_timeout:'连接超时',scrape_timeout:'采集超时',topic:'Topic',consumer_group:'Consumer Group',management_port:'管理端口',vhost:'Virtual Host'} as Record<string,string>)[name] ?? name}
const visiblePlugins=computed(()=>catalog.value.filter(item=>(!pluginSearch.value||`${item.name} ${item.id}`.toLowerCase().includes(pluginSearch.value.toLowerCase()))&&(!pluginState.value||(pluginStatus(item.id)?.observed_state ?? 'not_installed')===pluginState.value)))
function selectPlugin(id: string): void {
 showUpdate.value = false; selected.value = id; password.value = ''; message.value = ''; clearPackage()
 status.value = statuses.value.find(item => item.id === id) ?? null
 for (const key of Object.keys(configuration)) delete configuration[key]
 const source = id.replace('-exporter', '')
 Object.assign(configuration, { host: '', connect_timeout: '1s', scrape_timeout: '2s' }, source === 'redis' ? { port: 6379, database: 0 } : source === 'mysql' ? { port: 3306, database: '', username: '' } : source === 'rabbitmq' ? { management_port: 15672, vhost: '/', username: '' } : source === 'kafka' ? { port: 19092, topic: '', consumer_group: '' } : source === 'victoriametrics' ? { port: 8428, username: '' } : { port: 9200 })
}
async function configure(kind: 'check' | 'install' | 'save'): Promise<void> {
  if (busy.value) return
  if (kind === 'save' && !window.confirm('替换配置将试启动并验证所选插件，失败时恢复原配置。是否继续？')) return
  operation.value = kind; message.value = ''
  const candidate = Object.fromEntries(Object.entries(configuration).filter(([key, value]) => key !== 'username' || value !== ''))
  const secrets: Record<string, string> = password.value ? { password: password.value } : {}
  try {
    if (kind === 'check') { await pluginConfigApi.check(candidate, secrets, selected.value); message.value = '连接测试成功；尚未保存候选配置。' }
    else { status.value = await pluginConfigApi.save(candidate, secrets, kind === 'install', selected.value); catalog.value = await pluginConfigApi.catalog(); statuses.value = await exporterApi.list(); message.value = '配置已验证并保存。' }
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
function formatTime(value: string | null | undefined): string { return value ? new Date(value).toLocaleString('zh-CN', { hour12:false }) : '—' }
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
  try { const [items, descriptors] = await Promise.all([exporterApi.list(controller.signal), pluginConfigApi.catalog(controller.signal)]); catalog.value = descriptors; statuses.value = items; status.value = items.find(item => item.id === selected.value) ?? null; loaded.value = true; updatedAt.value = new Date().toLocaleString('zh-CN', { hour12:false }); if (!items.length) message.value = '尚未安装 Redis Exporter，请配置目标并安装官方包。' }
  catch (error) { if (!controller.signal.aborted) message.value = errorMessage(error) }
  finally { loading.value = false }
}
function clearPackage(): void { packageFile.value = null; if (packageInput.value) packageInput.value.value = '' }
function selectPackage(event: Event): void { packageFile.value = (event.target as HTMLInputElement).files?.[0] ?? null; message.value = validateExporterPackage(packageFile.value) }
async function run(kind: 'install'|'update'|'start'|'stop'): Promise<void> {
  if (operation.value) return
  if ((kind === 'install' || kind === 'update')) { const issue = validateExporterPackage(packageFile.value); if (issue) { message.value = issue; return } }
  if ((kind === 'stop' && !window.confirm('停止 Exporter 将暂停所选插件的新指标采集，是否继续？')) || (kind === 'update' && !window.confirm('更新会替换当前 Exporter 包并可能短暂中断采集，是否继续？'))) return
  operation.value = kind; message.value = ''
  try {
    const next = kind === 'install' ? await exporterApi.install(packageFile.value!) : kind === 'update' ? await exporterApi.update(packageFile.value!, selected.value) : kind === 'start' ? await exporterApi.start(selected.value) : await exporterApi.stop(selected.value)
    status.value = next; statuses.value = [...statuses.value.filter(item => item.id !== next.id), next]; updatedAt.value = new Date().toLocaleString('zh-CN', { hour12:false })
    message.value = `${kind === 'install' ? '安装' : kind === 'update' ? '更新' : kind === 'start' ? '启动' : '停止'}请求已完成；当前状态以此处 DTO 为准，Events 记录可能稍后到达。`
    if (kind === 'update') {
      try { catalog.value = await pluginConfigApi.catalog() }
      catch { message.value = '更新已成功，但插件目录刷新失败；请点击“刷新状态”同步配置门禁，不要重复上传安装包。' }
    }
  } catch (error) { message.value = errorMessage(error) }
  finally { if (kind === 'install' || kind === 'update') clearPackage(); operation.value = '' }
}
onMounted(load)
onBeforeUnmount(() => { controller?.abort(); clearPackage(); password.value = '' })
</script>
<template>
  <section :aria-busy="busy" class="exporter-page">
    <div class="admin-title"><div><h2>Exporter</h2><p>管理数据采集插件的生命周期，配置目标数据源并监控运行状态。</p></div><button class="button button--secondary" :disabled="busy" @click="load"><AdminIcon name="refresh" />{{loading?'刷新中…':'刷新状态'}}</button></div>
    <p v-if="message" class="notice" role="status">{{message}}</p>
    <div class="summary-grid" aria-label="已安装插件状态摘要">
      <AdminStat label="全部" :value="loaded?statuses.length:'未知'" note="已安装的 Exporter 总数" icon="box" />
      <AdminStat v-for="(state,index) in ['running','stopped','failed']" :key="state" :label="state" :value="loaded?statuses.filter(item=>item.observed_state===state).length:'未知'" note="已安装插件的实际状态" :icon="['play','stop','warning'][index]!" :tone="index===0?'green':'red'" />
    </div>
    <div class="exporter-master-detail">
      <div class="panel exporter-list-panel"><h3>Exporter 列表</h3>
        <div class="list-filters"><label class="search-field"><AdminIcon name="search" /><input v-model.trim="pluginSearch" aria-label="搜索 Exporter" placeholder="搜索名称或类型…"></label><label><span class="sr-only">插件状态</span><select v-model="pluginState"><option value="">全部状态</option><option value="running">running</option><option value="stopped">stopped</option><option value="failed">failed</option><option value="not_installed">未安装</option></select></label></div>
        <div class="exporter-catalog" aria-label="官方插件目录">
          <button v-for="item in visiblePlugins" :key="item.id" :disabled="busy" :aria-pressed="selected===item.id" @click="selectPlugin(item.id)">
            <AdminIcon :name="pluginIcon(item.source)" :size="32" :class="`plugin-symbol plugin-${item.source}`" />
            <span class="plugin-name"><strong>{{pluginName(item.source)}}</strong><small>{{item.id}}</small></span>
            <span class="plugin-meta"><span class="status-label" :class="`status-label--${pluginStatus(item.id)?.observed_state ?? 'unknown'}`">{{pluginStatus(item.id)?.observed_state ?? (item.available?'未安装':'未交付')}}</span><small>{{pluginStatus(item.id)?'v'+pluginStatus(item.id)!.version:'尚无实例'}}</small></span>
          </button>
        </div><p v-if="!visiblePlugins.length" class="empty-inline">暂无符合条件的插件</p><footer class="list-footer">目录 {{catalog.length}} 项 · 当前显示 {{visiblePlugins.length}} 项</footer>
      </div>
      <div class="panel exporter-workspace">
        <header class="exporter-heading"><div><AdminIcon :name="pluginIcon(selectedItem?.source ?? '')" :size="32" :class="`plugin-symbol plugin-${selectedItem?.source}`"/><h3>{{pluginName(selectedItem?.source ?? '')}}</h3><span v-if="status" class="state-pill" :class="`state-pill--${status.observed_state}`">{{status.observed_state}}</span></div><div v-if="status" class="exporter-actions"><button class="button button--secondary" :disabled="!canStart" @click="run('start')"><AdminIcon name="play" />{{operation==='start'?'启动中…':'启动'}}</button><button class="button button--danger" :disabled="!canStop" @click="run('stop')"><AdminIcon name="stop" />{{operation==='stop'?'停止中…':'停止'}}</button><button class="icon-button" aria-label="更新安装包" :disabled="busy" @click="showUpdate=!showUpdate"><AdminIcon name="refresh" /></button></div></header>
        <nav class="detail-tabs" aria-label="Exporter 详情"><a href="#exporter-info">基本信息</a><a href="#exporter-config">配置参数</a><RouterLink v-if="selectedItem" :to="`/metrics?source=${selectedItem.source}`">采集指标</RouterLink></nav>
        <div id="exporter-info" v-if="status && status.id===selected" class="exporter-status">
          <dl class="exporter-info-grid"><div><dt>Exporter 名称</dt><dd>{{status.name}}</dd></div><div><dt>组件类型</dt><dd>{{status.kind}}</dd></div><div><dt>当前状态</dt><dd>{{status.observed_state}}</dd></div><div><dt>版本</dt><dd>v{{status.version}}</dd></div><div><dt>期望状态</dt><dd>{{status.desired_state}}</dd></div><div><dt>最近采集</dt><dd>{{formatTime(status.last_scrape_at)}}</dd></div><div><dt>最近成功</dt><dd>{{formatTime(status.last_success_at)}}</dd></div><div><dt>安装时间</dt><dd>{{formatTime(status.installed_at)}}</dd></div><div><dt>启动时间</dt><dd>{{formatTime(status.started_at)}}</dd></div><div><dt>来源</dt><dd>{{status.source}}</dd></div></dl>
          <div v-if="status.last_error" class="safe-error" role="alert"><strong>{{status.last_error.code}}</strong><span>{{status.last_error.code==='network_failed'?'插件目标不可达或拒绝采集；进程运行不代表目标健康。':status.last_error.message}}</span><time>{{formatTime(status.last_error.at)}}</time></div>
          <p v-if="status.source==='victoriametrics'" class="muted">目标故障与指标存储/查询不可用是不同状态；存储不可用期间以此安全状态为准。</p>
        </div><p v-else id="exporter-info" class="empty-inline">{{selectedItem?.available?'尚未安装此插件，请配置实际目标后安装。':'此类型尚未交付。'}}</p>
        <div v-if="selectedItem?.available" id="exporter-config" class="exporter-configuration">
          <h3>配置参数</h3><p class="muted">填写实际目标地址。密码不回填；配置替换留空密码表示保留，提交后清空。</p><p v-if="selectedItem.summary==='upgrade_required'" class="notice">旧版包保持原状态，请先显式更新到 v2，再修改配置。</p>
          <label v-for="field in selectedItem.schema.fields" :key="field.name" class="field"><span>{{fieldLabel(field.name)}} <span v-if="field.required" class="required">*</span></span>
            <input v-if="field.secret" v-model="password" type="password" autocomplete="new-password" :aria-label="field.name" :disabled="busy">
            <input v-else-if="field.type==='port'||field.type==='integer'" v-model.number="configuration[field.name]" type="number" :min="field.minimum" :max="field.maximum" :aria-label="field.name" :disabled="busy">
            <input v-else v-model="configuration[field.name]" :aria-label="field.name" :disabled="busy">
          </label>
          <div class="exporter-actions configuration-actions"><button class="button button--secondary" :disabled="busy || (selectedItem.schema.fields.some(field=>field.secret&&field.required)&&!password)" @click="configure('check')"><AdminIcon name="link" />{{operation==='check'?'测试中…':'连接测试'}}</button><button v-if="!status" class="button" :disabled="busy || (selectedItem.schema.fields.some(field=>field.secret&&field.required)&&!password)" @click="configure('install')"><AdminIcon name="save" />安装并启动</button><button v-else class="button" :disabled="busy||selectedItem.summary==='upgrade_required'" @click="configure('save')"><AdminIcon name="save" />替换配置</button></div>
        </div>
        <div v-if="status" v-show="showUpdate || selectedItem?.summary==='upgrade_required'" class="exporter-update"><h3>更新安装包</h3><p class="muted">保留服务端安全校验与回滚语义。</p><label class="file-field">新的 .tar.gz 包<input ref="packageInput" type="file" accept=".tar.gz,application/gzip" @change="selectPackage"></label><button class="button" :disabled="busy" @click="run('update')">{{operation==='update'?'更新中…':'确认更新'}}</button></div>
      </div>
    </div>
  </section>
</template>
