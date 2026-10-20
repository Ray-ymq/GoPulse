<script setup lang="ts">
import { computed } from 'vue'
import { normalizeProductStatus, productStatusLabel, productStatusTone } from './status'

const props = withDefaults(defineProps<{
  status: unknown
  label?: string
  title?: string
}>(), { label: undefined, title: undefined })

const key = computed(() => normalizeProductStatus(props.status))
const tone = computed(() => productStatusTone(key.value))
const text = computed(() => props.label || productStatusLabel(key.value))
</script>

<template>
  <span class="gp-status" :class="`gp-status--${tone}`" :data-status="key" :title="title || undefined">
    {{ text }}
  </span>
</template>
