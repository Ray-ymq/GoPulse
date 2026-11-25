<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import { user } from '../composables/session'
import { updatePost } from '../composables/community'
import type { Post } from '../types/api'
import Icon from './Icon.vue'
const props = defineProps<{ post: Post }>()
const emit = defineEmits<{ change: [post: Post] }>()
const busy = ref(''), error = ref(''), shared = ref(false)
async function toggle(kind: 'like' | 'bookmark') {
  if (busy.value) return; busy.value = kind; error.value = ''
  const owner = user.value?.id
  const active = kind === 'like' ? !props.post.liked_by_me : !props.post.bookmarked_by_me
  try {
    await api[kind](props.post.id, active)
    const p = { ...props.post, ...(kind === 'like' ? { liked_by_me: active, like_count: Math.max(0, props.post.like_count + (active ? 1 : -1)) } : { bookmarked_by_me: active }) }
    if (user.value?.id === owner) { updatePost(p); emit('change', p) }
  } catch (e) { error.value = errorMessage(e) } finally { busy.value = '' }
}
async function share() { error.value = ''; const url = new URL(`/posts/${props.post.id}`, location.origin); try { await navigator.clipboard.writeText(url.href); shared.value = true } catch { error.value = `请复制此链接：${url.href}` } }
</script>
<template><div class="post-actions"><button class="action" :class="{ active: post.liked_by_me }" :disabled="!!busy" :aria-pressed="post.liked_by_me" :aria-label="post.liked_by_me ? '取消点赞' : '点赞'" @click="toggle('like')"><Icon name="heart" /><span>{{ post.like_count }}</span></button><RouterLink class="action" :to="`/posts/${post.id}#comments`" aria-label="查看评论"><Icon name="comment" /><span>{{ post.comment_count || '评论' }}</span></RouterLink><button class="action" :class="{ active: post.bookmarked_by_me }" :disabled="!!busy" :aria-pressed="post.bookmarked_by_me" @click="toggle('bookmark')"><Icon name="bookmark" /><span>{{ post.bookmarked_by_me ? '已收藏' : '收藏' }}</span></button><button class="action" aria-label="复制帖子链接" @click="share"><Icon name="share" /></button></div><p v-if="error" class="inline-error" role="alert">{{ error }}</p><span v-if="shared" class="hint" role="status">链接已复制。</span></template>
