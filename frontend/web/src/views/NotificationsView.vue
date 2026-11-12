<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import { user } from '../composables/session'
import { unreadCount } from '../composables/community'
import type { Notification } from '../types/api'
import { displayName, timeLabel } from '../utils/format'
import InkAvatar from '../components/InkAvatar.vue'
const items = ref<Notification[]>([]), cursor = ref<string | null>(null), loading = ref(false), busy = ref(false), ready = ref(false), error = ref(''), actionError = ref('')
const owner = user.value?.id
const labels: Record<string, string> = { 'comment.created': '评论了你的分享', 'post.liked': '喜欢了你的分享', 'user.followed': '关注了你', 'post.created': '发布了新的分享', 'post.updated': '更新了分享', 'post.deleted': '删除了分享' }
function updateCount() { if (user.value?.id !== owner) return; unreadCount.value = items.value.filter(n => !n.read_at).length }
async function load(reset = false) { if (loading.value) return; loading.value = true; error.value = ''; try { const result = await api.notifications(reset ? undefined : cursor.value); items.value = [...new Map([...(reset ? [] : items.value), ...result.data].map(n => [n.id, n])).values()]; cursor.value = result.meta.next_cursor; updateCount(); ready.value = true } catch (e) { error.value = errorMessage(e) } finally { loading.value = false } }
async function read(n: Notification) { if (busy.value || n.read_at) return; busy.value = true; actionError.value = ''; try { await api.read(n.id); n.read_at = new Date().toISOString(); updateCount() } catch (e) { actionError.value = errorMessage(e) } finally { busy.value = false } }
async function readVisible() { if (busy.value) return; busy.value = true; actionError.value = ''; try { for (const n of items.value.filter(n => !n.read_at)) { await api.read(n.id); n.read_at = new Date().toISOString() } } catch (e) { actionError.value = errorMessage(e) } finally { updateCount(); busy.value = false } }
onMounted(() => load(true))
</script>
<template><section class="page-banner"><h1>消息通知</h1><p>每一次互动，都是分享的回响。</p></section><div class="section-head"><div class="section-title"><h2>互动消息</h2></div><button class="link-button" :disabled="busy || !items.some(n => !n.read_at)" @click="readVisible">{{ busy ? '处理中…' : '当前消息标为已读' }}</button></div><p v-if="actionError" class="inline-error" role="alert">{{ actionError }}</p><div class="feed"><article v-for="n in items" :key="n.id" class="notice" :class="{ unread: !n.read_at }"><InkAvatar :name="displayName(n.actor)" /><div class="notice-body"><p><RouterLink :to="`/users/${encodeURIComponent(n.actor.username)}`"><strong>{{ displayName(n.actor) }}</strong></RouterLink> {{ labels[n.type] || '与你产生了新的互动' }}</p><small>{{ timeLabel(n.created_at) }} · {{ n.read_at ? '已读' : '未读' }}</small><p v-if="n.resource_deleted" class="hint">相关内容已删除。</p><RouterLink v-else-if="n.post_id" class="notification-link" :to="`/posts/${n.post_id}`" @click="read(n)">查看相关分享</RouterLink><button v-if="!n.read_at" class="link-button" :disabled="busy" @click="read(n)">标为已读</button></div></article></div><p v-if="loading" class="state-block" role="status">正在加载消息……</p><div v-if="error" class="state-block" role="alert"><p>{{ error }}</p><button class="secondary" @click="load(!ready)">重试</button></div><div v-else-if="ready && !items.length" class="empty"><h3>暂时没有新消息</h3><p>有了新的互动，会在这里告诉你。</p></div><button v-if="cursor" class="secondary more-posts" :disabled="loading" @click="load()">继续查看</button><p v-if="cursor" class="count-note">未读数仅统计已加载的消息。</p></template>
