<script setup lang="ts">
import AdminIcon from './AdminIcon.vue'
withDefaults(defineProps<{ label: string; value: string | number; note?: string; icon: string; tone?: string }>(), { tone: 'blue', note: '' })
</script>
<template><div class="stat-card"><span class="stat-icon" :class="`tone-${tone}`"><AdminIcon :name="icon" :size="28" /></span><div class="stat-copy"><span class="stat-label">{{ label }}</span><strong><slot>{{ value }}</slot></strong><span class="stat-note">{{ note }}</span></div></div></template>
