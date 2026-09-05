<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { ApiError } from '../services/http'

const props = defineProps<{ mode: 'login' | 'register' }>()
const auth = useAuth()
const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const submitting = ref(false)
const errorMessage = ref('')
const title = computed(() => (props.mode === 'login' ? '欢迎回来' : '创建账号'))
const destination = computed(() => {
  const redirect = route.query.redirect
  return props.mode === 'login' && typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/posts'
})

function validate(): string {
  const normalized = username.value.trim()
  if (!/^[A-Za-z0-9_]{3,32}$/.test(normalized)) return '用户名需为 3–32 位字母、数字或下划线。'
  const characters = Array.from(password.value).length
  if (characters < 8 || new TextEncoder().encode(password.value).length > 72) {
    return '密码需至少 8 个字符且不超过 72 字节。'
  }
  return ''
}

function messageFor(error: unknown): string {
  if (!(error instanceof ApiError)) return '操作失败，请稍后重试。'
  if (error.code === 'username_conflict') return '该用户名已被使用。'
  if (error.code === 'invalid_credentials') return '用户名或密码错误。'
  if (error.code === 'validation_failed') return '提交内容不符合要求，请检查后重试。'
  return error.message
}

async function submit(): Promise<void> {
  if (submitting.value) return
  errorMessage.value = validate()
  if (errorMessage.value) return
  submitting.value = true
  try {
    const credentials = { username: username.value.trim(), password: password.value }
    if (props.mode === 'login') await auth.login(credentials)
    else await auth.register(credentials)
    await router.push(destination.value)
  } catch (error) {
    errorMessage.value = messageFor(error)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-card">
      <RouterLink class="brand" to="/">GoPulse</RouterLink>
      <p class="eyebrow">PHASE 1</p>
      <h1>{{ title }}</h1>
      <p class="muted">用一个轻量账号进入帖子、评论与点赞闭环。</p>
      <form class="stack-form" @submit.prevent="submit">
        <label>
          <span>用户名</span>
          <input v-model="username" name="username" autocomplete="username" maxlength="32" required />
        </label>
        <label>
          <span>密码</span>
          <input
            v-model="password"
            name="password"
            type="password"
            :autocomplete="mode === 'login' ? 'current-password' : 'new-password'"
            required
          />
        </label>
        <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>
        <button class="button button--primary" type="submit" :disabled="submitting">
          {{ submitting ? '提交中…' : mode === 'login' ? '登录' : '注册并登录' }}
        </button>
      </form>
      <p class="auth-switch">
        {{ mode === 'login' ? '还没有账号？' : '已经有账号？' }}
        <RouterLink :to="mode === 'login' ? '/register' : '/login'">
          {{ mode === 'login' ? '立即注册' : '返回登录' }}
        </RouterLink>
      </p>
    </section>
  </main>
</template>
