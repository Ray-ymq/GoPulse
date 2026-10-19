<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getSelfMonitor, setSelfMonitor, type SelfMonitorStatus } from '../services/selfMonitor'
const status = ref<SelfMonitorStatus | null>(null)
const pending = ref(false)
const error = ref(false)
const labels = { stopped: '已停止', starting: '启动中', running: '运行中', stopping: '停止中', degraded: '部分不可达', failed: '启动或停止失败', unavailable: '控制服务不可用' }
const transitioning = computed(() => status.value?.state === 'starting' || status.value?.state === 'stopping')
let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
let disposed = false
async function refresh(action?: 'start' | 'stop') {
  if (pending.value || disposed) return
  if (timer !== undefined) clearTimeout(timer)
  controller = new AbortController()
  pending.value = true
  error.value = false
  try {
    const next = await (action ? setSelfMonitor(action, controller.signal) : getSelfMonitor(controller.signal))
    if (!disposed) status.value = next
  } catch {
    if (!disposed) { error.value = true; status.value = null }
  } finally {
    pending.value = false
    if (!disposed) timer = setTimeout(() => { void refresh() }, 5000)
  }
}
onMounted(() => { void refresh() })
onBeforeUnmount(() => { disposed = true; controller?.abort(); if (timer !== undefined) clearTimeout(timer) })
</script>
<template>
  <article class="panel dashboard-section" aria-label="独立自监控" :aria-busy="pending">
    <header class="panel-heading"><h3>独立自监控</h3></header>
    <p role="status">{{ error ? '状态读取失败，请重试' : status ? labels[status.state] : '正在读取状态…' }}</p>
    <div class="title-actions">
      <button type="button" :disabled="pending || transitioning || !status || !['stopped', 'failed', 'unavailable'].includes(status.state)" @click="refresh('start')">启动自监控</button>
      <button type="button" :disabled="pending || transitioning || !status || !['running', 'degraded', 'failed'].includes(status.state)" @click="refresh('stop')">停止自监控</button>
      <a v-if="status && ['running', 'degraded'].includes(status.state)" href="/grafana/" target="_blank" rel="noopener noreferrer">打开 Grafana</a>
      <button type="button" :disabled="pending" @click="refresh()">重试 / 刷新状态</button>
    </div>
    <p>独立采集与持久化存储；关闭页面后继续运行。Grafana 使用独立账号登录。</p>
    <p v-if="status?.state === 'degraded'">存在未采集到的服务或数据源，请进入 Grafana 查看。缺失数据不代表零。</p>
  </article>
</template>
