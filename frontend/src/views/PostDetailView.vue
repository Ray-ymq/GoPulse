<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppNav from '../components/AppNav.vue'
import { postApi } from '../services/api'
import { ApiError } from '../services/http'
import type { Comment, Post } from '../types/api'
import { formatDate } from '../utils/format'

const route = useRoute()
const post = ref<Post | null>(null)
const comments = ref<Comment[]>([])
const nextCursor = ref<string | null>(null)
const loadingPost = ref(false)
const loadingComments = ref(false)
const submittingComment = ref(false)
const updatingLike = ref(false)
const commentContent = ref('')
const errorMessage = ref('')
const commentError = ref('')
const postId = computed(() => Number(route.params.postId))

function messageFor(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.code === 'post_not_found') return '帖子不存在或已无法访问。'
  return error instanceof ApiError ? error.message : fallback
}

async function loadPost(): Promise<void> {
  loadingPost.value = true
  errorMessage.value = ''
  try {
    post.value = await postApi.detail(postId.value)
  } catch (error) {
    post.value = null
    errorMessage.value = messageFor(error, '帖子详情加载失败。')
  } finally {
    loadingPost.value = false
  }
}

async function loadComments(reset = false): Promise<void> {
  if (loadingComments.value || (!reset && nextCursor.value === null)) return
  loadingComments.value = true
  commentError.value = ''
  try {
    const page = await postApi.comments(postId.value, reset ? undefined : nextCursor.value ?? undefined)
    comments.value = reset ? page.data : [...comments.value, ...page.data]
    nextCursor.value = page.nextCursor
  } catch (error) {
    commentError.value = messageFor(error, '评论加载失败。')
  } finally {
    loadingComments.value = false
  }
}

async function submitComment(): Promise<void> {
  if (submittingComment.value) return
  const content = commentContent.value.trim()
  if (Array.from(content).length < 1 || Array.from(content).length > 2000) {
    commentError.value = '评论需为 1–2000 个字符。'
    return
  }
  submittingComment.value = true
  commentError.value = ''
  try {
    await postApi.createComment(postId.value, { content })
    commentContent.value = ''
    await Promise.all([loadPost(), loadComments(true)])
  } catch (error) {
    commentError.value = messageFor(error, '评论发布失败。')
  } finally {
    submittingComment.value = false
  }
}

async function toggleLike(): Promise<void> {
  if (!post.value || updatingLike.value) return
  updatingLike.value = true
  errorMessage.value = ''
  try {
    if (post.value.liked_by_me) await postApi.unlike(postId.value)
    else await postApi.like(postId.value)
    await loadPost()
  } catch (error) {
    errorMessage.value = messageFor(error, '点赞操作失败。')
  } finally {
    updatingLike.value = false
  }
}

async function loadAll(): Promise<void> {
  comments.value = []
  nextCursor.value = null
  await Promise.all([loadPost(), loadComments(true)])
}

watch(() => route.params.postId, () => void loadAll())
onMounted(() => void loadAll())
</script>

<template>
  <div>
    <AppNav />
    <main class="content-shell content-shell--narrow">
      <RouterLink class="back-link" to="/posts">← 返回帖子</RouterLink>
      <p v-if="loadingPost && !post" class="state-card">正在加载帖子…</p>
      <p v-else-if="errorMessage && !post" class="notice notice--error" role="alert">{{ errorMessage }}</p>
      <article v-else-if="post" class="detail-card">
        <div class="post-card__meta">
          <span>@{{ post.author.username }}</span>
          <time :datetime="post.created_at">{{ formatDate(post.created_at) }}</time>
        </div>
        <h1>{{ post.title }}</h1>
        <p class="post-content">{{ post.content }}</p>
        <div class="detail-actions">
          <span>评论 {{ post.comment_count }}</span>
          <span>点赞 {{ post.like_count }}</span>
          <button class="button" type="button" :disabled="updatingLike" @click="toggleLike">
            {{ updatingLike ? '处理中…' : post.liked_by_me ? '取消点赞' : '点赞' }}
          </button>
        </div>
        <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>
      </article>

      <section v-if="post" class="comments-section">
        <h2>评论</h2>
        <form class="comment-form" @submit.prevent="submitComment">
          <textarea v-model="commentContent" rows="4" maxlength="2000" placeholder="写下你的评论…" required />
          <button class="button button--primary" type="submit" :disabled="submittingComment">
            {{ submittingComment ? '发布中…' : '发布评论' }}
          </button>
        </form>
        <p v-if="commentError" class="notice notice--error" role="alert">{{ commentError }}</p>
        <p v-if="loadingComments && comments.length === 0" class="state-card">正在加载评论…</p>
        <p v-else-if="!loadingComments && comments.length === 0" class="state-card">还没有评论。</p>
        <div v-else class="comment-list">
          <article v-for="comment in comments" :key="comment.id" class="comment-card">
            <div class="post-card__meta">
              <strong>@{{ comment.author.username }}</strong>
              <time :datetime="comment.created_at">{{ formatDate(comment.created_at) }}</time>
            </div>
            <p>{{ comment.content }}</p>
          </article>
        </div>
        <button v-if="nextCursor" class="button load-more-button" type="button" :disabled="loadingComments" @click="loadComments(false)">
          {{ loadingComments ? '加载中…' : '加载更多评论' }}
        </button>
      </section>
    </main>
  </div>
</template>
