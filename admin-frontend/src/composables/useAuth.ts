import { nextTick, readonly, ref } from 'vue'
import { authApi } from '../services/api'
import { setForbiddenHandler, setUnauthorizedHandler } from '../services/http'
import type { PublicUser } from '../types/api'
const user = ref<PublicUser | null>(null)
export function useAuth() {
  return { user: readonly(user), async refresh() { user.value = await authApi.me() } }
}
export function bindAuthNavigation(navigate: (path: string) => void = path => window.location.replace(path)) {
  setForbiddenHandler(async () => {
    if (user.value) user.value = { ...user.value, role: 'user' }
    await nextTick() // unmount views, abort reads and erase candidate secrets before leaving
    navigate('/posts')
  })
  setUnauthorizedHandler(async () => {
    user.value = null
    await nextTick()
    navigate('/login?redirect=' + encodeURIComponent(window.location.pathname + window.location.search))
  })
}
