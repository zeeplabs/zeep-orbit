// Key/value storage that never throws.
//
// The preview runs inside a sandboxed iframe with an opaque origin, where
// merely touching `window.localStorage` throws a SecurityError. The same
// happens in some private-browsing modes. This adapter probes the browser
// storage once and silently falls back to an in-memory Map, so the app keeps
// working (the session just won't survive a page reload in that case).

export interface SafeStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

function probeLocalStorage(): Storage | null {
  try {
    const storage = window.localStorage
    const probe = '__orbit_storage_probe__'
    storage.setItem(probe, '1')
    storage.removeItem(probe)
    return storage
  } catch {
    return null
  }
}

export function createSafeStorage(): SafeStorage {
  const memory = new Map<string, string>()
  const persistent = probeLocalStorage()

  return {
    getItem(key) {
      if (persistent) {
        try {
          return persistent.getItem(key)
        } catch {
          // fall through to memory
        }
      }
      return memory.get(key) ?? null
    },
    setItem(key, value) {
      memory.set(key, value)
      if (persistent) {
        try {
          persistent.setItem(key, value)
        } catch {
          // memory copy above keeps the value for this page load
        }
      }
    },
    removeItem(key) {
      memory.delete(key)
      if (persistent) {
        try {
          persistent.removeItem(key)
        } catch {
          // ignore
        }
      }
    },
  }
}
