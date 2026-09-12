<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { loginDestination } from '../utils/redirect'
import { ApiError } from '../services/http'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const retrying = ref(false)
const errorMessage = ref('')

async function retry(): Promise<void> {
  if (retrying.value) return
  retrying.value = true
  errorMessage.value = ''
  try {
    await auth.initialize()
    const target = auth.status.value === 'authenticated' ? loginDestination(route.query.redirect, auth.user.value?.role) : '/login'
    if (target.startsWith('/admin/')) window.location.replace(target)
    else await router.replace(target)
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
