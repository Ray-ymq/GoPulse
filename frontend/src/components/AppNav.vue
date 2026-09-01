<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '../composables/useAuth'

const auth = useAuth()
const router = useRouter()
const signingOut = ref(false)

async function signOut(): Promise<void> {
  if (signingOut.value) return
  signingOut.value = true
  try {
    await auth.logout()
    await router.push('/login')
  } finally {
    signingOut.value = false
  }
}
</script>

<template>
  <header class="app-header">
    <RouterLink class="brand" to="/posts">GoPulse</RouterLink>
    <nav v-if="auth.status.value === 'authenticated'" class="app-nav" aria-label="主导航">
      <RouterLink to="/posts">帖子</RouterLink>
      <RouterLink class="button button--small" to="/posts/new">发布</RouterLink>
      <span class="nav-user">@{{ auth.user.value?.username }}</span>
      <button class="link-button" type="button" :disabled="signingOut" @click="signOut">
        {{ signingOut ? '退出中…' : '退出' }}
      </button>
    </nav>
  </header>
</template>
