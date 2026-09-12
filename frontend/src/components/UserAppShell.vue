<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import '../user.css'
const auth = useAuth()
const router = useRouter()
const leaving = ref(false)
const error = ref('')
async function logout() {
  leaving.value = true
  try { await auth.logout(); await router.push('/login') }
  catch { error.value = '退出失败，请重试。' }
  finally { leaving.value = false }
}
</script>
<template>
  <div class="user-shell">
    <a class="user-skip" href="#user-content">跳转到内容</a>
    <aside class="user-sidebar">
      <RouterLink class="user-brand" to="/posts" aria-label="GoPulse 首页">G<span>oPulse</span></RouterLink>
      <nav class="user-navigation" aria-label="用户主导航">
        <RouterLink to="/posts" aria-label="首页"><span aria-hidden="true">⌂</span><span class="nav-label">首页</span></RouterLink>
        <RouterLink to="/search" aria-label="搜索"><span aria-hidden="true">⌕</span><span class="nav-label">搜索</span></RouterLink>
        <RouterLink to="/notifications" aria-label="通知"><span aria-hidden="true">♧</span><span class="nav-label">通知</span></RouterLink>
        <RouterLink to="/bookmarks"><span aria-hidden="true">♧</span><span class="nav-label">收藏</span></RouterLink>
        <RouterLink :to="`/users/${auth.user.value?.username}`" aria-label="我的资料"><span aria-hidden="true">◎</span><span class="nav-label">我的资料</span></RouterLink>
      </nav>
      <RouterLink class="user-publish" to="/posts/new" aria-label="发布"><span aria-hidden="true">＋</span><span class="nav-label">发布</span></RouterLink>
      <div class="user-account">
        <p>@{{ auth.user.value?.username }}</p>
        <a v-if="auth.user.value?.role === 'super_admin'" href="/admin/">管理中心</a>
        <button :disabled="leaving" @click="logout">{{ leaving ? '退出中…' : '退出' }}</button>
        <p v-if="error" role="alert">{{ error }}</p>
      </div>
    </aside>
    <div id="user-content" class="user-timeline" tabindex="-1"><p v-if="auth.user.value?.management_setup_available === false" role="status">管理尚未初始化，请由运维声明引导管理员；社交功能仍可使用。</p><RouterView /></div>
    <aside class="user-context" aria-label="社区信息">
      <h2>发现正在发生的讨论</h2><p>通过帖子与人建立连接。</p>
      <RouterLink to="/search">搜索帖子和用户 →</RouterLink>
      <p class="muted">GoPulse · 社区</p>
    </aside>
  </div>
</template>
