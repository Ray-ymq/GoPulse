<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import PostCard from '../components/PostCard.vue'
import { userApi } from '../services/api'
import type { UserProfile, Post } from '../types/api'
import { formatDate } from '../utils/format'
const route = useRoute()
const profile = ref<UserProfile | null>(null)
const posts = ref<Post[]>([])
const cursor = ref<string | null>(null)
const loading = ref(false)
const error = ref('')
const editing = ref(false)
const name = ref('')
const bio = ref('')
const saving = ref(false)
const saveError = ref('')
const success = ref('')
const nameCount = computed(() => Array.from(name.value.trim()).length)
const bioCount = computed(() => Array.from(bio.value.trim()).length)
const nameError = computed(() => nameCount.value < 1 || nameCount.value > 64 ? '显示名称需要 1–64 个字符。' : '')
const bioError = computed(() => bioCount.value > 160 ? '个人介绍不能超过 160 个字符。' : '')
let generation = 0
async function load(reset = false) {
  if (loading.value) return
  const current = generation
  loading.value = true; error.value = ''
  try {
    const username = String(route.params.username)
    if (reset) {
      const result = await userApi.profile(username)
      if (current !== generation) return
      profile.value = result
    }
    const result = await userApi.posts(username, reset ? undefined : cursor.value ?? undefined)
    if (current !== generation) return
    posts.value = reset ? result.data : [...posts.value, ...result.data]
    cursor.value = result.nextCursor
  } catch (e) { if (current === generation) error.value = e instanceof Error ? e.message : '资料加载失败。' }
  finally { if (current === generation) loading.value = false }
}
function edit() {
  if (!profile.value) return
  name.value = profile.value.display_name; bio.value = profile.value.bio
  editing.value = true; saveError.value = ''; success.value = ''
}
async function save() {
  if (saving.value || nameError.value || bioError.value) return
  const current = generation
  saving.value = true; saveError.value = ''
  try {
    const result = await userApi.update(name.value, bio.value)
    if (current !== generation) return
    profile.value = result
    posts.value = posts.value.map(post => ({ ...post, author: { ...post.author, display_name: result.display_name } }))
    editing.value = false; success.value = '资料已更新。'
  } catch (e) { if (current === generation) saveError.value = e instanceof Error ? e.message : '保存失败，请重试。' }
  finally { if (current === generation) saving.value = false }
}
watch(() => route.params.username, () => {
  generation++; profile.value = null; posts.value = []; cursor.value = null
  loading.value = false; editing.value = false; saving.value = false; success.value = ''
  void load(true)
}, { immediate: true })
</script>
<template>
  <main>
    <header class="page-heading"><h1>用户资料</h1></header>
    <p v-if="error" class="notice notice--error" role="alert">{{ error }} <button @click="load(!profile)">重试</button></p>
    <p v-if="loading && !profile" class="state-card" role="status">正在加载资料…</p>
    <section v-if="profile" class="user-profile" aria-label="公开资料">
      <span class="user-avatar user-avatar--large" aria-hidden="true">{{ Array.from(profile.display_name)[0]?.toUpperCase() }}</span>
      <h1>{{ profile.display_name }}</h1><p class="muted">@{{ profile.username }}</p>
      <p class="bio">{{ profile.bio }}</p><p class="muted">加入于 {{ formatDate(profile.created_at) }}</p>
      <button v-if="profile.is_self && !editing" class="button" @click="edit">编辑资料</button>
    </section>
    <p v-if="success" class="notice" role="status">{{ success }}</p>
    <form v-if="editing" class="profile-form" aria-label="编辑资料" @submit.prevent="save">
      <p class="muted">用户名 @{{ profile?.username }} 不可修改</p>
      <label>显示名称<input v-model="name" :aria-invalid="!!nameError" aria-describedby="name-error name-count" /></label>
      <span id="name-count">{{ nameCount }} / 64</span><p v-if="nameError" id="name-error" role="alert">{{ nameError }}</p>
      <label>个人介绍<textarea v-model="bio" rows="4" :aria-invalid="!!bioError" aria-describedby="bio-error bio-count" /></label>
      <span id="bio-count">{{ bioCount }} / 160</span><p v-if="bioError" id="bio-error" role="alert">{{ bioError }}</p>
      <p v-if="saveError" role="alert">{{ saveError }}</p>
      <button class="button button--primary" :disabled="saving || !!nameError || !!bioError">{{ saving ? '保存中…' : '保存资料' }}</button>
      <button type="button" class="button" :disabled="saving" @click="editing = false">取消</button>
    </form>
    <section v-if="profile" aria-label="用户帖子">
      <PostCard v-for="post in posts" :key="post.id" :post="post" />
      <p v-if="!loading && !error && posts.length === 0" class="state-card">还没有帖子。</p>
      <p v-if="loading" class="state-card" role="status">正在加载帖子…</p>
      <div v-if="cursor" class="load-more"><button class="button" :disabled="loading" @click="load()">加载更多</button></div>
    </section>
  </main>
</template>
