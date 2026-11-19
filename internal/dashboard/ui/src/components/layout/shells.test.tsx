import { describe, test, expect, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// useBlocker needs a data router; jsdom's AbortSignal breaks data-router
// navigation in these tests, so the blocker (covered on its own) is stubbed.
vi.mock('@/components/patterns/UnsavedChangesBlocker', () => ({ UnsavedChangesBlocker: () => null }))

vi.mock('@/lib/useCurrentRole', () => ({ useCurrentRole: () => 'admin' }))

const builder = vi.hoisted(() => ({ available: true }))
vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  useBuilderSessions: () => ({ data: [] }),
  useBuilderStatus: () => ({ data: { available: builder.available } }),
}))

import SimpleShell from './SimpleShell'
import { Sidebar } from './Sidebar'

const user = { id: 'u1', email: 'a@b.co', name: 'Ana Lima', role: 'admin' }

function wrap(ui: React.ReactNode, path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('SimpleShell', () => {
  function setup(path: string) {
    return wrap(
      <Routes>
        <Route element={<SimpleShell user={user} />}>
          <Route path="/" element={<p>home</p>} />
          <Route path="/projetos" element={<p>projects</p>} />
        </Route>
      </Routes>,
      path,
    )
  }

  test('header has the Chat and My projects links, current one marked', () => {
    setup('/projetos')
    const nav = screen.getByRole('navigation', { name: 'Main navigation' })
    expect(within(nav).getByRole('link', { name: 'Chat' })).not.toHaveAttribute('aria-current')
    expect(within(nav).getByRole('link', { name: 'My projects' })).toHaveAttribute('aria-current', 'page')
  })

  test('Chat is current only on the exact home route', () => {
    setup('/')
    expect(
      within(screen.getByRole('navigation', { name: 'Main navigation' })).getByRole('link', { name: 'Chat' }),
    ).toHaveAttribute('aria-current', 'page')
  })

  test('the switch is off here, the page renders in <main>, the skip link exists', () => {
    setup('/')
    expect(screen.getByRole('switch', { name: 'Advanced mode' })).toHaveAttribute('aria-checked', 'false')
    expect(within(screen.getByRole('main')).getByText('home')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Skip to main content' })).toHaveAttribute('href', '#main-content')
  })

  test('collapsed shell keeps the header and shows the user actions as icon buttons', () => {
    wrap(
      <Routes>
        <Route element={<SimpleShell user={user} collapsed />}>
          <Route path="/builder/:sessionId" element={<p>builder</p>} />
        </Route>
      </Routes>,
      '/builder/x',
    )
    expect(document.querySelector('aside')).toHaveClass('w-[68px]')
    expect(screen.getByRole('button', { name: 'Log Out' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Change Password' })).toBeInTheDocument()
    expect(
      within(screen.getByRole('navigation', { name: 'Main navigation' })).getByRole('link', { name: 'My projects' }),
    ).toBeInTheDocument()
    expect(within(screen.getByRole('main')).getByText('builder')).toBeInTheDocument()
  })

  test('redirects to /login without a user', () => {
    wrap(
      <Routes>
        <Route element={<SimpleShell user={null} />}>
          <Route path="/" element={<p>home</p>} />
        </Route>
        <Route path="/login" element={<p>login page</p>} />
      </Routes>,
      '/',
    )
    expect(screen.getByText('login page')).toBeInTheDocument()
  })
})

describe('SimpleMobileNav', () => {
  function setup(path = '/') {
    return wrap(
      <Routes>
        <Route element={<SimpleShell user={user} />}>
          <Route path="*" element={<p>page</p>} />
        </Route>
      </Routes>,
      path,
    )
  }

  test('bottom bar has Chat, My projects and More, current tab marked', () => {
    setup('/projetos')
    const bar = screen.getByRole('navigation', { name: 'Mobile navigation' })
    expect(within(bar).getByRole('link', { name: 'Chat' })).not.toHaveAttribute('aria-current')
    expect(within(bar).getByRole('link', { name: 'My projects' })).toHaveAttribute('aria-current', 'page')
    expect(within(bar).getByRole('button', { name: 'More' })).toBeInTheDocument()
  })

  test('More opens a sheet with New project and the user actions', async () => {
    const { default: userEvent } = await import('@testing-library/user-event')
    setup('/')
    await userEvent.setup().click(screen.getByRole('button', { name: 'More' }))
    const sheet = await screen.findByRole('dialog')
    expect(within(sheet).getByRole('link', { name: 'New project' })).toHaveAttribute('href', '/')
    expect(within(sheet).getByRole('button', { name: 'Log Out' })).toBeInTheDocument()
    expect(within(sheet).getByRole('button', { name: 'Change Password' })).toBeInTheDocument()
  })
})

describe('advanced Sidebar', () => {
  test('shows the mode switch turned on when the builder is available', () => {
    builder.available = true
    wrap(<Sidebar companyName="Acme" footer={<div>footer</div>} />, '/apps')
    expect(screen.getByRole('switch', { name: 'Advanced mode' })).toHaveAttribute('aria-checked', 'true')
  })

  test('hides the switch when the builder is not available (HCH-20)', () => {
    builder.available = false
    wrap(<Sidebar companyName="Acme" footer={<div>footer</div>} />, '/apps')
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    expect(screen.getByText('footer')).toBeInTheDocument()
  })
})
