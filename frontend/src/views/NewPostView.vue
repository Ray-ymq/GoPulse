<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import AppNav from '../components/AppNav.vue'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'

const router = useRouter()
const title = ref('')
const content = ref('')
const submitting = ref(false)
const errorMessage = ref('')

async function submit(): Promise<void> {
  if (submitting.value) return
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
    const post = await postApi.create({ title: normalizedTitle, content: normalizedContent })
    await router.push(`/posts/${post.id}`)
  } catch (error) {
    errorMessage.value = error instanceof ApiError ? error.message : '发布失败，请稍后重试。'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div>
    <AppNav />
    <main class="content-shell content-shell--narrow">
      <RouterLink class="back-link" to="/posts">← 返回帖子</RouterLink>
      <section class="form-card">
        <p class="eyebrow">NEW POST</p>
        <h1>发布帖子</h1>
        <form class="stack-form" @submit.prevent="submit">
          <label>
            <span>标题</span>
            <input v-model="title" name="title" maxlength="120" required />
          </label>
          <label>
            <span>正文</span>
            <textarea v-model="content" name="content" rows="10" maxlength="10000" required />
          </label>
          <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>
          <button class="button button--primary" type="submit" :disabled="submitting">
            {{ submitting ? '发布中…' : '发布' }}
          </button>
        </form>
      </section>
    </main>
  </div>
</template>
