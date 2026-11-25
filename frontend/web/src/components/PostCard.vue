<script setup lang="ts">
import type { Post } from '../types/api'
import { displayName, hashtags, timeLabel } from '../utils/format'
import InkAvatar from './InkAvatar.vue'
import PostActions from './PostActions.vue'
defineProps<{ post: Post }>()
const emit = defineEmits<{ change: [post: Post] }>()
</script>
<template><article class="post"><div class="post-head"><RouterLink class="avatar-link" :to="`/users/${encodeURIComponent(post.author.username)}`" :aria-label="`查看${displayName(post.author)}的主页`"><InkAvatar :name="displayName(post.author)" /></RouterLink><RouterLink class="author-link" :to="`/users/${encodeURIComponent(post.author.username)}`"><strong>{{ displayName(post.author) }}</strong><small>{{ timeLabel(post.created_at) }} · 分享了新发现<span v-if="post.edited_at"> · 已编辑</span></small></RouterLink></div><RouterLink class="post-title" :to="`/posts/${post.id}`">{{ post.title }}</RouterLink><p class="excerpt">{{ Array.from(post.content).length > 150 ? Array.from(post.content).slice(0, 150).join('') + '…' : post.content }}</p><div v-if="hashtags(post.content).length" class="tags"><RouterLink v-for="tag in hashtags(post.content)" :key="tag" class="tag" :to="{ path: '/search', query: { q: tag.slice(1) } }">{{ tag }}</RouterLink></div><PostActions :post="post" @change="emit('change', $event)" /></article></template>
