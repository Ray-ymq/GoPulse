<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import type { Profile } from '../types/api'
import { displayName } from '../utils/format'
import InkAvatar from '../components/InkAvatar.vue'
import FollowButton from '../components/FollowButton.vue'
const route = useRoute(), kind = computed(() => route.params.kind === 'followers' ? 'followers' : 'following'), items = ref<Profile[]>([]), cursor = ref<string | null>(null), busy = ref(false), ready = ref(false), error = ref('')
async function load(reset = false) { if (busy.value) return; busy.value = true; error.value = ''; try { const r = await api.relations(kind.value, reset ? undefined : cursor.value); items.value = [...new Map([...(reset ? [] : items.value), ...r.data].map(p => [p.id, p])).values()]; cursor.value = r.meta.next_cursor; ready.value = true } catch (e) { error.value = errorMessage(e) } finally { busy.value = false } }
onMounted(() => load(true))
</script>
<template><section class="page-banner"><h1>{{ kind === 'following' ? '我的关注' : '关注我的人' }}</h1><p>每一次相遇，都有回响。</p></section><div class="person-list"><div v-for="p in items" :key="p.id" class="person"><InkAvatar :name="displayName(p)" /><RouterLink class="person-info author-link" :to="`/users/${encodeURIComponent(p.username)}`"><strong>{{ displayName(p) }}</strong><small>{{ p.bio || '@' + p.username }}</small></RouterLink><FollowButton :id="p.id" :following="p.following" :name="displayName(p)" @change="p.following = $event" /></div></div><p v-if="busy" class="state-block" role="status">正在加载……</p><div v-if="error" class="state-block" role="alert"><p>{{ error }}</p><button class="secondary" @click="load(!ready)">重试</button></div><div v-else-if="ready && !items.length" class="empty"><h3>这里还很安静</h3><p>去发现分享，也遇见喜欢的人。</p><RouterLink class="link-button" to="/">去发现内容</RouterLink></div><button v-if="cursor" class="secondary more-posts" :disabled="busy" @click="load()">继续查看</button></template>
