<script setup lang="ts">
import { ref } from 'vue'
import { postApi } from '../services/api'
import { useAuth } from '../composables/useAuth'
import type { Post } from '../types/api'
import { formatDate } from '../utils/format'
const props = defineProps<{ post: Post }>()
const dialog = ref<HTMLDialogElement>()
const pending = ref(false)
const error = ref('')
async function remove() {
 if (pending.value) return
 pending.value = true
 error.value = ''
 try {
  await postApi.delete(props.post.id)
  // Reload also drops all local timeline/bookmark/detail snapshots.
  window.location.assign('/?deleted=1')
 } catch {
  error.value = '删除失败，内容未移除。请重试；若已删除，刷新页面确认。'
  pending.value = false
 }
}
const { user } = useAuth()
</script>
<template>
  <time v-if="post.edited_at" :datetime="post.edited_at">已编辑 {{ formatDate(post.edited_at) }}</time>
  <details v-if="user?.id === post.author.id" class="post-edit-menu">
    <summary aria-label="帖子操作">更多</summary>
    <RouterLink :to="`/posts/${post.id}/edit`">编辑</RouterLink>
    <button type="button" @click="dialog?.showModal()">删除</button>
    <dialog ref="dialog" :aria-labelledby="`delete-title-${post.id}`" @cancel="pending && $event.preventDefault()">
      <h2 :id="`delete-title-${post.id}`">永久删除帖子</h2>
      <p>{{ post.title }}</p>
      <p>此操作永久且不可恢复，将清理评论、点赞和收藏。</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <button autofocus type="button" :disabled="pending" @click="dialog?.close()">取消</button>
      <button class="delete-danger" type="button" :disabled="pending" @click="remove">{{ pending ? '删除中…' : '永久删除' }}</button>
    </dialog>
  </details>
</template>

<style scoped>
dialog { max-width: min(90vw, 32rem); border: 1px solid #777; border-radius: 12px; padding: 24px; }
dialog::backdrop { background: #0008; }
.delete-danger { color: #b91c1c; margin-left: 12px; }
</style>
