<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { signIn } from '../composables/session'
import { errorMessage } from '../services/http'
import { safeRedirect } from '../utils/format'
const route = useRoute(), router = useRouter(), username = ref(''), password = ref(''), error = ref(''), busy = ref(false)
const register = computed(() => route.name === 'register')
async function submit() {
  if (busy.value) return
  const name = username.value.trim()
  if (!/^[A-Za-z0-9_]{3,32}$/.test(name)) { error.value = '用户名需为 3–32 位字母、数字或下划线。'; return }
  if (Array.from(password.value).length < 8 || new TextEncoder().encode(password.value).length > 72) { error.value = '密码至少需有 8 个字符，UTF-8 编码长度不得超过 72 字节。'; return }
  busy.value = true; error.value = ''
  try { await signIn(name, password.value, register.value); password.value = ''; await router.replace(safeRedirect(route.query.redirect)) }
  catch (e) { error.value = errorMessage(e) } finally { busy.value = false }
}
</script>
<template><section class="auth-view"><div class="ink-edition">一笔一墨 · 皆是日常</div><h1>{{ register ? '从这里，写下日常。' : '欢迎回来。' }}</h1><p>{{ register ? '创建账号，开始你的第一篇分享。' : '登录 GoPulse，继续阅读与你同频的生活。' }}</p><p v-if="route.query.unavailable" class="app-notice" role="status">暂时无法连接服务。请确认后端已启动，再尝试登录。</p><form @submit.prevent="submit"><label class="form-field"><span>用户名</span><input v-model="username" name="username" autocomplete="username" required maxlength="32" placeholder="3–32 位字母、数字或下划线" :disabled="busy" /></label><label class="form-field"><span>密码</span><input v-model="password" name="password" type="password" :autocomplete="register ? 'new-password' : 'current-password'" required placeholder="至少 8 个字符" :disabled="busy" /></label><p v-if="error" class="inline-error" role="alert">{{ error }}</p><button class="primary" :disabled="busy">{{ busy ? '正在处理…' : register ? '创建账号' : '登录' }}</button></form><p class="hint">{{ register ? '已有账号？' : '还没有账号？' }}<RouterLink :to="{ path: register ? '/login' : '/register', query: { redirect: route.query.redirect } }">{{ register ? '直接登录' : '注册账号' }}</RouterLink></p></section></template>
