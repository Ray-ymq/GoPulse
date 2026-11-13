import { createRouter, createWebHistory } from 'vue-router'
import { ensureSession, user } from './composables/session'
import FeedView from './views/FeedView.vue'
import AuthView from './views/AuthView.vue'
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/admin/roles', name: 'admin-roles', component: () => import('./views/AdminRolesView.vue'), meta: { title: '角色管理', superAdmin: true } },
    { path: '/', name: 'discover', component: FeedView, meta: { title: '发现广场' } },
    { path: '/following', name: 'following', component: FeedView, meta: { title: '我的关注' } },
    { path: '/bookmarks', name: 'bookmarks', component: FeedView, meta: { title: '我的收藏' } },
    { path: '/notifications', name: 'notifications', component: () => import('./views/NotificationsView.vue'), meta: { title: '消息通知' } },
    { path: '/me', name: 'me', component: () => import('./views/ProfileView.vue'), meta: { title: '个人主页' } },
    { path: '/users/:username', name: 'profile', component: () => import('./views/ProfileView.vue'), meta: { title: '作者主页' } },
    { path: '/relations/:kind(following|followers)', name: 'relations', component: () => import('./views/RelationsView.vue'), meta: { title: '关注与相遇' } },
    { path: '/posts/:id', name: 'post', component: () => import('./views/PostDetailView.vue'), meta: { title: '帖子与讨论' } },
    { path: '/search', name: 'search', component: () => import('./views/SearchView.vue'), meta: { title: '寻找同频' } },
    { path: '/login', name: 'login', component: AuthView, meta: { title: '登录', public: true, layout: 'auth' } },
    { path: '/register', name: 'register', component: AuthView, meta: { title: '注册', public: true, layout: 'auth' } },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFoundView.vue'), meta: { title: '页面未找到', public: true } },
  ],
  scrollBehavior(to, from, saved) { if (saved) return saved; if (to.path === from.path && to.hash) return { el: to.hash, top: 90 }; return { top: 0 } },
})
router.beforeEach(async to => {
  try { await ensureSession() } catch { if (!to.meta.public) return { name: 'login', query: { redirect: to.fullPath, unavailable: '1' } }; return }
  if (!to.meta.public && !user.value) return { name: 'login', query: { redirect: to.fullPath } }
  if (to.meta.superAdmin && user.value?.role !== 'super_admin') return '/'
  if (user.value && ['login', 'register'].includes(String(to.name))) return '/'
})
router.afterEach(to => { document.title = `${String(to.meta.title || '生活有声')} · GoPulse` })
