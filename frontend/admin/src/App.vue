<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { standaloneObservability } from './composables/deployment'
import { useAuth } from './composables/useAuth'
const auth = useAuth()
const loginPage = /^\/admin\/login\/?$/.test(window.location.pathname)
const error = ref(false)
async function bootstrap() {
  error.value = false
  try {
    await auth.refresh()
    if (auth.user.value?.role !== 'super_admin') window.location.replace(standaloneObservability.value ? '/admin/login?forbidden=1' : '/posts')
  } catch { error.value = true }
}
onMounted(() => { if (!loginPage) void bootstrap() })
</script>
<template>
  <RouterView v-if="loginPage || auth.user.value?.role === 'super_admin'" />
  <main v-else><p role="status">{{ error ? '会话恢复失败，请重试。' : '正在验证管理权限…' }}</p><button v-if="error" type="button" @click="bootstrap">重试</button></main>
</template>
