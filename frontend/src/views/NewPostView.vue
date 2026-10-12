<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { usePostCommand } from '../composables/usePostCommand'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'
import type { CreatePostInput, PostCommandMeta } from '../types/api'

const router = useRouter()
const route = useRoute()
const auth = useAuth()
const editing = computed(() => route.params.postId !== undefined)
const userID = computed(() => auth.user.value?.id ?? null)
const command = usePostCommand(userID)
const loading = ref(false)
const loadError = ref('')
const title = ref('')
const content = ref('')
const submitting = ref(false)
const errorMessage = ref('')
const committedMeta = ref<PostCommandMeta | null>(null)

function validateInput(): CreatePostInput | null {
  const normalizedTitle = title.value.trim()
  const normalizedContent = content.value.trim()
  if (Array.from(normalizedTitle).length < 1 || Array.from(normalizedTitle).length > 120) {
    errorMessage.value = '标题需为 1–120 个字符。'
    return null
  }
  if (Array.from(normalizedContent).length < 1 || Array.from(normalizedContent).length > 10000) {
    errorMessage.value = '正文需为 1–10000 个字符。'
    return null
  }
  return { title: normalizedTitle, content: normalizedContent }
}

async function handleCommandResult(result: { data: import('../types/api').Post | null; meta: PostCommandMeta }): Promise<void> {
  if (result.data) {
    await router.push(`/posts/${result.data.id}`)
    return
  }
  committedMeta.value = result.meta
  errorMessage.value = '帖子已提交，但当前无法读取帖子内容。提交身份仍已保留。'
}

async function submit(): Promise<void> {
  if (submitting.value || loading.value || loadError.value) return
  const input = validateInput()
  if (!input) return
  submitting.value = true
  errorMessage.value = ''
  committedMeta.value = null
  try {
    if (editing.value) {
      const post = await postApi.update(Number(route.params.postId), input)
      await router.push(`/posts/${post.id}`)
      return
    }
    await handleCommandResult(await command.submit(input))
  } catch (error) {
    errorMessage.value = error instanceof ApiError ? error.message : '保存失败，请稍后重试。'
  } finally {
    submitting.value = false
  }
}

async function checkCommand(): Promise<void> {
  if (submitting.value || !command.draft.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    await handleCommandResult(await command.check())
  } catch (error) {
    errorMessage.value = error instanceof ApiError ? error.message : '查证失败，请稍后重试。'
  } finally {
    submitting.value = false
  }
}

function startNewCommand(): void {
  command.startNew()
  title.value = ''
  content.value = ''
  errorMessage.value = ''
  committedMeta.value = null
}

async function load(): Promise<void> {
  title.value = ''; content.value = ''; errorMessage.value = ''; loadError.value = ''; committedMeta.value = null
  if (!editing.value) {
    const restored = command.restoreInput()
    if (restored) {
      title.value = restored.title
      content.value = restored.content
    }
    return
  }
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
        <template v-if="!loading && !loadError">
          <p v-if="command.outcomeUnknown.value && !editing" class="notice notice--error" role="alert">
            提交结果尚未确认。可以查证提交结果，或使用相同内容重试；请不要修改正文后继续使用当前身份。
          </p>
          <p v-if="committedMeta" class="notice" role="status">{{ errorMessage }}</p>
          <form class="stack-form" :aria-busy="submitting" @submit.prevent="submit">
            <label>
              <span>标题</span>
              <input v-model="title" name="title" maxlength="120" required :readonly="command.locked.value && !editing" />
            </label>
            <label>
              <span>正文</span>
              <textarea v-model="content" name="content" rows="10" maxlength="10000" required :readonly="command.locked.value && !editing" />
            </label>
            <p aria-live="polite">标题 {{ Array.from(title).length }}/120 · 正文 {{ Array.from(content).length }}/10000</p>
            <p v-if="errorMessage && !committedMeta" class="notice notice--error" role="alert">{{ errorMessage }}</p>
            <div class="form-actions">
              <button class="button button--primary" type="submit" :disabled="submitting">
                {{ submitting ? '处理中…' : editing ? '保存' : command.locked.value ? '使用相同内容重试' : '发布' }}
              </button>
              <button v-if="command.outcomeUnknown.value && !editing" class="button" type="button" :disabled="submitting" @click="checkCommand">查证提交结果</button>
              <button v-if="command.locked.value && !editing" class="button" type="button" :disabled="submitting" @click="startNewCommand">开始新的发帖</button>
            </div>
          </form>
        </template>
      </section>
    </main>
  </div>
</template>
