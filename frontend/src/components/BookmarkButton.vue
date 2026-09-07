<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAuth } from '../composables/useAuth'
import { useBookmarks } from '../composables/useBookmarks'
import type { Post } from '../types/api'
const props = defineProps<{ post: Post }>()
const auth = useAuth()
const bookmarks = useBookmarks()
const error = ref('')
const viewer = computed(() => auth.user.value?.id ?? 0)
const value = computed(() => bookmarks.value(viewer.value, props.post.id, props.post.bookmarked_by_me))
async function toggle() {
  error.value = ''
  try { await bookmarks.toggle(viewer.value, props.post.id, value.value) }
  catch { error.value = '收藏操作失败，已恢复原状态，请重试。' }
}
</script>
<template>
  <span class="bookmark-control">
    <button type="button" class="button button--small" aria-label="收藏帖子" :aria-pressed="value" :disabled="!viewer || bookmarks.pending(viewer, post.id)" @click.stop.prevent="toggle">{{ value ? '取消收藏' : '收藏' }}</button>
    <span v-if="error" role="alert">{{ error }}</span>
  </span>
</template>
