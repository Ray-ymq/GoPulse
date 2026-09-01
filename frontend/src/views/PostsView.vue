<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppNav from '../components/AppNav.vue'
import PostCard from '../components/PostCard.vue'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'
import type { Post } from '../types/api'

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
    const page = await postApi.list(cursor)
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

onMounted(() => void load(true))
</script>

<template>
  <div>
    <AppNav />
    <main class="content-shell">
      <section class="page-heading">
        <div>
          <p class="eyebrow">COMMUNITY FEED</p>
          <h1>最新帖子</h1>
          <p class="muted">发现正在发生的讨论，或发布你的第一条动态。</p>
        </div>
        <RouterLink class="button button--primary" to="/posts/new">发布帖子</RouterLink>
      </section>

      <p v-if="errorMessage" class="notice notice--error" role="alert">
        {{ errorMessage }} <button class="inline-action" type="button" @click="load(!loaded)">重试</button>
      </p>
      <p v-if="loading && posts.length === 0" class="state-card">正在加载帖子…</p>
      <p v-else-if="loaded && posts.length === 0" class="state-card">还没有帖子，成为第一个发布者吧。</p>
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
