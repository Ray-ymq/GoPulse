import { ref } from 'vue'
import { api } from '../services/api'
import { ApiError } from '../services/http'
import type { Profile, User } from '../types/api'
export const user = ref<User | null>(null)
export const myProfile = ref<Profile | null>(null)
let initialized = false
let initialization: Promise<void> | null = null
export async function ensureSession() {
  if (initialized) return
  initialization ??= (async () => { try { user.value = (await api.me()).data; initialized = true } catch (error) { if (error instanceof ApiError && error.status === 401) { user.value = null; initialized = true } else throw error } })().finally(() => { initialization = null })
  await initialization
}
export async function refreshProfile() { const current = user.value; if (!current) return; const result = await api.profile(current.username); if (user.value?.id === current.id) myProfile.value = result.data }
export async function signIn(username: string, password: string, register: boolean) { user.value = (await api.login(username, password, register)).data; initialized = true; myProfile.value = null }
export function clearSession() { user.value = null; myProfile.value = null; initialized = true }
export async function signOut() { await api.logout(); clearSession() }
