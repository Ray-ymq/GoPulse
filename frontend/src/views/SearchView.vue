<script setup lang="ts">
import FollowButton from '../components/FollowButton.vue'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PostCard from '../components/PostCard.vue'
import { searchApi, userApi } from '../services/api'
import { ApiError } from '../services/http'
import type { Post, UserProfile } from '../types/api'

const route = useRoute()
const router = useRouter()
const input = ref('')
const activeQuery = ref('')
const posts = ref<Post[]>([])
const users = ref<UserProfile[]>([])
const userTab = computed(() => route.query.tab === 'users')
const resultCount = computed(() => userTab.value ? users.value.length : posts.value.length)
async function selectTab(tab: string) { await router.push({ path: '/search', query: { q: input.value.trim() || undefined, tab } }) }
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)
const errorMessage = ref('')
let requestSequence = 0
let failedRequest: { reset: boolean; cursor?: string } | null = null

function queryFromRoute(): string {
  return typeof route.query.q === 'string' ? route.query.q.trim() : ''
}

function messageFor(error: unknown, paginating: boolean): string {
  if (!(error instanceof ApiError)) return '搜索失败，请稍后重试。'
  if (error.code === 'search_unavailable') return '搜索服务暂时不可用，请稍后重试。'
  if (error.code === 'validation_failed') return paginating ? '搜索结果已更新，请重试以加载最新结果。' : '搜索词或分页参数无效。'
  return error.message
}

async function load(reset: boolean, requestedCursor?: string): Promise<void> {
  const query = activeQuery.value
  if (!query || loading.value) return
  const sequence = ++requestSequence
  const cursor = reset ? undefined : requestedCursor ?? nextCursor.value ?? undefined
  loading.value = true
  errorMessage.value = ''
  failedRequest = null
  try {
    const page = userTab.value ? await userApi.search(query, cursor) : await searchApi.posts(query, cursor)
    if (sequence !== requestSequence) return
    if (userTab.value) users.value = reset ? page.data as UserProfile[] : [...users.value, ...page.data as UserProfile[]]
    else posts.value = reset ? page.data as Post[] : [...posts.value, ...page.data as Post[]]
    nextCursor.value = page.nextCursor
    loaded.value = true
  } catch (error) {
    if (sequence !== requestSequence) return
    const cursorInvalid = cursor !== undefined && error instanceof ApiError && error.code === 'validation_failed'
    if (cursorInvalid) {
      posts.value = []
      users.value = []
      nextCursor.value = null
      loaded.value = false
      failedRequest = { reset: true }
    } else {
      failedRequest = { reset, cursor }
    }
    errorMessage.value = messageFor(error, cursor !== undefined)
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

async function retry(): Promise<void> {
  if (!failedRequest) return
  const request = failedRequest
  await load(request.reset, request.cursor)
}

async function submit(): Promise<void> {
  const query = input.value.trim()
  if (!query || Array.from(query).length > 200) {
    errorMessage.value = '请输入 1–200 个字符的搜索词。'
    return
  }
  // An unchanged URL does not trigger the route watcher. Resubmitting must
  // query again so asynchronously indexed content can become visible.
  if (query === activeQuery.value) {
    await load(true)
    return
  }
  await router.push({ path: '/search', query: { q: query, tab: userTab.value ? 'users' : 'posts' } })
}

watch(
  () => [route.query.q, route.query.tab],
  () => {
    requestSequence += 1
    const query = queryFromRoute()
    input.value = query
    activeQuery.value = query
    posts.value = []
    users.value = []
    nextCursor.value = null
    loaded.value = false
    loading.value = false
    errorMessage.value = ''
    failedRequest = null
    if (query) void load(true)
  },
  { immediate: true },
)
</script>

<template>
  <div>
    <main class="content-shell content-shell--narrow">
      <section class="page-heading search-heading">
        <div>
          <p class="eyebrow">POST SEARCH</p>
          <h1>搜索帖子和用户</h1>
          <p class="muted">搜索标题和正文，结果内容始终从最新的帖子数据装配。</p>
        </div>
      </section>

      <div class="user-tabs" role="tablist" aria-label="搜索类型">
        <button role="tab" :aria-selected="!userTab" @click="selectTab('posts')">帖子</button>
        <button role="tab" :aria-selected="userTab" @click="selectTab('users')">用户</button>
      </div>
      <form class="search-form" role="search" @submit.prevent="submit">
        <label class="sr-only" for="post-search">搜索词</label>
        <input id="post-search" v-model="input" name="q" maxlength="200" :placeholder="userTab ? '输入用户名或显示名称' : '输入标题或正文关键词'" />
        <button class="button button--primary" type="submit" :disabled="loading">搜索</button>
      </form>

      <p v-if="errorMessage" class="notice notice--error" role="alert">
        {{ errorMessage }}
        <button v-if="activeQuery" class="inline-action" type="button" @click="retry">重试</button>
      </p>
      <p v-if="!activeQuery" class="state-card">输入关键词开始搜索。</p>
      <p v-else-if="loading && resultCount === 0" class="state-card">正在搜索…</p>
      <p v-else-if="loaded && resultCount === 0 && !nextCursor" class="state-card">没有找到相关{{ userTab ? '用户' : '帖子' }}。</p>
      <section v-else class="post-list" aria-live="polite">
        <template v-if="userTab"><article v-for="user in users" :key="user.id" class="user-result"><RouterLink :to="`/users/${user.username}`">
          <span class="user-avatar" aria-hidden="true">{{ Array.from(user.display_name)[0]?.toUpperCase() }}</span>
          <div><strong>{{ user.display_name }}</strong><p class="muted">@{{ user.username }}</p><p>{{ user.bio }}</p></div>
        </RouterLink><FollowButton :target="user" /></article></template>
        <template v-else><PostCard v-for="post in posts" :key="post.id" :post="post" /></template>
      </section>

      <div v-if="resultCount > 0 || nextCursor" class="load-more">
        <button v-if="nextCursor" class="button" type="button" :disabled="loading" @click="load(false)">
          {{ loading ? '加载中…' : '加载更多' }}
        </button>
        <p v-else class="muted">已经到底了</p>
      </div>
    </main>
  </div>
</template>
