import { reactive } from 'vue'
import { userApi } from '../services/api'

// Session-scoped shared overrides keep cards, search and profile controls in sync.
const states = reactive(new Map<string, boolean>())
const pending = reactive(new Set<string>())
export function useFollowing() {
  const key = (viewer: number, target: number) => `${viewer}:${target}`
  return {
    value: (viewer: number, target: number, initial = false) => states.get(key(viewer, target)) ?? initial,
    pending: (viewer: number, target: number) => pending.has(key(viewer, target)),
    async set(viewer: number, target: number, previous: boolean) {
      const id = key(viewer, target)
      if (pending.has(id)) return
      pending.add(id); states.set(id, !previous)
      try { const result = await userApi.follow(target, !previous); states.set(id, result.following) }
      catch (error) { states.set(id, previous); throw error }
      finally { pending.delete(id) }
    },
  }
}
export function clearFollowing() { states.clear(); pending.clear() }
