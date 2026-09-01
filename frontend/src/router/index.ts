import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import AuthRecoveryView from '../views/AuthRecoveryView.vue'
import DevStatusView from '../views/DevStatusView.vue'
import LoginView from '../views/LoginView.vue'
import NewPostView from '../views/NewPostView.vue'
import PostDetailView from '../views/PostDetailView.vue'
import PostsView from '../views/PostsView.vue'
import RegisterView from '../views/RegisterView.vue'

export function createAppRouter(history = createWebHistory()): Router {
  const router = createRouter({
    history,
    routes: [
      { path: '/', redirect: '/posts' },
      { path: '/register', component: RegisterView, meta: { guestOnly: true } },
      { path: '/login', component: LoginView, meta: { guestOnly: true } },
      { path: '/auth-recovery', component: AuthRecoveryView, meta: { skipAuthRecovery: true } },
      { path: '/posts', component: PostsView, meta: { requiresAuth: true } },
      { path: '/posts/new', component: NewPostView, meta: { requiresAuth: true } },
      { path: '/posts/:postId', component: PostDetailView, meta: { requiresAuth: true } },
      { path: '/dev/status', component: DevStatusView, meta: { skipAuthRecovery: true } },
      { path: '/:pathMatch(.*)*', redirect: '/posts' },
    ],
  })

  router.beforeEach(async (to) => {
    const auth = useAuth()
    if (!to.meta.skipAuthRecovery) {
      try {
        await auth.initialize()
      } catch {
        return { path: '/auth-recovery', query: { redirect: to.fullPath } }
      }
    }
    if (to.meta.requiresAuth && auth.status.value !== 'authenticated') return '/login'
    if (to.meta.guestOnly && auth.status.value === 'authenticated') return '/posts'
    return true
  })
  return router
}

export const router = createAppRouter()
