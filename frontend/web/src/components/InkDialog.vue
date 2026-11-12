<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
const props = defineProps<{ open: boolean; title: string; labelId: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement>()
function sync() { if (!dialog.value) return; if (props.open && !dialog.value.open) dialog.value.showModal(); else if (!props.open && dialog.value.open) dialog.value.close() }
onMounted(sync); watch(() => props.open, sync)
function backdrop(event: MouseEvent) { if (event.target !== dialog.value) return; const r = dialog.value.getBoundingClientRect(); if (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom) emit('close') }
</script>
<template><dialog ref="dialog" class="dialog" :aria-labelledby="labelId" @close="emit('close')" @click="backdrop"><div class="dialog-head"><h2 :id="labelId">{{ title }}</h2><button class="icon-button" aria-label="关闭窗口" @click="emit('close')"><Icon name="close" /></button></div><div class="dialog-body"><slot /></div></dialog></template>
