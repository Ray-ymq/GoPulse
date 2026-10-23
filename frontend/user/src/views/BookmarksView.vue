<script setup lang="ts">
import { onMounted, ref } from 'vue'
import PostCard from '../components/PostCard.vue'
import { postApi } from '../services/api'
import type { Post } from '../types/api'
import ProductState from '../../../shared/ProductState.vue'
const posts = ref<Post[]>([])
const cursor = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
async function load() {
  if (loading.value || (loaded.value && !cursor.value)) return
  loading.value = true; error.value = ''
  try {
    const page = await postApi.bookmarks(cursor.value ?? undefined)
    const ids = new Set(posts.value.map(p => p.id))
    posts.value.push(...page.data.filter(p => !ids.has(p.id)))
    cursor.value = page.nextCursor; loaded.value = true
  } catch { error.value = '收藏加载失败，请重试。' }
  finally { loading.value = false }
}
onMounted(() => void load())
</script>
<template>
  <main class="content-shell">
    <header class="page-heading"><h1>我的收藏</h1></header>
    <p class="muted">仅你可见。已删除的帖子会自动跳过；取消收藏后，下次打开列表不再显示。</p>
    <ProductState v-if="error && !posts.length" state="unavailable" :message="error" action-label="重试" @retry="load" />
    <ProductState v-else-if="loading && !loaded" state="loading" message="正在读取收藏…" />
    <ProductState v-else-if="loaded && !posts.length" state="empty" message="还没有收藏的帖子。" />
    <section v-if="posts.length" class="post-list" aria-live="polite"><PostCard v-for="post in posts" :key="post.id" :post="post" /></section>
    <button v-if="cursor" class="button" type="button" :disabled="loading" @click="load">加载更多</button>
    <p v-else-if="loaded && posts.length" class="muted">已经到底了</p>
  </main>
</template>
