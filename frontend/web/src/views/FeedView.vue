<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import PostFeed from '../components/PostFeed.vue'
const route = useRoute()
const mode = computed(() => String(route.name))
const endpoint = computed(() => mode.value === 'following' ? '/posts/following' : mode.value === 'bookmarks' ? '/bookmarks' : '/posts')
const headings: Record<string, [string, string]> = { following: ['我的关注', '看看关注的人，最近分享了什么。'], bookmarks: ['我的收藏', '把喜欢的内容留住，想起时再回来看看。'] }
</script>
<template><section v-if="mode === 'discover'" class="hero" aria-label="社区介绍"><div class="section-eyebrow">日常有迹 · 分享有声</div><h1>发现，分享，相遇。</h1><p>记录生活，分享想法，遇见同频的人。</p></section><section v-else class="page-banner"><h1>{{ headings[mode]?.[0] }}</h1><p>{{ headings[mode]?.[1] }}</p></section><div class="section-head"><div class="section-title"><h2>{{ mode === 'discover' ? '发现新鲜事' : mode === 'following' ? '关注动态' : '收藏的分享' }}</h2><span v-if="mode === 'discover'">总有一种热爱，与你同频</span></div><span class="hint">按发布时间</span></div><nav v-if="mode === 'discover'" class="filters" aria-label="探索话题"><RouterLink class="chip active" to="/" aria-current="page">全部</RouterLink><RouterLink v-for="topic in ['生活日常', '旅行见闻', '设计灵感', '数码科技', '阅读时光']" :key="topic" class="chip" :to="{ path: '/search', query: { q: topic } }">{{ topic }}</RouterLink></nav><PostFeed :endpoint="endpoint" :bookmarks="mode === 'bookmarks'" :empty-title="mode === 'bookmarks' ? '把喜欢留在这里' : mode === 'following' ? '这里还很安静' : '你的故事，从这里开始'" :empty-text="mode === 'bookmarks' ? '点击帖子下方的收藏按钮，就能随时回来阅读。' : mode === 'following' ? '关注感兴趣的作者，他们的分享就会出现在这里。' : '分享一个想法，或记录今天的小小发现。'" /></template>
