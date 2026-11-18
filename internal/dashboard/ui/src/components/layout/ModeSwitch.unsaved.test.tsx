import { describe, test, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, Link, Outlet, RouterProvider, useLocation } from 'react-router-dom'
import { UnsavedChangesBlocker } from '@/components/patterns/UnsavedChangesBlocker'
import { useUnsavedChangesGuard } from '@/lib/useUnsavedChangesGuard'
import { MODE_STORAGE_KEY, readMode, resetModeMemory } from '@/lib/mode'
import { ModeSwitch } from './ModeSwitch'

// The data router builds a Request per navigation, and Node's Request rejects jsdom's
// AbortSignal. A minimal stand-in is enough: these routes have no loaders or actions.
class TestRequest {
  url: string
  method: string
  signal: AbortSignal | undefined
  constructor(url: string | URL, init?: { method?: string; signal?: AbortSignal }) {
    this.url = String(url)
    this.method = init?.method ?? 'GET'
    this.signal = init?.signal
  }
}

function DirtyForm() {
  useUnsavedChangesGuard(true)
  return <p>editing</p>
}

function Where() {
  return <output data-testid="where">{useLocation().pathname}</output>
}

function setup() {
  const router = createMemoryRouter(
    [
      {
        element: (
          <>
            <UnsavedChangesBlocker />
            <ModeSwitch advanced />
            <Link to="/">plain link home</Link>
            <Where />
            <Outlet />
          </>
        ),
        children: [
          { path: '/apps', element: <DirtyForm /> },
          { path: '/', element: <p>home</p> },
        ],
      },
    ],
    { initialEntries: ['/apps'] },
  )
  render(<RouterProvider router={router} />)
}

beforeEach(() => {
  resetModeMemory()
  localStorage.setItem(MODE_STORAGE_KEY, 'advanced')
  vi.stubGlobal('Request', TestRequest)
  return () => vi.unstubAllGlobals()
})

describe('ModeSwitch with unsaved changes', () => {
  test('staying on the page keeps the saved mode', async () => {
    const user = userEvent.setup()
    setup()
    await user.click(screen.getByRole('switch', { name: 'Advanced mode' }))
    expect(await screen.findByText('Leave without saving?')).toBeInTheDocument()
    expect(readMode()).toBe('advanced')

    await user.click(screen.getByRole('button', { name: 'Stay' }))
    expect(readMode()).toBe('advanced')
    expect(screen.getByTestId('where')).toHaveTextContent('/apps')
  })

  test('leaving saves the new mode and navigates', async () => {
    const user = userEvent.setup()
    setup()
    await user.click(screen.getByRole('switch', { name: 'Advanced mode' }))
    await user.click(await screen.findByRole('button', { name: 'Leave page' }))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/'))
    expect(screen.getByText('home')).toBeInTheDocument()
    expect(readMode()).toBe('simple')
  })

  test('a stay does not leak into a later, unrelated leave', async () => {
    const user = userEvent.setup()
    setup()
    await user.click(screen.getByRole('switch', { name: 'Advanced mode' }))
    await user.click(await screen.findByRole('button', { name: 'Stay' }))

    await user.click(screen.getByRole('link', { name: 'plain link home' }))
    await user.click(await screen.findByRole('button', { name: 'Leave page' }))
    await waitFor(() => expect(screen.getByText('home')).toBeInTheDocument())
    expect(readMode()).toBe('advanced')
  })
})
