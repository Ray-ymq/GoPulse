import { readonly, ref } from 'vue'
import { authApi } from '../services/api'
import { ApiError, setUnauthorizedHandler } from '../services/http'
import type { Credentials, PublicUser } from '../types/api'

export type AuthStatus = 'uninitialized' | 'authenticated' | 'anonymous'

const user = ref<PublicUser | null>(null)
const status = ref<AuthStatus>('uninitialized')
let initialization: Promise<void> | null = null

function clear(): void {
  user.value = null
  status.value = 'anonymous'
}

setUnauthorizedHandler(clear)

async function initialize(): Promise<void> {
  if (status.value !== 'uninitialized') return
  if (initialization) return initialization
  initialization = (async () => {
    try {
      user.value = await authApi.me()
      status.value = 'authenticated'
    } catch (error) {
      clear()
      if (!(error instanceof ApiError && error.status === 401)) throw error
    } finally {
      initialization = null
    }
  })()
  return initialization
}

async function register(credentials: Credentials): Promise<void> {
  user.value = await authApi.register(credentials)
  status.value = 'authenticated'
}

async function login(credentials: Credentials): Promise<void> {
  user.value = await authApi.login(credentials)
  status.value = 'authenticated'
}

async function logout(): Promise<void> {
  try {
    await authApi.logout()
  } finally {
    clear()
  }
}

export function useAuth() {
  return {
    user: readonly(user),
    status: readonly(status),
    initialize,
    register,
    login,
    logout,
    clear,
  }
}

export function bindUnauthorizedNavigation(navigate: () => void | Promise<void>): void {
  setUnauthorizedHandler(async () => {
    clear()
    await navigate()
  })
}

export function resetAuthForTests(): void {
  user.value = null
  status.value = 'uninitialized'
  initialization = null
  setUnauthorizedHandler(clear)
}
