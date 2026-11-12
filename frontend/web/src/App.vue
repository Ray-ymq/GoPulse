<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { user, myProfile, clearSession, refreshProfile, signOut } from './composables/session'
import { latestPosts, people, topics, unreadCount, refreshCommunity, refreshNotifications, resetCommunity } from './composables/community'
import { errorMessage } from './services/http'
import { displayName } from './utils/format'
import IconSprite from './components/IconSprite.vue'
import Icon from './components/Icon.vue'
import InkAvatar from './components/InkAvatar.vue'
import FollowButton from './components/FollowButton.vue'
import PostComposer from './components/PostComposer.vue'
import InkDialog from './components/InkDialog.vue'
const route = useRoute(), router = useRouter(), query = ref(''), composer = ref(false), loggingOut = ref(false), logoutError = ref(''), logoutDialog = ref(false), communityError = ref(''), offset = ref(0)
const nav = [ { path: '/', label: '发现广场', icon: 'discover' }, { path: '/following', label: '我的关注', icon: 'users' }, { path: '/bookmarks', label: '我的收藏', icon: 'bookmark' }, { path: '/notifications', label: '消息通知', icon: 'bell' }, { path: '/me', label: '个人主页', icon: 'user' } ]
const suggested = computed(() => { const list = people.value.filter(p => p.id !== user.value?.id); return [...list.slice(offset.value), ...list.slice(0, offset.value)].slice(0, 3) })
const accountName = computed(() => myProfile.value ? displayName(myProfile.value) : user.value?.username || '访客')
let accountGeneration = 0
watch(() => user.value?.id, async id => {
  const generation = ++accountGeneration
  resetCommunity(); composer.value = false; offset.value = 0; communityError.value = ''
  if (!id) return
  const results = await Promise.allSettled([refreshProfile(), refreshCommunity(), refreshNotifications()])
  if (generation !== accountGeneration) return
  if (results[1]?.status === 'rejected') communityError.value = '暂时无法加载社区分享。'
}, { immediate: true })
watch(() => route.query.q, value => { query.value = typeof value === 'string' ? value : '' }, { immediate: true })
watch(() => route.fullPath, async () => { await nextTick(); document.querySelector<HTMLElement>('#main')?.focus({ preventScroll: true }) })
function search() { const q = query.value.trim(); if (q) void router.push({ path: '/search', query: { q } }) }
function compose() { if (user.value) composer.value = true; else void router.push({ path: '/login', query: { redirect: route.fullPath } }) }
function refreshPeople() { if (people.value.length) offset.value = (offset.value + 3) % people.value.length }
async function retryCommunity() { communityError.value = ''; try { await refreshCommunity() } catch (e) { communityError.value = errorMessage(e) } }
async function logout() { if (loggingOut.value) return; loggingOut.value = true; logoutError.value = ''; try { await signOut(); resetCommunity(); composer.value = false; logoutDialog.value = false; await router.replace('/login') } catch (e) { logoutError.value = errorMessage(e) } finally { loggingOut.value = false } }
function expired() { if (!user.value) return; const redirect = route.fullPath; clearSession(); resetCommunity(); composer.value = false; if (!route.meta.public) void router.replace({ path: '/login', query: { redirect } }) }
function keyboard(event: KeyboardEvent) { if (event.key === '/' && !event.ctrlKey && !event.metaKey && !event.altKey && !['INPUT','TEXTAREA','SELECT'].includes(document.activeElement?.tagName || '') && !document.querySelector('dialog[open]')) { event.preventDefault(); document.querySelector<HTMLInputElement>('#search')?.focus() } }
window.addEventListener('gopulse:unauthorized', expired); document.addEventListener('keydown', keyboard)
onBeforeUnmount(() => { window.removeEventListener('gopulse:unauthorized', expired); document.removeEventListener('keydown', keyboard) })
</script>
<template>
<a class="skip-link" href="#main">跳到主要内容</a><IconSprite />
<header class="topbar"><div class="top-inner"><RouterLink class="brand" to="/" aria-label="GoPulse 首页"><span class="brand-mark"><Icon name="pulse" /></span>GoPulse<span>.</span></RouterLink><div class="top-links"><RouterLink to="/" :class="{ active: route.name === 'discover' }">发现</RouterLink><RouterLink to="/following" :class="{ active: route.name === 'following' }">关注</RouterLink></div><form class="search" role="search" @submit.prevent="search"><Icon name="search" /><label class="sr-only" for="search">搜索帖子或作者</label><input id="search" v-model="query" type="search" maxlength="400" placeholder="搜索灵感、生活与同频的人" /><kbd aria-hidden="true">/</kbd></form><div class="top-actions"><RouterLink v-if="user" class="icon-button" to="/notifications" aria-label="查看通知"><Icon name="bell" /><span v-if="unreadCount" class="dot" /></RouterLink><RouterLink :to="user ? '/me' : '/login'" :aria-label="user ? '查看个人主页' : '登录'"><InkAvatar :name="accountName" /></RouterLink></div></div></header>
<div class="layout"><aside class="sidebar"><nav class="nav" aria-label="主导航"><button v-for="item in nav" :key="item.path" :class="{ active: route.path === item.path || item.path === '/me' && route.name === 'relations' }" :aria-current="route.path === item.path ? 'page' : undefined" @click="router.push(item.path)"><Icon :name="item.icon" />{{ item.label }}<span v-if="item.path === '/notifications' && unreadCount" class="badge" :aria-label="`已加载消息中有 ${unreadCount} 条未读`">{{ unreadCount }}</span></button></nav><button class="primary" @click="compose"><Icon name="plus" />发布新帖子</button><p class="nav-label">探索你的兴趣</p><div class="topic-nav"><button v-for="topic in ['生活日常', '旅行见闻', '设计灵感', '数码科技']" :key="topic" @click="router.push({ path: '/search', query: { q: topic } })"><span class="topic-mark" />{{ topic }}</button></div><div class="sidebar-foot"><strong>让每一次分享，都有回响。</strong><span>GoPulse © {{ new Date().getFullYear() }}</span><button v-if="user" class="link-button" @click="logoutDialog = true; logoutError = ''">退出登录</button><RouterLink v-else class="link-button" to="/login">登录 / 注册</RouterLink></div></aside>
<main id="main" class="content" tabindex="-1"><RouterView v-slot="{ Component }"><component :is="Component" :key="route.path + JSON.stringify(route.query)" /></RouterView></main>
<aside class="right" aria-label="社区分享与作者"><section class="panel"><div class="panel-head"><h2>{{ topics.length ? '分享里的话题' : '最近，有这些分享' }}</h2></div><div v-if="user && topics.length" class="hot-list"><RouterLink v-for="(topic, i) in topics" :key="topic" class="hot-item" :to="{ path: '/search', query: { q: topic.slice(1) } }"><span class="rank">{{ String(i + 1).padStart(2, '0') }}</span><span><strong>{{ topic }}</strong><small>看看相关的分享</small></span></RouterLink></div><div v-else-if="user && latestPosts.length" class="hot-list"><RouterLink v-for="(post, i) in latestPosts.slice(0, 4)" :key="post.id" class="hot-item" :to="`/posts/${post.id}`"><span class="rank">{{ String(i + 1).padStart(2, '0') }}</span><span><strong>{{ post.title }}</strong><small>{{ displayName(post.author) }}</small></span></RouterLink></div><p v-else class="hint">{{ user ? '新的分享，正在路上。' : '登录后，展开生活的更多篇章。' }}</p><div v-if="communityError" role="alert"><p class="hint">{{ communityError }}</p><button class="link-button" @click="retryCommunity">重新加载</button></div></section><section class="panel"><div class="panel-head"><h2>遇见同频的人</h2><button v-if="people.length > 3" @click="refreshPeople">换一换 <Icon name="refresh" /></button></div><div v-if="user"><div v-for="p in suggested" :key="p.id" class="person"><InkAvatar :name="displayName(p)" /><RouterLink class="person-info author-link" :to="`/users/${encodeURIComponent(p.username)}`"><strong>{{ displayName(p) }}</strong><small>@{{ p.username }}</small></RouterLink><FollowButton :id="p.id" :name="displayName(p)" :following="p.following" /></div><p v-if="!suggested.length" class="hint">在分享里，慢慢遇见彼此。</p></div><RouterLink v-else class="link-button" to="/register">创建账号，开始相遇</RouterLink></section><div class="footnote"><p>生活有很多面，分享你喜欢的那一面。</p><p>一笔一墨，皆是日常。</p><p v-if="user && unreadCount" class="count-note">未读提示统计最近加载的消息。</p></div></aside></div>
<button class="primary mobile-publish" aria-label="发布新帖子" @click="compose"><Icon name="plus" /></button><PostComposer :open="composer" @close="composer = false" /><InkDialog :open="logoutDialog" title="暂别，随时再见。" label-id="logout-title" @close="logoutDialog = false"><p class="hint">退出后需重新登录，才能查看你的关注和收藏。</p><p v-if="logoutError" class="inline-error" role="alert">{{ logoutError }}</p><div class="inline-actions"><button class="secondary" :disabled="loggingOut" @click="logoutDialog = false">继续浏览</button><button class="primary" :disabled="loggingOut" @click="logout">{{ loggingOut ? '正在退出…' : '退出登录' }}</button></div></InkDialog>
</template>
