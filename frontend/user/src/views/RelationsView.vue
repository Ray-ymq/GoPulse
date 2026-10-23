<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import FollowButton from '../components/FollowButton.vue'
import { userApi } from '../services/api'
import type { UserProfile } from '../types/api'
import ProductState from '../../../shared/ProductState.vue'
const route = useRoute()
const records = ref<UserProfile[]>([])
const cursor = ref<string | null>(null)
const loading = ref(false)
const error = ref('')
let generation = 0
async function load(reset = false) {
  const current = generation
  if (loading.value) return
  loading.value = true; error.value = ''
  try {
    const page = await userApi.relations(route.path.endsWith('/followers') ? 'followers' : 'following', reset ? undefined : cursor.value ?? undefined)
    if (current !== generation) return
    records.value = reset ? page.data : [...records.value, ...page.data]; cursor.value = page.nextCursor
  } catch { if (current === generation) error.value = '关系列表加载失败，请重试。' }
  finally { if (current === generation) loading.value = false }
}
watch(() => route.path, () => { generation++; records.value = []; cursor.value = null; loading.value = false; void load(true) }, { immediate: true })
</script>
<template>
  <main class="content-shell">
    <h1>{{ route.path.endsWith('/followers') ? '我的粉丝' : '我的关注' }}</h1>
    <nav class="user-tabs" aria-label="我的关系"><RouterLink to="/me/following">关注</RouterLink><RouterLink to="/me/followers">粉丝</RouterLink></nav>
    <ProductState v-if="error" state="unavailable" :message="error" action-label="重试" @retry="load(records.length === 0)" />
    <ProductState v-else-if="loading" state="loading" message="正在读取关系…" />
    <ProductState v-else-if="!records.length" state="empty" message="暂无关系。" />
    <article v-for="user in records" :key="user.id" class="user-result">
      <RouterLink :to="`/users/${user.username}`"><strong>{{ user.display_name }}</strong> @{{ user.username }}<p>{{ user.bio }}</p></RouterLink>
      <FollowButton :target="user" />
    </article>
    <button v-if="cursor" class="button" type="button" :disabled="loading" @click="load()">加载更多</button>
  </main>
</template>
