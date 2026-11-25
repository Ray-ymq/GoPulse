<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import { contentVersion } from '../composables/community'
import { user } from '../composables/session'
import type { Post } from '../types/api'
import PostCard from './PostCard.vue'
const props = withDefaults(defineProps<{ endpoint: string; query?: string; emptyTitle?: string; emptyText?: string; bookmarks?: boolean }>(), { query: '', emptyTitle: '这里还很安静', emptyText: '暂时没有分享，稍后再来看看。' })
const items = ref<Post[]>([]), cursor = ref<string | null>(null), loading = ref(false), error = ref(''), ready = ref(false)
let controller: AbortController | undefined, generation = 0
async function load(reset = false) {
  if (loading.value && !reset) return
  if (reset) { controller?.abort(); items.value = []; cursor.value = null; ready.value = false }
  const current = ++generation; controller = new AbortController(); const signal = controller.signal
  loading.value = true; error.value = ''
  try { const page = await api.posts(props.endpoint, reset ? undefined : cursor.value, props.query ? { q: props.query } : undefined, signal); if (current !== generation) return; items.value = [...new Map([...items.value, ...page.data].map(p => [p.id, p])).values()]; cursor.value = page.meta.next_cursor; ready.value = true }
  catch (e) { if (!signal.aborted && current === generation) error.value = errorMessage(e) }
  finally { if (current === generation) loading.value = false }
}
function changed(post: Post) { items.value = items.value.map(p => p.id === post.id ? post : p).filter(p => !props.bookmarks || p.bookmarked_by_me) }
watch([() => props.endpoint, () => props.query, () => user.value?.id, contentVersion], () => { void load(true) }, { immediate: true })
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>
<template><div v-if="loading && !items.length" class="state-block" role="status" aria-live="polite">正在展开新的分享……</div><div class="feed" :aria-busy="loading"><PostCard v-for="post in items" :key="post.id" :post="post" @change="changed" /></div><div v-if="error" class="state-block load-error" role="alert"><p>{{ error }}</p><button class="secondary" :disabled="loading" @click="load(!ready)">重新加载</button></div><div v-else-if="ready && !items.length" class="empty"><h3>{{ emptyTitle }}</h3><p>{{ emptyText }}</p><RouterLink class="link-button" to="/">去发现内容</RouterLink></div><button v-if="cursor && !error" class="secondary more-posts" :disabled="loading" @click="load()">{{ loading ? '正在加载…' : '继续阅读' }}</button><div v-else-if="ready && items.length && !error" class="end">已读完当前分享</div></template>
