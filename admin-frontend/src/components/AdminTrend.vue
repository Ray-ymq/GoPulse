<script setup lang="ts">
import { computed } from 'vue'
import type { MetricSeries } from '../types/observability'
const props = defineProps<{ series: MetricSeries[]; unit: string }>()
const colors = ['#1670ff', '#00a66a', '#f59b23', '#8b5cf6']
const points = computed(() => props.series.flatMap(row => row.points))
const bounds = computed(() => {
  const values = points.value.map(point => point.value)
  const times = points.value.map(point => Date.parse(point.timestamp))
  let min = values.reduce((a,b) => Math.min(a,b), 0)
  let max = values.reduce((a,b) => Math.max(a,b), 0)
  if (max === min) max = min + 1
  return { min, max, start: Math.min(...times), end: Math.max(...times) }
})
function x(timestamp: string) { const b = bounds.value; return b.start === b.end ? 530 : 58 + (Date.parse(timestamp)-b.start)/(b.end-b.start)*910 }
function y(value: number) { const b = bounds.value; return 205 - (value-b.min)/(b.max-b.min)*185 }
function line(series: MetricSeries) { return series.points.map(point => `${x(point.timestamp)},${y(point.value)}`).join(' ') }
function number(value: number) { return Intl.NumberFormat(undefined, { maximumFractionDigits: 2, notation:'compact' }).format(value) }
function time(value: number) { return new Date(value).toLocaleTimeString('zh-CN', { hour:'2-digit', minute:'2-digit', second: bounds.value.end-bounds.value.start<300000?'2-digit':undefined, hour12:false }) }
</script>
<template>
  <figure class="trend-figure">
    <div v-if="points.length" class="trend-legend"><span v-for="(row,index) in series" :key="index"><i :style="{background:colors[index%colors.length]}" />{{Object.values(row.labels).join(' / ') || '默认序列'}} <b>{{row.points.at(-1)?.value ?? '未知'}}</b></span><span>{{unit}}</span></div>
    <svg v-if="points.length" class="metric-chart" viewBox="0 0 1000 240" role="img" :aria-label="`真实指标趋势，${points.length} 个采样点，单位 ${unit}`">
      <g v-for="tick in [0,1,2,3,4]" :key="tick"><line x1="58" x2="968" :y1="20+tick*46.25" :y2="20+tick*46.25" stroke="#e9eff7"/><text x="45" :y="24+tick*46.25" text-anchor="end">{{number(bounds.max-(bounds.max-bounds.min)*tick/4)}}</text><line :x1="58+tick*227.5" :x2="58+tick*227.5" y1="20" y2="205" stroke="#f0f4f9"/><text :x="58+tick*227.5" y="229" text-anchor="middle">{{time(bounds.start+(bounds.end-bounds.start)*tick/4)}}</text></g>
      <g v-for="(row,index) in series" :key="index"><polygon v-if="row.points.length>1" :points="`${x(row.points[0]!.timestamp)},205 ${line(row)} ${x(row.points.at(-1)!.timestamp)},205`" :fill="colors[index%colors.length]" fill-opacity=".06"/><polyline v-if="row.points.length>1" :points="line(row)" fill="none" :stroke="colors[index%colors.length]" stroke-width="2" vector-effect="non-scaling-stroke"/><circle v-for="point in row.points.length===1?row.points:[]" :key="point.timestamp" :cx="x(point.timestamp)" :cy="y(point.value)" r="3" :fill="colors[index%colors.length]"/></g>
    </svg>
    <div v-else class="chart-empty" role="status">暂无可用趋势采样</div>
  </figure>
</template>
