<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { ApiError } from '../services/http'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const retrying = ref(false)
const errorMessage = ref('')

function targetPath(): string {
  const redirect = route.query.redirect
  return typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')
    ? redirect
    : '/posts'
}

async function retry(): Promise<void> {
  if (retrying.value) return
  retrying.value = true
  errorMessage.value = ''
  try {
    await auth.initialize()
    await router.replace(auth.status.value === 'authenticated' ? targetPath() : '/login')
  } catch (error) {
    errorMessage.value = error instanceof ApiError
      ? error.message
      : '认证状态仍无法恢复，请稍后重试。'
  } finally {
    retrying.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-card auth-recovery-card" aria-labelledby="auth-recovery-title">
      <RouterLink class="brand" to="/">GoPulse</RouterLink>
      <p class="eyebrow">SESSION RECOVERY</p>
      <h1 id="auth-recovery-title">暂时无法确认登录状态</h1>
      <p class="muted">
        你的会话没有被清除。服务恢复后可直接重试，无需重新登录。
      </p>
      <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>
      <button class="button button--primary" type="button" :disabled="retrying" @click="retry">
        {{ retrying ? '正在重试…' : '重试认证恢复' }}
      </button>
    </section>
  </main>
</template>
