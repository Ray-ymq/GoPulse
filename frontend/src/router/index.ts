import { loginDestination } from '../utils/redirect'
import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import UserAppShell from '../components/UserAppShell.vue'
import RelationsView from '../views/RelationsView.vue'
import ProfileView from '../views/ProfileView.vue'
import AuthRecoveryView from '../views/AuthRecoveryView.vue'
import DevStatusView from '../views/DevStatusView.vue'
import ForbiddenView from '../views/ForbiddenView.vue'
import LoginView from '../views/LoginView.vue'
import NewPostView from '../views/NewPostView.vue'
import NotificationsView from '../views/NotificationsView.vue'
import PostDetailView from '../views/PostDetailView.vue'
import BookmarksView from '../views/BookmarksView.vue'
import PostsView from '../views/PostsView.vue'
import RegisterView from '../views/RegisterView.vue'
import SearchView from '../views/SearchView.vue'

export function createAppRouter(history = createWebHistory()): Router {
  const router = createRouter({
    history,
    routes: [
      { path: '/', redirect: '/login' },
      { path: '/register', component: RegisterView, meta: { guestOnly: true } },
      { path: '/login', component: LoginView, meta: { guestOnly: true } },
      { path: '/auth-recovery', component: AuthRecoveryView, meta: { skipAuthRecovery: true } },
      {
        path: '', component: UserAppShell, meta: { requiresAuth: true },
        children: [
          { path: '/posts', component: PostsView },
          { path: '/bookmarks', component: BookmarksView },
          { path: '/search', component: SearchView },
          { path: '/notifications', component: NotificationsView },
          { path: '/me/following', component: RelationsView },
          { path: '/me/followers', component: RelationsView },
          { path: '/posts/new', component: NewPostView },
          { path: '/posts/:postId/edit', component: NewPostView },
          { path: '/posts/:postId', component: PostDetailView },
          { path: '/users/:username', component: ProfileView },
        ],
      },
      { path: '/forbidden', component: ForbiddenView, meta: { requiresAuth: true } },
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
    if (to.meta.guestOnly && auth.status.value === 'authenticated') {
      const target = loginDestination(to.query.redirect, auth.user.value?.role)
      if (target.startsWith('/admin/')) { window.location.assign(target); return false }
      return target
    }
    return true
  })
  return router
}

export const router = createAppRouter()
