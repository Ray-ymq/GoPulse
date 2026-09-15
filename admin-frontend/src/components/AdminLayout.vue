<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { requestVoid } from '../services/http'
const auth = useAuth()
const route = useRoute()
const open = ref(false)
const logoutError = ref(false)
const loggingOut = ref(false)
const groups = [
  { title: 'OVERVIEW', links: [{ to: '/', label: '管理大屏', icon: 'D' }] },
  { title: 'OBSERVABILITY', links: [{ to: '/metrics', label: 'Metrics', icon: 'M' }, { to: '/logs', label: 'Logs', icon: 'L' }, { to: '/events', label: 'Events', icon: 'E' }, { to: '/alerts', label: '告警', icon: 'A' }] },
  { title: 'MANAGEMENT', links: [{ to: '/plugins', label: 'Exporter', icon: 'X' }, { to: '/users', label: '用户角色', icon: 'U' }, { to: '/audit', label: '审计', icon: 'R' }] },
]
const title = computed(() => groups.flatMap(group => group.links).find(link => link.to === route.path)?.label ?? '管理中心')
async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  logoutError.value = false
  try { await requestVoid('/auth/logout', { method: 'POST' }); window.location.replace('/login') }
  catch { logoutError.value = true }
  finally { loggingOut.value = false }
}
</script>
<template>
  <div class="admin-shell">
    <a class="gp-skip" href="#admin-main">跳至主要内容</a>
    <aside class="admin-sidebar">
      <div class="admin-brand"><span class="brand-mark" aria-hidden="true">GP</span><div><strong>GoPulse</strong><small>OBSERVABILITY</small></div></div>
      <button class="nav-toggle button button--secondary" :aria-expanded="open" aria-controls="admin-navigation" @click="open = !open">管理导航</button>
      <nav id="admin-navigation" class="admin-nav" :class="{ 'is-open': open }" aria-label="可观测导航">
        <div v-for="group in groups" :key="group.title" class="nav-group">
          <p>{{ group.title }}</p>
          <RouterLink v-for="link in group.links" :key="link.to" :to="link.to" @click="open = false"><span class="nav-icon" aria-hidden="true">{{ link.icon }}</span>{{ link.label }}</RouterLink>
        </div>
      </nav>
      <p class="sidebar-note">运行状态以实时查询结果为准</p>
    </aside>
    <div class="admin-workspace">
      <header class="admin-header">
        <div class="admin-breadcrumb"><span>GoPulse / </span><h1>可观测中心</h1><span> / <strong>{{ title }}</strong></span></div>
        <div class="admin-account"><a class="button button--secondary" href="/posts">返回社交</a><span class="admin-identity">{{ auth.user.value?.username }} <small>超级管理员</small></span><button class="button button--secondary" :disabled="loggingOut" @click="logout">退出登录</button></div>
        <p v-if="logoutError" role="alert">退出失败，请重试。</p>
      </header>
      <main id="admin-main" tabindex="-1" class="admin-content"><RouterView /></main>
    </div>
  </div>
</template>
