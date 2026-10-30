<script setup lang="ts">
import { ref } from 'vue'
import { authApi } from '../services/api'
import { ApiError } from '../services/http'

const username = ref('')
const password = ref('')
const registering = ref(false)
const pending = ref(false)
const message = ref(new URLSearchParams(window.location.search).has('forbidden') ? '当前账户没有管理权限，请使用管理员账户登录。' : '')

async function submit() {
  if (pending.value) return
  pending.value = true
  message.value = ''
  try {
    const user = await (registering.value ? authApi.register : authApi.login)(username.value, password.value)
    password.value = ''
    if (user.role === 'super_admin') {
      window.location.replace('/admin/')
    } else {
      message.value = '账户验证成功，请联系管理员授予管理权限后重新登录。'
      registering.value = false
    }
  } catch (error) {
    message.value = error instanceof ApiError ? error.message : '登录失败，请稍后重试。'
  } finally { pending.value = false }
}
</script>

<template>
  <main class="management-login">
    <form class="panel" @submit.prevent="submit">
      <h1>GoPulse 管理中心</h1>
      <h2>{{ registering ? '注册账户' : '管理员登录' }}</h2>
      <label for="management-username">用户名</label>
      <input id="management-username" v-model="username" name="username" autocomplete="username" required pattern="[A-Za-z0-9_]{3,32}" maxlength="32" :disabled="pending">
      <label for="management-password">密码</label>
      <input id="management-password" v-model="password" name="password" type="password" :autocomplete="registering ? 'new-password' : 'current-password'" required minlength="8" :disabled="pending">
      <p v-if="message" role="status">{{ message }}</p>
      <button class="button" type="submit" :disabled="pending">{{ pending ? '正在提交…' : registering ? '注册' : '登录' }}</button>
      <button class="button button--secondary" type="button" :disabled="pending" @click="registering = !registering; message = ''; password = ''">{{ registering ? '返回登录' : '注册账户' }}</button>
    </form>
  </main>
</template>

<style scoped>
.management-login { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
form { width: min(100%, 420px); display: grid; gap: 16px; padding: 32px; }
input { width: 100%; min-height: 42px; padding: 8px 12px; border: 1px solid #cad4e0; border-radius: 8px; }
</style>
