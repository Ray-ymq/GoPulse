<script setup lang="ts">
import { formatDate } from './format'
defineProps<{ value: string }>()
</script>
<template><time :datetime="value" :title="value">{{ formatDate(value) }}</time></template>
