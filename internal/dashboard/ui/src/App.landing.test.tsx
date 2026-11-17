import { describe, test, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route, Outlet } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { BuilderUnavailableReason } from '@/lib/api'
import { resetModeMemory, writeMode } from '@/lib/mode'

// The shells and pages are covered on their own; here only the decision to
// render them (HCH-10, HCH-20) matters, so they are stubbed.
vi.mock('@/components/patterns/UnsavedChangesBlocker', () => ({ UnsavedChangesBlocker: () => null }))
vi.mock('@/pages/HomeChatPage', () => ({ default: () => <p>chat home</p> }))
vi.mock('@/components/layout/SimpleShell', () => ({
  default: ({ children, collapsed }: { children?: React.ReactNode; collapsed?: boolean }) => (
    <div data-testid="simple-shell" data-collapsed={String(!!collapsed)}>
      {children ?? <Outlet />}
    </div>
  ),
}))

const status = vi.hoisted(() => ({
  value: { isPending: false, isError: false, data: undefined as undefined | { available: boolean; reason: string } },
}))
vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  useBuilderStatus: () => status.value,
}))

import { HomeEntry, SimpleLayout, BuilderLayout } from './App'

const user = { id: 'u1', email: 'a@b.co', role: 'superadmin' }

function setup(path: string) {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<HomeEntry user={user} />} />
          <Route element={<SimpleLayout user={user} />}>
            <Route path="/projetos" element={<p>projects page</p>} />
          </Route>
          <Route element={<BuilderLayout user={user} />}>
            <Route path="/builder/:sessionId" element={<p>builder page</p>} />
          </Route>
          <Route path="/apps" element={<p>apps page</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const available = { isPending: false, isError: false, data: { available: true, reason: '' } }
const unavailable = (reason: BuilderUnavailableReason) => ({
  isPending: false,
  isError: false,
  data: { available: false, reason },
})

const REASONS: BuilderUnavailableReason[] = [
  'not_configured',
  'disabled',
  'provider_not_configured',
  'model_not_in_catalog',
  'budget_not_configured',
  'builder_unreachable',
]

beforeEach(() => {
  resetModeMemory()
  status.value = available
})

describe.each(REASONS)('builder unavailable (%s)', (reason) => {
  beforeEach(() => {
    status.value = unavailable(reason)
  })

  test('/ goes to /apps and never renders the chat', () => {
    setup('/')
    expect(screen.getByText('apps page')).toBeInTheDocument()
    expect(screen.queryByText('chat home')).not.toBeInTheDocument()
  })

  test('/projetos goes to /apps', () => {
    setup('/projetos')
    expect(screen.getByText('apps page')).toBeInTheDocument()
    expect(screen.queryByText('projects page')).not.toBeInTheDocument()
  })

  test('a builder session opened by link goes to /apps', () => {
    setup('/builder/s1')
    expect(screen.getByText('apps page')).toBeInTheDocument()
    expect(screen.queryByText('builder page')).not.toBeInTheDocument()
  })
})

describe('builder status failing or loading', () => {
  test('a status error sends / and /projetos to /apps', () => {
    status.value = { isPending: false, isError: true, data: undefined }
    setup('/')
    expect(screen.getByText('apps page')).toBeInTheDocument()
    setup('/projetos')
    expect(screen.getAllByText('apps page').length).toBeGreaterThan(1)
    setup('/builder/s1')
    expect(screen.getAllByText('apps page').length).toBeGreaterThan(2)
  })

  test('while loading neither the chat nor a redirect is shown', () => {
    status.value = { isPending: true, isError: false, data: undefined }
    setup('/')
    expect(screen.queryByText('chat home')).not.toBeInTheDocument()
    expect(screen.queryByText('apps page')).not.toBeInTheDocument()
  })
})

describe('builder available', () => {
  test('/ shows the chat in simple mode and /apps in advanced mode', () => {
    setup('/')
    expect(screen.getByText('chat home')).toBeInTheDocument()
  })

  test('advanced mode lands on /apps', () => {
    writeMode('advanced')
    setup('/')
    expect(screen.getByText('apps page')).toBeInTheDocument()
  })

  test('a builder session renders in the collapsed shell', () => {
    setup('/builder/s1')
    expect(screen.getByText('builder page')).toBeInTheDocument()
    expect(screen.getByTestId('simple-shell')).toHaveAttribute('data-collapsed', 'true')
  })

  test('/projetos renders in the expanded shell', () => {
    setup('/projetos')
    expect(screen.getByText('projects page')).toBeInTheDocument()
    expect(screen.getByTestId('simple-shell')).toHaveAttribute('data-collapsed', 'false')
  })
})
