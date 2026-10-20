import { readonly, ref } from 'vue'
import { clearBookmarks } from './useBookmarks'
import { clearFollowing } from './useFollowing'
import { clearPostCommandDrafts } from './usePostCommand'
import { authApi } from '../services/api'
import { ApiError, setUnauthorizedHandler } from '../services/http'
import type { Credentials, PublicUser } from '../types/api'

export type AuthStatus = 'uninitialized' | 'authenticated' | 'anonymous' | 'error'

const user = ref<PublicUser | null>(null)
const status = ref<AuthStatus>('uninitialized')
let initialization: Promise<void> | null = null

function clear(): void {
	clearPostCommandDrafts(user.value?.id)
	clearFollowing()
	clearBookmarks()
	user.value = null
	status.value = 'anonymous'
}

function setAuthenticated(next: PublicUser): void {
	if (user.value && user.value.id !== next.id) clearPostCommandDrafts()
	user.value = next
	status.value = 'authenticated'
}

function removeAdminCapability(): void {
  if (user.value?.role === 'super_admin') user.value = { ...user.value, role: 'user' }
}

function markRecoveryError(): void {
	clearPostCommandDrafts(user.value?.id)
	clearFollowing()
	clearBookmarks()
	user.value = null
	status.value = 'error'
}

setUnauthorizedHandler(clear)

async function initialize(): Promise<void> {
  if (status.value === 'authenticated' || status.value === 'anonymous') return
  if (initialization) return initialization
  initialization = (async () => {
    try {
      setAuthenticated(await authApi.me())
    } catch (error) {
      if (error instanceof ApiError && error.status === 401 && error.code === 'authentication_required') {
        clear()
        return
      }
      markRecoveryError()
      throw error
    } finally {
      initialization = null
    }
  })()
  return initialization
}

async function refresh(): Promise<void> {
  try {
    setAuthenticated(await authApi.me())
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) clear()
    else markRecoveryError()
    throw error
  }
}

async function register(credentials: Credentials): Promise<void> {
  setAuthenticated(await authApi.register(credentials))
}

async function login(credentials: Credentials): Promise<void> {
  setAuthenticated(await authApi.login(credentials))
}

async function logout(): Promise<void> {
  await authApi.logout()
  clear()
}

export function useAuth() {
  return {
    user: readonly(user),
    status: readonly(status),
    initialize,
    refresh,
    register,
    login,
    logout,
    clear,
    removeAdminCapability,
  }
}

export function bindUnauthorizedNavigation(navigate: () => void | Promise<void>): void {
  setUnauthorizedHandler(async () => {
    clear()
    await navigate()
  })
}

export function resetAuthForTests(): void {
  clearPostCommandDrafts()
  user.value = null
  status.value = 'uninitialized'
  initialization = null
  setUnauthorizedHandler(clear)
}
