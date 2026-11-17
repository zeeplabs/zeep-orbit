import { useCallback, useState } from 'react'

/**
 * Interface mode: "simple" (chat home, Meus projetos) or "advanced" (the
 * dashboard as it was before the chat home).
 *
 * Persistence: localStorage, per browser, like `theme.ts`. There is no
 * server-side per-user preference store (home-chat spec, assumption 1). The
 * value is read when the hook mounts, not live-synced across tabs.
 */
export type UiMode = 'simple' | 'advanced'

export const MODE_STORAGE_KEY = 'orbit.mode'
export const DEFAULT_MODE: UiMode = 'simple'

type ModeStorage = Pick<Storage, 'getItem' | 'setItem'>

// Used when localStorage throws (private mode, blocked site data): the choice
// still holds for the rest of the session, it just is not persisted.
let memoryMode: UiMode | null = null

function browserStorage(): ModeStorage | null {
  try {
    return localStorage
  } catch {
    return null
  }
}

export function readMode(storage: ModeStorage | null = browserStorage()): UiMode {
  try {
    const stored = storage?.getItem(MODE_STORAGE_KEY)
    if (stored === 'simple' || stored === 'advanced') return stored
  } catch {
    // storage unavailable: fall through to the in-memory value
  }
  return memoryMode ?? DEFAULT_MODE
}

export function writeMode(mode: UiMode, storage: ModeStorage | null = browserStorage()): void {
  memoryMode = mode
  try {
    storage?.setItem(MODE_STORAGE_KEY, mode)
  } catch {
    // keep the in-memory value only
  }
}

/** Test hook: forget the in-memory fallback. */
export function resetModeMemory(): void {
  memoryMode = null
}

export type Landing = 'loading' | 'home' | 'apps'

/**
 * Where `/` lands (HCH-10). The chat home needs the builder; anyone else, and
 * anyone in advanced mode, lands on `/apps`. A failed status request is
 * treated as "builder unavailable" so `/` never dead-ends.
 */
export function resolveLanding(
  mode: UiMode,
  builder: { isPending: boolean; isError: boolean; available: boolean | undefined },
): Landing {
  if (mode === 'advanced') return 'apps'
  if (builder.isPending) return 'loading'
  if (builder.isError || !builder.available) return 'apps'
  return 'home'
}

export function useMode() {
  const [mode, setModeState] = useState<UiMode>(() => readMode())

  const setMode = useCallback((next: UiMode) => {
    writeMode(next)
    setModeState(next)
  }, [])

  return { mode, setMode }
}
