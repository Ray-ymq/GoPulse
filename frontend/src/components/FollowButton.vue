<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAuth } from '../composables/useAuth'
import { useFollowing } from '../composables/useFollowing'
const props = defineProps<{ target: { id: number; username: string; following?: boolean } }>()
const auth = useAuth()
const follow = useFollowing()
const error = ref('')
const viewer = computed(() => auth.user.value?.id ?? 0)
const value = computed(() => follow.value(viewer.value, props.target.id, props.target.following))
async function toggle() {
  error.value = ''
  try { await follow.set(viewer.value, props.target.id, value.value) }
  catch { error.value = '关注操作失败，已恢复原状态，请重试。' }
}
</script>
<template>
  <span v-if="viewer && viewer !== target.id" class="follow-control">
    <button class="button button--small" type="button" :aria-label="`${value ? '取消关注' : '关注'} @${target.username}`" :aria-pressed="value" :disabled="follow.pending(viewer, target.id)" @click.stop.prevent="toggle">{{ value ? '取消关注' : '关注' }}</button>
    <span v-if="error" role="alert">{{ error }}</span>
  </span>
</template>
