<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  name: string
  size?: number
  label?: string
  strokeWidth?: number
}>(), { size: 20, strokeWidth: 1.75 })

const paths: Record<string, string> = {
  pulse: 'M2 12h4l3-9 4 18 3-9h6',
  home: 'm3 10 9-7 9 7M5 9v12h5v-7h4v7h5V9',
  search: 'M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14m5 12 6 6',
  bell: 'M5 17h14l-2-3V9a5 5 0 0 0-10 0v5l-2 3m5 4h4',
  bookmark: 'M6 3h12v18l-6-4-6 4V3Z',
  profile: 'M12 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8M4 22v-4a8 8 0 0 1 16 0v4',
  user: 'M12 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8M4 22v-4a8 8 0 0 1 16 0v4',
  users: 'M16 21v-3a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v3m14-7a4 4 0 0 1 6 4v3M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8m8 0a4 4 0 0 1 0 8',
  plus: 'M12 5v14M5 12h14',
  menu: 'M4 6h16M4 12h16M4 18h16',
  close: 'm6 6 12 12M6 18 18 6',
  refresh: 'M20 7a9 9 0 0 0-15-2L2 8m0-6v6h6m-4 9a9 9 0 0 0 15 2l3-3m0 6v-6h-6',
  clock: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20m0 4v6l4 2',
  arrow: 'M4 12h16m-6-6 6 6-6 6',
  chevron: 'm9 5 7 7-7 7',
  check: 'm5 12 4 4L19 6',
  warning: 'm12 3 10 18H2L12 3Zm0 6v5m0 3v1',
  info: 'M12 11v5m0-9v.5M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20',
  heart: 'M20.8 8.6c0 5.4-8.8 10.4-8.8 10.4S3.2 14 3.2 8.6A4.6 4.6 0 0 1 12 6a4.6 4.6 0 0 1 8.8 2.6Z',
  comment: 'M20 11.5a7.5 7.5 0 0 1-7.5 7.5H8l-4 3v-5.2a7.5 7.5 0 1 1 16-5.3Z',
  share: 'M15 8l5-5m0 0v4m0-4h-4M20 13v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h5',
  play: 'm8 4 12 8-12 8V4Z',
  stop: 'M5 5h14v14H5z',
  save: 'M4 3h13l4 4v14H3V3h1m3 0v7h10V3M7 21v-7h10v7',
  link: 'm9 15 6-6m-7 3-2 2a4 4 0 0 0 6 6l2-2m-4-12 2-2a4 4 0 0 1 6 6l-2 2',
  database: 'M3 6c0-5 18-5 18 0s-18 5-18 0Zm0 0v12c0 5 18 5 18 0V6M3 12c0 5 18 5 18 0',
  box: 'm3 7 9-5 9 5v10l-9 5-9-5V7Zm0 0 9 5 9-5M12 12v10M7 4l10 6',
  metrics: 'M4 21V11h4v10m3 0V3h4v18m3 0v-7h4v7',
  logs: 'M6 3h9l4 4v14H5V3h1m9 0v5h4M8 12h8M8 16h8',
  events: 'M5 3h14v18H5zM8 7h8M8 11h5M8 15h8',
  alerts: 'M5 16h14l-2-3V9a5 5 0 0 0-10 0v4l-2 3m5 4h4M12 2v2',
  audit: 'M8 4H4v17h16V4h-4M8 2h8v5H8zM8 11h8M8 15h6',
  redis: 'm2 7 10-5 10 5-10 5-10-5Zm0 5 10 5 10-5M2 17l10 5 10-5',
  kafka: 'M12 8v8M8 5h-2v14h2m8-14h2v14h-2M9 2h6v6H9zM9 16h6v6H9z',
  elasticsearch: 'M3 8h18M3 16h18M12 2a10 10 0 1 0 0 20 10 10 0 0 0-20',
  victoriametrics: 'm2 5 10 8L22 5M2 11l10 8 10-8M7 18l5 4 5-4',
  rabbitmq: 'M3 3h4v8h4V3h4v8h6v10H3V3Zm12 12v3h3v-3h-3Z',
}

const path = computed(() => paths[props.name] ?? paths.box)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    :aria-hidden="label ? undefined : 'true'"
    :aria-label="label"
    :role="label ? 'img' : undefined"
    focusable="false"
    class="product-icon"
  >
    <title v-if="label">{{ label }}</title>
    <path :d="path" />
  </svg>
</template>
