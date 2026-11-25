<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import type { Profile } from '../types/api'
import { displayName, validText } from '../utils/format'
import { user } from '../composables/session'
import PostFeed from '../components/PostFeed.vue'
import InkAvatar from '../components/InkAvatar.vue'
import FollowButton from '../components/FollowButton.vue'
const route = useRoute(), q = computed(() => typeof route.query.q === 'string' ? route.query.q.trim() : ''), usersTab = computed(() => route.query.tab === 'users')
const valid = computed(() => validText(q.value, 200)), profiles = ref<Profile[]>([]), cursor = ref<string | null>(null), busy = ref(false), error = ref('')
let controller: AbortController | undefined, generation = 0
async function load(reset = false) {
  if (!valid.value || (!reset && busy.value)) return
  if (reset) { controller?.abort(); profiles.value = []; cursor.value = null }
  const n = ++generation; controller = new AbortController(); const signal = controller.signal; busy.value = true; error.value = ''
  try { const result = await api.users(q.value, reset ? undefined : cursor.value, signal); if (n !== generation) return; profiles.value = [...new Map([...profiles.value, ...result.data].map(p => [p.id, p])).values()]; cursor.value = result.meta.next_cursor }
  catch (e) { if (!signal.aborted && n === generation) error.value = errorMessage(e) }
  finally { if (n === generation) busy.value = false }
}
watch([q, usersTab], () => { if (usersTab.value) void load(true) }, { immediate: true })
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>
<template><section class="page-banner"><h1>寻找你的同频</h1><p v-if="valid">与「{{ q }}」有关的人与分享。</p><p v-else>请输入 1–200 个字符的搜索词。</p></section><nav v-if="valid" class="search-tabs" aria-label="搜索类型"><RouterLink class="chip" :class="{ active: !usersTab }" :to="{ path: '/search', query: { q } }">帖子</RouterLink><RouterLink class="chip" :class="{ active: usersTab }" :to="{ path: '/search', query: { q, tab: 'users' } }">用户</RouterLink></nav><template v-if="valid"><PostFeed v-if="!usersTab" endpoint="/search/posts" :query="q" empty-title="暂时没有相关分享" empty-text="试试其他关键词，或搜索感兴趣的作者。" /><template v-else><div class="person-list"><div v-for="p in profiles" :key="p.id" class="person"><InkAvatar :name="displayName(p)" /><RouterLink class="person-info author-link" :to="`/users/${encodeURIComponent(p.username)}`"><strong>{{ displayName(p) }}</strong><small>{{ p.bio || '@' + p.username }}</small></RouterLink><FollowButton v-if="p.id !== user?.id" :id="p.id" :name="displayName(p)" :following="p.following" @change="p.following = $event" /></div></div><p v-if="busy" class="state-block" role="status">正在寻找同频的人……</p><div v-if="error" class="state-block" role="alert"><p>{{ error }}</p><button class="secondary" :disabled="busy" @click="load(!profiles.length)">重试</button></div><div v-else-if="!busy && !profiles.length" class="empty"><h3>暂时没有找到相关用户</h3><p>试试其他用户名或关键词。</p></div><button v-if="cursor && !error" class="secondary more-posts" :disabled="busy" @click="load()">继续查看</button></template></template></template>
