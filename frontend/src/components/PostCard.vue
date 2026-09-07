<script setup lang="ts">
import type { Post } from '../types/api'
import { formatDate } from '../utils/format'

defineProps<{ post: Post }>()
</script>

<template>
  <article class="post-card">
    <div class="post-card__meta">
      <RouterLink :to="`/users/${post.author.username}`"><span class="user-avatar" aria-hidden="true">{{ Array.from(post.author.display_name || post.author.username)[0]?.toUpperCase() }}</span><strong>{{ post.author.display_name || post.author.username }}</strong> @{{ post.author.username }}</RouterLink>
      <time :datetime="post.created_at">{{ formatDate(post.created_at) }}</time>
    </div>
    <RouterLink class="post-card__title" :to="`/posts/${post.id}`">{{ post.title }}</RouterLink>
    <p class="post-card__excerpt">{{ post.content }}</p>
    <div class="post-card__stats">
      <RouterLink :to="`/posts/${post.id}`">评论 {{ post.comment_count }}</RouterLink>
      <span>点赞 {{ post.like_count }}</span>
      <span v-if="post.liked_by_me" class="liked-label">已点赞</span>
      <button class="bookmark-placeholder" disabled title="Phase-13-03 开放" aria-label="收藏（暂未开放）">收藏</button>
    </div>
  </article>
</template>
