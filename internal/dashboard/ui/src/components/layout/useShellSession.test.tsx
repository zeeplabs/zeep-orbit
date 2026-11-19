import { describe, test, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/pages/ChangePasswordModal', () => ({
  default: ({ open }: { open: boolean }) => (open ? <p>change password modal</p> : null),
}))

const toastError = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: { error: toastError } }))

const authFetch = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api', () => ({ authFetch }))

const setLanguage = vi.hoisted(() => vi.fn())
vi.mock('@/lib/i18n', () => ({ setLanguage }))

import { useShellSession } from './useShellSession'

function Harness({ language }: { language?: string }) {
  const session = useShellSession({ language })
  return (
    <>
      <button onClick={() => session.saveLanguage('pt-BR')}>save language</button>
      <button onClick={session.openChangePassword}>open change password</button>
      <button onClick={session.openLogout}>open logout</button>
      {session.dialogs}
    </>
  )
}

function setup(language?: string) {
  const qc = new QueryClient()
  const view = render(
    <QueryClientProvider client={qc}>
      <Harness language={language} />
    </QueryClientProvider>,
  )
  return { qc, ...view }
}

const originalLocation = window.location

beforeEach(() => {
  vi.clearAllMocks()
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { href: '/dashboard/' },
  })
})

afterEach(() => {
  Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
  vi.unstubAllGlobals()
})

describe('useShellSession', () => {
  test('applies the language saved on the user when it differs from the current one', () => {
    setup('pt-BR')
    expect(setLanguage).toHaveBeenCalledWith('pt-BR')
  })

  test('saveLanguage switches locally and persists it', async () => {
    authFetch.mockResolvedValue({ ok: true })
    setup()
    await userEvent.click(screen.getByText('save language'))
    expect(setLanguage).toHaveBeenCalledWith('pt-BR')
    expect(authFetch).toHaveBeenCalledWith(
      '/dashboard/api/me/language',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ language: 'pt-BR' }) }),
    )
  })

  test('saveLanguage keeps the local switch when persisting fails', async () => {
    authFetch.mockRejectedValue(new Error('network'))
    setup()
    await userEvent.click(screen.getByText('save language'))
    expect(setLanguage).toHaveBeenCalledWith('pt-BR')
    expect(toastError).not.toHaveBeenCalled()
  })

  test('opens the change password modal', async () => {
    setup()
    expect(screen.queryByText('change password modal')).not.toBeInTheDocument()
    await userEvent.click(screen.getByText('open change password'))
    expect(screen.getByText('change password modal')).toBeInTheDocument()
  })

  test('logout posts, clears the cache and goes to the login page', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    const { qc } = setup()
    qc.setQueryData(['x'], 1)
    await userEvent.click(screen.getByText('open logout'))
    await userEvent.click(screen.getByRole('button', { name: 'Log Out' }))
    await waitFor(() => expect(window.location.href).toBe('/dashboard/login'))
    expect(fetchMock).toHaveBeenCalledWith(
      '/dashboard/api/logout',
      expect.objectContaining({ method: 'POST' }),
    )
    expect(qc.getQueryData(['x'])).toBeUndefined()
  })

  test('a failed logout keeps the user on the page and shows the error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
    setup()
    await userEvent.click(screen.getByText('open logout'))
    await userEvent.click(screen.getByRole('button', { name: 'Log Out' }))
    await waitFor(() => expect(toastError).toHaveBeenCalled())
    expect(window.location.href).toBe('/dashboard/')
  })
})
