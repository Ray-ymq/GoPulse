<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppNav from '../components/AppNav.vue'
import { notificationApi } from '../services/api'
import { ApiError } from '../services/http'
import type { Notification } from '../types/api'
import { formatDate } from '../utils/format'

const notifications = ref<Notification[]>([])
const nextCursor = ref<string | null>(null)
const initialized = ref(false)
const loading = ref(false)
const refreshing = ref(false)
const loadError = ref('')
const refreshError = ref('')
const actionError = ref('')
const marking = ref<Set<number>>(new Set())

function messageFor(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

async function load(reset = false): Promise<void> {
  if (loading.value || refreshing.value || (!reset && initialized.value && nextCursor.value === null)) return
  if (reset) refreshing.value = initialized.value
  else loading.value = true
  loadError.value = ''
  refreshError.value = ''
  try {
    const page = await notificationApi.list(reset ? undefined : nextCursor.value ?? undefined)
    notifications.value = reset ? page.data : [...notifications.value, ...page.data]
    nextCursor.value = page.nextCursor
    initialized.value = true
  } catch (error) {
    const message = messageFor(error, reset && initialized.value ? '通知刷新失败。' : '通知加载失败。')
    if (reset && initialized.value) refreshError.value = message
    else loadError.value = message
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function markRead(item: Notification): Promise<void> {
  if (item.read_at !== null || marking.value.has(item.id)) return
  marking.value = new Set(marking.value).add(item.id)
  actionError.value = ''
  try {
    await notificationApi.markRead(item.id)
    notifications.value = notifications.value.map((notification) =>
      notification.id === item.id ? { ...notification, read_at: new Date().toISOString() } : notification,
    )
  } catch (error) {
    actionError.value = messageFor(error, '标记已读失败。')
  } finally {
    const pending = new Set(marking.value)
    pending.delete(item.id)
    marking.value = pending
  }
}

onMounted(() => void load(true))
</script>

<template>
  <div>
    <AppNav />
    <main class="content-shell content-shell--narrow">
      <div class="page-heading">
        <div>
          <p class="eyebrow">异步动态</p>
          <h1>通知</h1>
          <p class="muted">评论和点赞通知由后台异步处理，可能稍后到达；请刷新查看最新结果。</p>
        </div>
        <button class="button" type="button" :disabled="loading || refreshing" @click="load(true)">
          {{ refreshing ? '刷新中…' : '刷新' }}
        </button>
      </div>

      <p v-if="refreshError" class="notice notice--error" role="alert">刷新失败：{{ refreshError }}</p>
      <p v-if="actionError" class="notice notice--error" role="alert">{{ actionError }}</p>
      <p v-if="loading && !initialized" class="state-card">正在加载通知…</p>
      <div v-else-if="loadError && notifications.length === 0" class="state-card">
        <p class="notice notice--error" role="alert">{{ loadError }}</p>
        <button class="button" type="button" @click="load(true)">重试</button>
      </div>
      <p v-else-if="initialized && notifications.length === 0" class="state-card">暂时没有通知。</p>

      <section v-else class="notification-list" aria-label="通知列表">
        <article
          v-for="item in notifications"
          :key="item.id"
          class="notification-card"
          :class="{ 'notification-card--unread': item.read_at === null }"
        >
          <div class="notification-card__body">
            <div class="post-card__meta">
              <strong>@{{ item.actor.username }}</strong>
              <time :datetime="item.created_at">{{ formatDate(item.created_at) }}</time>
              <span class="notification-status">{{ item.read_at === null ? '未读' : '已读' }}</span>
            </div>
            <p>
              {{ item.type === 'comment.created' ? '评论了你的帖子' : '赞了你的帖子' }}
              <RouterLink :to="`/posts/${item.post_id}`">查看帖子</RouterLink>
            </p>
          </div>
          <button
            v-if="item.read_at === null"
            class="button button--small"
            type="button"
            :disabled="marking.has(item.id)"
            @click="markRead(item)"
          >
            {{ marking.has(item.id) ? '处理中…' : '标记已读' }}
          </button>
        </article>
      </section>

      <div v-if="initialized && notifications.length > 0" class="load-more">
        <button v-if="nextCursor" class="button" type="button" :disabled="loading" @click="load(false)">
          {{ loading ? '加载中…' : '加载更多' }}
        </button>
        <p v-else class="muted">已无更多通知。</p>
        <p v-if="loadError" class="notice notice--error" role="alert">{{ loadError }}</p>
      </div>
    </main>
  </div>
</template>
