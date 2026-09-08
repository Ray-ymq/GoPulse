<script setup lang="ts">
import { useAuth } from '../composables/useAuth'
import type { Post } from '../types/api'
import { formatDate } from '../utils/format'
defineProps<{ post: Post }>()
const { user } = useAuth()
</script>
<template>
  <time v-if="post.edited_at" :datetime="post.edited_at">已编辑 {{ formatDate(post.edited_at) }}</time>
  <details v-if="user?.id === post.author.id" class="post-edit-menu">
    <summary aria-label="帖子操作">更多</summary>
    <RouterLink :to="`/posts/${post.id}/edit`">编辑</RouterLink>
  </details>
</template>
