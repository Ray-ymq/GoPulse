<script setup lang="ts">
import { ref } from 'vue'
import { requestVoid } from '../services/http'
const logoutError = ref(false)
async function logout() {
  logoutError.value = false
  try { await requestVoid('/auth/logout', { method: 'POST' }); window.location.replace('/login') }
  catch { logoutError.value = true }
}
</script>
<template>
  <div class="admin-shell">
    <a class="gp-skip" href="#admin-main">跳至主要内容</a>
    <header class="admin-header">
      <div><p class="admin-eyebrow">GOPULSE ADMIN</p><h1>可观测中心</h1></div>
      <a class="button button--secondary" href="/posts">返回社交</a>
    <button class="button" @click="logout">退出登录</button><p v-if="logoutError" role="alert">退出失败，请重试。</p></header>
    <nav class="admin-nav" aria-label="可观测导航">
      <RouterLink to="/">大屏</RouterLink>
      <RouterLink to="/metrics">Metrics</RouterLink>
      <RouterLink to="/logs">Logs</RouterLink>
      <RouterLink to="/events">Events</RouterLink>
      <RouterLink to="/alerts">告警</RouterLink><RouterLink to="/users">用户角色</RouterLink><RouterLink to="/audit">审计</RouterLink>
      <RouterLink to="/plugins">Exporter</RouterLink>
    </nav>
    <main id="admin-main" tabindex="-1" class="admin-content"><RouterView /></main>
  </div>
</template>
