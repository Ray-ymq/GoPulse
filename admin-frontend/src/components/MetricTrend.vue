<script setup lang="ts">
import { computed } from 'vue'
import type { MetricPoint } from '../types/observability'
const props = defineProps<{ points: MetricPoint[]; unit: string }>()
const stats = computed(() => {
  const values = props.points.map(point => point.value)
  return { min: Math.min(...values), max: Math.max(...values) }
})
const coordinates = computed(() => {
  const start = Date.parse(props.points[0]?.timestamp ?? '')
  const end = Date.parse(props.points.at(-1)?.timestamp ?? '')
  const span = stats.value.max - stats.value.min
  return props.points.map(point => `${end === start ? 500 : 10 + (Date.parse(point.timestamp) - start) / (end - start) * 980},${span === 0 ? 120 : 220 - (point.value - stats.value.min) / span * 200}`).join(' ')
})
</script>
<template>
  <figure v-if="points.length" class="metric-figure">
    <svg class="metric-chart" viewBox="0 0 1000 240" preserveAspectRatio="none" role="img" :aria-label="`指标趋势：${points.length} 个采样点，最小 ${stats.min}，最大 ${stats.max} ${unit}`">
      <polygon v-if="points.length > 1" :points="`10,240 ${coordinates} 990,240`" fill="#edf3ff" />
      <polyline v-if="points.length > 1" :points="coordinates" fill="none" stroke="#2864f5" stroke-width="2" vector-effect="non-scaling-stroke" />
      <circle v-else cx="500" cy="120" r="4" fill="#2864f5" />
    </svg>
    <figcaption class="chart-caption"><span>MIN {{ stats.min }}</span><span>MAX {{ stats.max }}</span><span>{{ points.length }} 个采样点 · {{ unit }}</span><span>纵轴按当前时序范围缩放</span></figcaption>
  </figure>
  <p v-else role="status">暂无趋势采样</p>
</template>
