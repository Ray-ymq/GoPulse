<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'

const router = useRouter()
const route = useRoute()
const editing = computed(() => route.params.postId !== undefined)
const loading = ref(false)
const loadError = ref('')
const title = ref('')
const content = ref('')
const submitting = ref(false)
const errorMessage = ref('')

async function submit(): Promise<void> {
  if (submitting.value || loading.value || loadError.value) return
  const normalizedTitle = title.value.trim()
  const normalizedContent = content.value.trim()
  if (Array.from(normalizedTitle).length < 1 || Array.from(normalizedTitle).length > 120) {
    errorMessage.value = '标题需为 1–120 个字符。'
    return
  }
  if (Array.from(normalizedContent).length < 1 || Array.from(normalizedContent).length > 10000) {
    errorMessage.value = '正文需为 1–10000 个字符。'
    return
  }
  submitting.value = true
  errorMessage.value = ''
  try {
    const input = { title: normalizedTitle, content: normalizedContent }
    const post = editing.value ? await postApi.update(Number(route.params.postId), input) : await postApi.create(input)
    await router.push(`/posts/${post.id}`)
  } catch (error) {
    errorMessage.value = error instanceof ApiError ? error.message : '保存失败，请稍后重试。'
  } finally {
    submitting.value = false
  }
}
async function load(): Promise<void> {
  title.value = ''; content.value = ''; errorMessage.value = ''; loadError.value = ''
  if (!editing.value) return
  loading.value = true
  try { const post = await postApi.detail(Number(route.params.postId)); title.value = post.title; content.value = post.content }
  catch (error) { loadError.value = error instanceof ApiError ? error.message : '加载失败，请重试。' }
  finally { loading.value = false }
}
watch(() => route.params.postId, load, { immediate: true })
</script>

<template>
  <div>
    <main class="content-shell content-shell--narrow">
      <RouterLink class="back-link" to="/posts">← 返回帖子</RouterLink>
      <section class="form-card">
        <p class="eyebrow">NEW POST</p>
        <h1>{{ editing ? '编辑帖子' : '发布帖子' }}</h1>
        <p v-if="loading" role="status">加载中…</p>
        <p v-if="loadError" role="alert">{{ loadError }} <button @click="load">重试</button></p>
        <form v-if="!loading && !loadError" class="stack-form" :aria-busy="submitting" @submit.prevent="submit">
          <label>
            <span>标题</span>
            <input v-model="title" name="title" maxlength="120" required />
          </label>
          <label>
            <span>正文</span>
            <textarea v-model="content" name="content" rows="10" maxlength="10000" required />
          </label>
          <p aria-live="polite">标题 {{ Array.from(title).length }}/120 · 正文 {{ Array.from(content).length }}/10000</p>
          <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>
          <button class="button button--primary" type="submit" :disabled="submitting">
            {{ submitting ? '保存中…' : editing ? '保存' : '发布' }}
          </button>
        </form>
      </section>
    </main>
  </div>
</template>
