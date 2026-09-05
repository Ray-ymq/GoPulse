import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import AdminLayout from '../components/AdminLayout.vue'
import AuthRecoveryView from '../views/AuthRecoveryView.vue'
import DevStatusView from '../views/DevStatusView.vue'
import ForbiddenView from '../views/ForbiddenView.vue'
import LoginView from '../views/LoginView.vue'
import NewPostView from '../views/NewPostView.vue'
import NotificationsView from '../views/NotificationsView.vue'
import ObservabilityEventsView from '../views/ObservabilityEventsView.vue'
import ObservabilityExportersView from '../views/ObservabilityExportersView.vue'
import ObservabilityOverviewView from '../views/ObservabilityOverviewView.vue'
import ObservabilityLogsView from '../views/ObservabilityLogsView.vue'
import ObservabilityMetricsView from '../views/ObservabilityMetricsView.vue'
import PostDetailView from '../views/PostDetailView.vue'
import PostsView from '../views/PostsView.vue'
import RegisterView from '../views/RegisterView.vue'
import SearchView from '../views/SearchView.vue'

export function createAppRouter(history = createWebHistory()): Router {
  const router = createRouter({
    history,
    routes: [
      { path: '/', redirect: '/posts' },
      { path: '/register', component: RegisterView, meta: { guestOnly: true } },
      { path: '/login', component: LoginView, meta: { guestOnly: true } },
      { path: '/auth-recovery', component: AuthRecoveryView, meta: { skipAuthRecovery: true } },
      { path: '/posts', component: PostsView, meta: { requiresAuth: true } },
      { path: '/search', component: SearchView, meta: { requiresAuth: true } },
      { path: '/notifications', component: NotificationsView, meta: { requiresAuth: true } },
      { path: '/posts/new', component: NewPostView, meta: { requiresAuth: true } },
      { path: '/posts/:postId', component: PostDetailView, meta: { requiresAuth: true } },
      { path: '/forbidden', component: ForbiddenView, meta: { requiresAuth: true } },
      {
        path: '/admin/observability',
        component: AdminLayout,
        meta: { requiresAuth: true, requiresAdmin: true },
        children: [
          { path: '', component: ObservabilityOverviewView },
          { path: 'metrics', component: ObservabilityMetricsView },
          { path: 'logs', component: ObservabilityLogsView },
          { path: 'events', component: ObservabilityEventsView },
          { path: 'exporters', component: ObservabilityExportersView },
          { path: ':pathMatch(.*)*', redirect: '/forbidden' },
        ],
      },
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
    if (to.meta.requiresAuth && auth.status.value !== 'authenticated') return { path: '/login', query: { redirect: to.fullPath } }
    if (to.meta.requiresAdmin) {
      try { await auth.refresh() } catch {
        if (auth.status.value !== 'authenticated') return '/login'
        return { path: '/auth-recovery', query: { redirect: to.fullPath } }
      }
      if (auth.user.value?.role !== 'admin') return '/forbidden'
    }
    if (to.meta.guestOnly && auth.status.value === 'authenticated') return '/posts'
    return true
  })
  return router
}

export const router = createAppRouter()
