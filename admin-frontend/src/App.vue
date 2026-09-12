<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAuth } from './composables/useAuth'
const auth = useAuth()
const error = ref(false)
async function bootstrap() {
  error.value = false
  try {
    await auth.refresh()
    if (auth.user.value?.role !== 'super_admin') window.location.replace('/posts')
  } catch { error.value = true }
}
onMounted(bootstrap)
</script>
<template>
  <RouterView v-if="auth.user.value?.role === 'super_admin'" />
  <main v-else><p role="status">{{ error ? '会话恢复失败，请重试。' : '正在验证管理权限…' }}</p><button v-if="error" @click="bootstrap">重试</button></main>
</template>
