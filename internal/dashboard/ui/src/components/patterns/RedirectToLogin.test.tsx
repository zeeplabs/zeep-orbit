import { describe, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route, Outlet, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { safeReturnTo } from '@/lib/returnTo'

vi.mock('@/components/patterns/UnsavedChangesBlocker', () => ({ UnsavedChangesBlocker: () => null }))
vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  useBuilderStatus: () => ({ isPending: false, isError: false, data: { available: true } }),
  useBuilderSessions: () => ({ data: [] }),
}))
vi.mock('@/pages/HomeChatPage', () => ({ default: () => <p>chat home</p> }))
vi.mock('@/components/layout/SimpleShell', () => ({ default: () => <Outlet /> }))

import { loginPath } from './RedirectToLogin'
import { HomeEntry, SimpleLayout, BuilderLayout } from '@/App'

function LoginProbe() {
  const { search } = useLocation()
  return <output data-testid="login">{search}</output>
}

function setup(path: string) {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<HomeEntry user={null} />} />
          <Route element={<SimpleLayout user={null} />}>
            <Route path="/projetos" element={<p>projects</p>} />
          </Route>
          <Route element={<BuilderLayout user={null} />}>
            <Route path="/builder/:sessionId" element={<p>builder</p>} />
          </Route>
          <Route path="/login" element={<LoginProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('loginPath', () => {
  test('carries the current path and query as return_to', () => {
    const path = loginPath('/builder/abc', '?x=1&y=2')
    expect(path).toBe('/login?return_to=%2Fbuilder%2Fabc%3Fx%3D1%26y%3D2')
    expect(safeReturnTo(path.slice('/login'.length))).toBe('/builder/abc?x=1&y=2')
  })

  test('the landing itself needs no return_to', () => {
    expect(loginPath('/', '')).toBe('/login')
  })

  test('a path that would read as another origin is not carried', () => {
    expect(loginPath('//evil.example/x', '')).toBe('/login')
    expect(loginPath('/\\evil.example', '')).toBe('/login')
  })
})

describe('simple screens without a session', () => {
  test('/builder/:id sends to /login and comes back to the same session', () => {
    setup('/builder/s1')
    expect(safeReturnTo(screen.getByTestId('login').textContent ?? '')).toBe('/builder/s1')
  })

  test('/projetos keeps its query string', () => {
    setup('/projetos?filter=published')
    expect(safeReturnTo(screen.getByTestId('login').textContent ?? '')).toBe('/projetos?filter=published')
  })

  test('/ goes to plain /login', () => {
    setup('/')
    expect(screen.getByTestId('login').textContent).toBe('')
  })
})
