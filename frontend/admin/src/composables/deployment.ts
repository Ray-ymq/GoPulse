import { ref } from 'vue'

export const standaloneObservability = ref(false)

export async function loadDeployment(): Promise<void> {
  standaloneObservability.value = false
  try {
    const response = await fetch('/admin/deployment.json', { cache: 'no-store', signal: AbortSignal.timeout(3000) })
    if (!response.ok) return
    const value: unknown = await response.json()
    standaloneObservability.value = typeof value === 'object' && value !== null
      && 'standaloneObservability' in value && value.standaloneObservability === true
  } catch { standaloneObservability.value = false }
}
