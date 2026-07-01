import { OrbitClient } from '@zeeptech/orbit-client'
import type { OrbitConfig } from './types'

const STORAGE_KEY = 'zeep-orbit-todo-config'

export function saveConfig(config: OrbitConfig) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(config))
}

export function loadConfig(): OrbitConfig | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

export function clearConfig() {
  localStorage.removeItem(STORAGE_KEY)
}

export function createClient(config: OrbitConfig): OrbitClient {
  return new OrbitClient({
    baseURL: config.baseURL,
    app: config.app,
    jwt: config.jwt ?? '',
  })
}
