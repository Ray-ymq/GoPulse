<script setup lang="ts">
import { computed } from 'vue'
import type { ServiceStatus } from '../types/connectivity'

const props = defineProps<{
  name: string
  eyebrow: string
  status: ServiceStatus
  description: string
}>()

const statusLabels: Record<ServiceStatus, string> = {
  loading: '检查中',
  up: '正常',
  down: '异常',
  unreachable: '不可达',
  invalid: '响应无效',
  unknown: '未知',
}

const statusLabel = computed(() => statusLabels[props.status])
</script>

<template>
  <article class="status-card" :class="`status-card--${status}`" :data-status="status">
    <div class="status-card__header">
      <div>
        <p class="status-card__eyebrow">{{ eyebrow }}</p>
        <h2>{{ name }}</h2>
      </div>
      <span class="status-pill" :class="`status-pill--${status}`">
        <span class="status-pill__dot" aria-hidden="true"></span>
        {{ statusLabel }}
      </span>
    </div>
    <p class="status-card__description">{{ description }}</p>
  </article>
</template>
