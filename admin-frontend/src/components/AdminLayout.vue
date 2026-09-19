<script setup lang="ts">
import { computed, ref } from 'vue'
import AdminIcon from './AdminIcon.vue'
import { useRoute } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { requestVoid } from '../services/http'
const auth = useAuth()
const route = useRoute()
const open = ref(false)
const logoutError = ref(false)
const loggingOut = ref(false)
const groups = [
  { title: '概览', links: [{ to: '/', label: '系统概览', icon: 'home' }] },
  { title: '可观测', links: [{ to: '/metrics', label: 'Metrics', icon: 'metrics' }, { to: '/logs', label: 'Logs', icon: 'logs' }, { to: '/events', label: 'Events', icon: 'events' }, { to: '/alerts', label: 'Alerts', icon: 'alerts' }] },
  { title: '采集管理', links: [{ to: '/plugins', label: 'Exporter', icon: 'box' }] },
  { title: '系统管理', links: [{ to: '/users', label: '用户角色', icon: 'users' }, { to: '/audit', label: '审计', icon: 'audit' }] },
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
      <div class="admin-brand"><AdminIcon name="pulse" :size="30" /><strong>GoPulse <small>管理中心</small></strong></div>
      <button class="nav-toggle button button--secondary" :aria-expanded="open" aria-controls="admin-navigation" @click="open = !open"><AdminIcon name="menu" />管理导航</button>
      <nav id="admin-navigation" class="admin-nav" :class="{ 'is-open': open }" aria-label="可观测导航">
        <div v-for="group in groups" :key="group.title" class="nav-group">
          <p>{{ group.title }}</p>
          <RouterLink v-for="link in group.links" :key="link.to" :to="link.to" @click="open = false"><AdminIcon :name="link.icon" />{{ link.label }}</RouterLink>
        </div>
      </nav>
      <p class="sidebar-note">运行状态以实时查询结果为准</p>
    </aside>
    <div class="admin-workspace">
      <header class="admin-header">
        <div class="admin-breadcrumb"><AdminIcon name="menu" /><span>GoPulse</span><span>/</span><h1>可观测中心</h1><span>/</span><strong>{{ title }}</strong></div>
        <div class="admin-account"><a class="button button--secondary" href="/posts">返回社交</a><span class="account-avatar"><AdminIcon name="user" /></span><span class="admin-identity">{{ auth.user.value?.username }} <small>超级管理员</small></span><button class="button button--secondary" :disabled="loggingOut" @click="logout">退出登录</button></div>
        <p v-if="logoutError" role="alert">退出失败，请重试。</p>
      </header>
      <main id="admin-main" tabindex="-1" class="admin-content"><RouterView /></main>
    </div>
  </div>
</template>
