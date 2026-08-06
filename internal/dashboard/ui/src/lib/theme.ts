import { useCallback, useEffect, useState } from 'react'

/**
 * Modo de tema (dark/light) do redesign.
 *
 * Persistência: localStorage por ora (fallback documentado na spec). Quando o
 * backend expuser `theme_pref` em `/me` + `PATCH /me/prefs`, a API deste hook
 * não muda — só a fonte da persistência. Ver `.specs/features/dashboard-redesign/design.md`.
 */
export type ThemeMode = 'dark' | 'light'

const STORAGE_KEY = 'zeep.theme'
const DEFAULT_MODE: ThemeMode = 'light'

export function getInitialThemeMode(): ThemeMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light') return stored
  } catch {
    // localStorage indisponível (SSR/modo privado) — cai no default
  }
  return DEFAULT_MODE
}

export function applyThemeMode(mode: ThemeMode) {
  const root = document.documentElement
  root.classList.remove('theme-dark', 'theme-light')
  root.classList.add(`theme-${mode}`)
}

/** Aplica o modo salvo o quanto antes, fora do ciclo de render, pra evitar flash. */
export function bootstrapThemeMode() {
  applyThemeMode(getInitialThemeMode())
}

export function useTheme() {
  const [mode, setMode] = useState<ThemeMode>(getInitialThemeMode)

  useEffect(() => {
    applyThemeMode(mode)
    try {
      localStorage.setItem(STORAGE_KEY, mode)
    } catch {
      // ignore
    }
  }, [mode])

  const toggle = useCallback(() => {
    setMode((m) => (m === 'dark' ? 'light' : 'dark'))
  }, [])

  return { mode, setMode, toggle }
}
