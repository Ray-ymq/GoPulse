<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import StatusCard from '../components/StatusCard.vue'
import { fetchHealth, fetchReadiness } from '../services/connectivity'
import type { ApiResult, ReadinessResponse, ServiceStatus } from '../types/connectivity'

type ServiceKey = 'backend' | 'mysql' | 'redis' | 'rabbitmq'

const statuses = reactive<Record<ServiceKey, ServiceStatus>>({
  backend: 'loading',
  mysql: 'loading',
  redis: 'loading',
  rabbitmq: 'loading',
})

const isRefreshing = ref(false)
const messages = ref<string[]>([])
const lastUpdated = ref<string | null>(null)
let latestRequestId = 0

const dependencyKeys = ['mysql', 'redis', 'rabbitmq'] as const

function setAllStatuses(status: ServiceStatus): void {
  statuses.backend = status
  setDependencyStatuses(status)
}

function setDependencyStatuses(status: ServiceStatus): void {
  for (const key of dependencyKeys) {
    statuses[key] = status
  }
}

function applyReadiness(result: ApiResult<ReadinessResponse>): void {
  if (result.type === 'success') {
    statuses.mysql = result.data.checks.mysql
    statuses.redis = result.data.checks.redis
    statuses.rabbitmq = result.data.checks.rabbitmq
    return
  }

  setDependencyStatuses('unknown')
  messages.value.push(result.message)
}

function formatUpdateTime(date: Date): string {
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

async function refreshStatuses(): Promise<void> {
  if (isRefreshing.value) {
    return
  }

  const requestId = ++latestRequestId
  isRefreshing.value = true
  messages.value = []
  setAllStatuses('loading')

  try {
    const [healthResult, readinessResult] = await Promise.all([
      fetchHealth(),
      fetchReadiness(),
    ])

    if (requestId !== latestRequestId) {
      return
    }

    if (healthResult.type === 'success') {
      statuses.backend = 'up'
      applyReadiness(readinessResult)
    } else {
      statuses.backend = healthResult.type
      setDependencyStatuses('unknown')
      messages.value.push(healthResult.message)
    }

    lastUpdated.value = formatUpdateTime(new Date())
  } finally {
    if (requestId === latestRequestId) {
      isRefreshing.value = false
    }
  }
}

onMounted(() => {
  void refreshStatuses()
})

onBeforeUnmount(() => {
  latestRequestId += 1
})

const dependencyDescription = computed(() => {
  if (statuses.backend === 'up') {
    return '状态来自 /ready 的最新有效响应'
  }
  if (statuses.backend === 'loading') {
    return '等待 Backend 连通性结果'
  }
  return 'Backend 状态不可靠，暂不采用 /ready 结果'
})

const backendDescription = computed(() => {
  const descriptions: Record<ServiceStatus, string> = {
    loading: '正在请求 /health',
    up: '/health 返回符合契约的 HTTP 200',
    down: 'Backend 报告异常',
    unreachable: '浏览器无法连接 /health',
    invalid: '/health 返回了非预期响应',
    unknown: '尚无法判断 Backend 状态',
  }
  return descriptions[statuses.backend]
})
</script>

<template>
  <main class="page-shell">
    <section class="hero" aria-labelledby="page-title">
      <div>
        <p class="hero__eyebrow">LOCAL ENVIRONMENT</p>
        <h1 id="page-title">GoPulse 连通性</h1>
        <p class="hero__summary">
          通过 Backend 的存活与就绪接口，快速确认本地开发环境是否可以继续工作。
        </p>
      </div>

      <div class="hero__actions">
        <button type="button" class="refresh-button" :disabled="isRefreshing" @click="refreshStatuses">
          <span class="refresh-button__icon" :class="{ 'refresh-button__icon--spinning': isRefreshing }" aria-hidden="true">↻</span>
          {{ isRefreshing ? '正在刷新…' : '刷新状态' }}
        </button>
        <p class="last-updated" aria-live="polite">
          {{ lastUpdated ? `最近更新 ${lastUpdated}` : '等待首次检查' }}
        </p>
      </div>
    </section>

    <section class="status-grid" aria-label="服务连通状态" aria-live="polite">
      <StatusCard
        data-testid="status-backend"
        eyebrow="APPLICATION"
        name="Backend"
        :status="statuses.backend"
        :description="backendDescription"
      />
      <StatusCard
        data-testid="status-mysql"
        eyebrow="DATABASE"
        name="MySQL"
        :status="statuses.mysql"
        :description="dependencyDescription"
      />
      <StatusCard
        data-testid="status-redis"
        eyebrow="CACHE"
        name="Redis"
        :status="statuses.redis"
        :description="dependencyDescription"
      />
      <StatusCard
        data-testid="status-rabbitmq"
        eyebrow="MESSAGE BROKER"
        name="RabbitMQ"
        :status="statuses.rabbitmq"
        :description="dependencyDescription"
      />
    </section>

    <section v-if="messages.length" class="diagnostics" aria-labelledby="diagnostics-title">
      <div>
        <p class="diagnostics__eyebrow">DIAGNOSTICS</p>
        <h2 id="diagnostics-title">本轮检查需要关注</h2>
      </div>
      <ul>
        <li v-for="message in messages" :key="message">{{ message }}</li>
      </ul>
    </section>

    <footer class="page-footer">
      <span>Frontend <strong>:5173</strong></span>
      <span>Backend <strong>:8080</strong></span>
      <span>请求路径 <strong>/health · /ready</strong></span>
    </footer>
  </main>
</template>
