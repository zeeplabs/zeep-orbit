import { test, expect } from '@playwright/test'
import { MODE_STORAGE_KEY, readMode, writeMode, resetModeMemory, resolveLanding } from '../src/lib/mode'

/**
 * Pure logic of the interface mode (home-chat HCH-08, HCH-10). No page: the
 * storage is a fake, so these run without touching the dashboard. The real
 * redirect in the browser is covered by the H09 UAT.
 */

function fakeStorage(initial: Record<string, string> = {}) {
  const data = { ...initial }
  return {
    data,
    getItem: (k: string) => (k in data ? data[k] : null),
    setItem: (k: string, v: string) => {
      data[k] = v
    },
  }
}

const throwing = {
  getItem: () => {
    throw new Error('blocked')
  },
  setItem: () => {
    throw new Error('blocked')
  },
}

test.beforeEach(() => resetModeMemory())

test.describe('readMode / writeMode', () => {
  test('absent value is simple', () => {
    expect(readMode(fakeStorage())).toBe('simple')
  })

  test('reads a stored simple and advanced', () => {
    expect(readMode(fakeStorage({ [MODE_STORAGE_KEY]: 'simple' }))).toBe('simple')
    expect(readMode(fakeStorage({ [MODE_STORAGE_KEY]: 'advanced' }))).toBe('advanced')
  })

  test('an unknown stored value falls back to simple', () => {
    expect(readMode(fakeStorage({ [MODE_STORAGE_KEY]: 'weird' }))).toBe('simple')
  })

  test('write persists and read returns it', () => {
    const s = fakeStorage()
    writeMode('advanced', s)
    expect(s.data[MODE_STORAGE_KEY]).toBe('advanced')
    expect(readMode(s)).toBe('advanced')
  })

  test('storage that throws does not throw and keeps the choice for the session', () => {
    expect(readMode(throwing)).toBe('simple')
    writeMode('advanced', throwing)
    expect(readMode(throwing)).toBe('advanced')
  })

  test('no storage at all behaves like a throwing one', () => {
    expect(readMode(null)).toBe('simple')
    writeMode('advanced', null)
    expect(readMode(null)).toBe('advanced')
  })
})

test.describe('resolveLanding matrix', () => {
  const avail = { isPending: false, isError: false, available: true }
  const unavail = { isPending: false, isError: false, available: false }
  const loading = { isPending: true, isError: false, available: undefined }
  const failed = { isPending: false, isError: true, available: undefined }

  test('simple + available lands on the chat home', () => {
    expect(resolveLanding('simple', avail)).toBe('home')
  })
  test('simple + unavailable lands on /apps', () => {
    expect(resolveLanding('simple', unavail)).toBe('apps')
  })
  test('advanced lands on /apps whatever the builder says', () => {
    expect(resolveLanding('advanced', avail)).toBe('apps')
    expect(resolveLanding('advanced', unavail)).toBe('apps')
    expect(resolveLanding('advanced', loading)).toBe('apps')
  })
  test('simple while the status loads waits', () => {
    expect(resolveLanding('simple', loading)).toBe('loading')
  })
  test('simple with a failed status request lands on /apps', () => {
    expect(resolveLanding('simple', failed)).toBe('apps')
  })
})
