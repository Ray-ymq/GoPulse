<script setup lang="ts">
import type { Post } from '../types/api'
import { formatDate } from '../utils/format'

defineProps<{ post: Post }>()
</script>

<template>
  <article class="post-card">
    <div class="post-card__meta">
      <span>@{{ post.author.username }}</span>
      <time :datetime="post.created_at">{{ formatDate(post.created_at) }}</time>
    </div>
    <RouterLink class="post-card__title" :to="`/posts/${post.id}`">{{ post.title }}</RouterLink>
    <p class="post-card__excerpt">{{ post.content }}</p>
    <div class="post-card__stats">
      <span>评论 {{ post.comment_count }}</span>
      <span>点赞 {{ post.like_count }}</span>
      <span v-if="post.liked_by_me" class="liked-label">已点赞</span>
    </div>
  </article>
</template>
