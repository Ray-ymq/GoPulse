import { reactive } from 'vue'
import type { Post } from '../types/api'
import { postApi } from '../services/api'

const states = reactive(new Map<string, boolean>())
const pending = reactive(new Set<string>())
let generation = 0
let revision = 0
const changed = new Map<string, number>()
export function useBookmarks() {
  const key = (viewer: number, post: number) => `${viewer}:${post}`
  return {
    value: (viewer: number, post: number, initial = false) => states.get(key(viewer, post)) ?? initial,
    pending: (viewer: number, post: number) => pending.has(key(viewer, post)),
    async toggle(viewer: number, post: number, previous: boolean) {
      const id = key(viewer, post)
      if (!viewer || pending.has(id)) return
      const session = generation
      pending.add(id); states.set(id, !previous); changed.set(id, ++revision)
      try { await postApi.bookmark(post, !previous) }
      catch (error) { if (session === generation) states.set(id, previous); throw error }
      finally { if (session === generation) pending.delete(id) }
    },
  }
}
export function clearBookmarks() { generation++; states.clear(); pending.clear(); changed.clear() }

// A later server read replaces session overrides; a read started before a local
// mutation (or in a previous login session) must never undo that mutation.
export function bookmarkReadBarrier() {
  const session = generation; const started = revision
  return (records: Post[]) => {
    if (session !== generation) return
    const facts = new Map(records.map(post => [post.id, post.bookmarked_by_me]))
    for (const id of states.keys()) {
      const postId = Number(id.split(':')[1])
      if (facts.has(postId) && !pending.has(id) && (changed.get(id) ?? 0) <= started) states.set(id, facts.get(postId)!)
    }
  }
}
