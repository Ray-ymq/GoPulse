<script setup lang="ts">
defineProps<{ state: 'loading' | 'empty' | 'partial' | 'stale' | 'denied' | 'unavailable'; message?: string }>()
const labels = { loading: '正在加载…', empty: '暂无数据。', partial: '部分数据暂不可用。', stale: '当前为上次成功结果，请刷新。', denied: '当前账号无权访问。', unavailable: '服务暂不可用，请重试。' }
</script>
<template><p class="notice gp-state" role="status" aria-live="polite" :data-state="state">{{ message || labels[state] }}<slot /></p></template>
