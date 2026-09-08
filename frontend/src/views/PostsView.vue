<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import PostCard from '../components/PostCard.vue'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'
import type { Post } from '../types/api'

const route = useRoute()
const following = ref(false)
const posts = ref<Post[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)
const errorMessage = ref('')
let requestedCursor: string | null | undefined

async function load(reset = false): Promise<void> {
  const cursor = reset ? undefined : nextCursor.value ?? undefined
  if (loading.value || (!reset && loaded.value && nextCursor.value === null)) return
  if (!reset && cursor === requestedCursor) return
  loading.value = true
  requestedCursor = cursor
  errorMessage.value = ''
  try {
    const page = await (following.value ? postApi.following(cursor) : postApi.list(cursor))
    posts.value = reset ? page.data : [...posts.value, ...page.data]
    nextCursor.value = page.nextCursor
    loaded.value = true
  } catch (error) {
    requestedCursor = undefined
    errorMessage.value = error instanceof ApiError ? error.message : '帖子加载失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}

function selectTab(value: boolean) {
 if (loading.value || following.value === value) return
 following.value = value; posts.value = []; loaded.value = false; nextCursor.value = null; requestedCursor = undefined
 void load(true)
}
onMounted(() => void load(true))
</script>

<template>
  <div>
    <main class="content-shell">
      <header class="page-heading"><h1>首页</h1></header>
      <p v-if="route.query.deleted === '1'" role="status" class="notice">帖子已永久删除，无法恢复。</p>
      <div class="user-tabs" role="tablist" aria-label="时间线">
        <button role="tab" :aria-selected="!following" :disabled="loading" @click="selectTab(false)">全部</button>
        <button role="tab" :aria-selected="following" :disabled="loading" @click="selectTab(true)">Following</button>
      </div>

      <p v-if="errorMessage" class="notice notice--error" role="alert">
        {{ errorMessage }} <button class="inline-action" type="button" @click="load(!loaded)">重试</button>
      </p>
      <p v-if="loading && posts.length === 0" class="state-card">正在加载帖子…</p>
      <p v-else-if="loaded && posts.length === 0" class="state-card">{{ following ? '关注的人还没有帖子，去搜索用户并关注吧。' : '还没有帖子，成为第一个发布者吧。' }}</p>
      <section v-else class="post-list" aria-live="polite">
        <PostCard v-for="post in posts" :key="post.id" :post="post" />
      </section>

      <div v-if="posts.length > 0" class="load-more">
        <button v-if="nextCursor" class="button" type="button" :disabled="loading" @click="load(false)">
          {{ loading ? '加载中…' : '加载更多' }}
        </button>
        <p v-else class="muted">已经到底了</p>
      </div>
    </main>
  </div>
</template>
