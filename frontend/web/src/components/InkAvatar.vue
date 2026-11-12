<script setup lang="ts">
import { initial } from '../utils/format'
defineProps<{ name: string }>()
</script>
<template><span class="avatar" aria-hidden="true">{{ initial(name) }}</span></template>
