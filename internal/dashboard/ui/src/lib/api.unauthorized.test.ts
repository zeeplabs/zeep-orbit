import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest'
import { authFetch, redirectToLoginOnUnauthorized } from './api'

const assign = vi.fn()

function at(pathname: string, search = '') {
  vi.stubGlobal('location', { pathname, search, assign })
}

function answer(status: number, body: string) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(body, { status })))
}

beforeEach(() => {
  vi.useFakeTimers()
  assign.mockReset()
  at('/dashboard/apps/a1', '?tab=users')
})

afterEach(() => {
  // Let the 2 s re-arm window of a previous redirect pass before the next test.
  vi.advanceTimersByTime(2000)
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('redirectToLoginOnUnauthorized', () => {
  test('goes to /login with the current page as return_to', () => {
    redirectToLoginOnUnauthorized()
    expect(assign).toHaveBeenCalledWith('/dashboard/login?return_to=%2Fdashboard%2Fapps%2Fa1%3Ftab%3Dusers')
  })

  test('does nothing on the login page itself', () => {
    at('/dashboard/login', '?return_to=%2Fapps')
    redirectToLoginOnUnauthorized()
    expect(assign).not.toHaveBeenCalled()
  })

  test('a burst of 401s redirects once, and it re-arms after 2 seconds', () => {
    redirectToLoginOnUnauthorized()
    redirectToLoginOnUnauthorized()
    expect(assign).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(2000)
    redirectToLoginOnUnauthorized()
    expect(assign).toHaveBeenCalledTimes(2)
  })
})

describe('authFetch', () => {
  test('a 401 from the auth middleware sends to login and still returns the response', async () => {
    answer(401, JSON.stringify({ error: 'unauthorized' }))
    const res = await authFetch('/dashboard/api/apps')
    expect(res.status).toBe(401)
    expect(assign).toHaveBeenCalledTimes(1)
  })

  test('a 401 with another error (wrong current password) is not a dead session', async () => {
    answer(401, JSON.stringify({ error: 'current password is incorrect' }))
    await authFetch('/dashboard/api/me/password', { method: 'PUT' })
    expect(assign).not.toHaveBeenCalled()
  })

  test('a 401 without a JSON body is not treated as a dead session', async () => {
    answer(401, 'Unauthorized')
    await authFetch('/dashboard/api/apps')
    expect(assign).not.toHaveBeenCalled()
  })

  test('other statuses never redirect', async () => {
    answer(403, JSON.stringify({ error: 'unauthorized' }))
    await authFetch('/dashboard/api/apps')
    expect(assign).not.toHaveBeenCalled()
  })

  test('the caller can still read the body after the check', async () => {
    answer(401, JSON.stringify({ error: 'unauthorized' }))
    const res = await authFetch('/dashboard/api/apps')
    await expect(res.json()).resolves.toEqual({ error: 'unauthorized' })
  })
})
