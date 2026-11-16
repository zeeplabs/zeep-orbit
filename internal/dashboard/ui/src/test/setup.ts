import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'
import '@/lib/i18n' // English by default (no saved language in jsdom)

afterEach(() => {
  cleanup()
  localStorage.clear()
})
