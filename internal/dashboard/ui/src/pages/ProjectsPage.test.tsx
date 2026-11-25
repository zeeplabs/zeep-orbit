import { describe, test, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom'

const h = vi.hoisted(() => ({
  sessions: { data: undefined as unknown, isPending: false, isError: false, refetch: vi.fn() },
  apps: { data: undefined as unknown, isPending: false, isError: false, refetch: vi.fn() },
  mutate: vi.fn(),
  isPending: false,
  variables: undefined as unknown,
}))

vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  useBuilderSessions: () => h.sessions,
  useApps: () => h.apps,
  useCreateBuilderSession: () => ({ mutate: h.mutate, isPending: h.isPending, variables: h.variables }),
}))

import ProjectsPage from './ProjectsPage'

function Where() {
  return <output data-testid="where">{useLocation().pathname}</output>
}

function setup() {
  return render(
    <MemoryRouter initialEntries={['/projetos']}>
      <Routes>
        <Route path="/projetos" element={<ProjectsPage />} />
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  )
}

const session = (o: Record<string, unknown>) => ({
  backend_app_id: 'a1',
  published_sha: '',
  frontend_app_id: null,
  updated_at: '2026-03-01T00:00:00Z',
  ...o,
})
const app = (id: string, name: string) => ({ id, name, created_at: '2026-01-01T00:00:00Z' })

beforeEach(() => {
  h.sessions = { data: [], isPending: false, isError: false, refetch: vi.fn() }
  h.apps = { data: [], isPending: false, isError: false, refetch: vi.fn() }
  h.mutate.mockReset()
  h.isPending = false
  h.variables = undefined
})

function seed() {
  h.apps.data = [app('a1', 'clinic'), app('a2', 'shop'), app('a9', 'orphan')]
  h.sessions.data = [
    session({ id: 'p', title: 'Clínica Odonto', backend_app_id: 'a1', published_sha: 'abc', updated_at: '2026-03-01T00:00:00Z' }),
    session({ id: 'b', title: 'Loja', backend_app_id: 'a2', brief_version: 1, milestones_total: 4, milestones_done: 1, updated_at: '2026-02-01T00:00:00Z' }),
  ]
}

const cards = () => screen.getAllByRole('article')

describe('ProjectsPage', () => {
  test('lists sessions and the backend-only card, with status pills', () => {
    seed()
    setup()
    expect(screen.getByRole('heading', { level: 1, name: 'My projects' })).toBeInTheDocument()
    expect(cards()).toHaveLength(3)
    const clinic = screen.getByRole('heading', { name: 'Clínica Odonto' }).closest('article')!
    expect(within(clinic).getByText('Published')).toBeInTheDocument()
    expect(within(clinic).getByText('clinic')).toBeInTheDocument()
    const shop = screen.getByRole('heading', { name: 'Loja' }).closest('article')!
    expect(within(shop).getByText(/In progress/)).toBeInTheDocument()
    expect(within(shop).getByText(/milestone 2 of 4/)).toBeInTheDocument()
    const orphan = screen.getByRole('heading', { name: 'orphan' }).closest('article')!
    expect(within(orphan).getByText('Backend only')).toBeInTheDocument()
  })

  test('a session card links to the chat and to the backend', () => {
    seed()
    setup()
    const clinic = screen.getByRole('heading', { name: 'Clínica Odonto' }).closest('article')!
    expect(within(clinic).getByRole('link', { name: 'Open chat' })).toHaveAttribute('href', '/builder/p')
    expect(within(clinic).getByRole('link', { name: 'Backend' })).toHaveAttribute('href', '/apps/a1')
  })

  test('all milestones done reads as such, not as "milestone 5 of 4"', () => {
    h.apps.data = [app('a1', 'clinic')]
    h.sessions.data = [session({ id: 'x', title: 'Done', brief_version: 1, milestones_total: 4, milestones_done: 4 })]
    setup()
    expect(screen.getByText(/all 4 milestones done/)).toBeInTheDocument()
    expect(screen.queryByText(/milestone 5/)).not.toBeInTheDocument()
  })

  test('the backend-only card opens the backend and creates a frontend session on an existing backend', async () => {
    seed()
    h.mutate.mockImplementation((_input, opts) => opts.onSuccess({ id: 's77' }))
    setup()
    const orphan = screen.getByRole('heading', { name: 'orphan' }).closest('article')!
    expect(within(orphan).getByRole('link', { name: 'Open backend' })).toHaveAttribute('href', '/apps/a9')
    await userEvent.click(within(orphan).getByRole('button', { name: 'Create frontend with AI' }))
    expect(h.mutate.mock.calls[0][0]).toEqual({ backend: { mode: 'existing', app_id: 'a9' }, locale: 'en' })
    expect(screen.getByTestId('where').textContent).toBe('/builder/s77')
  })

  test('while a frontend session is being created its button says so and no other can be pressed', () => {
    seed()
    h.apps.data = [app('a1', 'clinic'), app('a8', 'one'), app('a9', 'two')]
    h.isPending = true
    h.variables = { backend: { mode: 'existing', app_id: 'a9' } }
    setup()
    expect(screen.getByRole('button', { name: 'Creating…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Create frontend with AI' })).toBeDisabled()
  })
})

describe('ProjectsPage filters and search', () => {
  test('filter chips are toggle buttons that narrow the list', async () => {
    seed()
    setup()
    const group = screen.getByRole('group', { name: 'Filter projects' })
    expect(within(group).getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(within(group).getByRole('button', { name: 'Published' }))
    expect(within(group).getByRole('button', { name: 'Published' })).toHaveAttribute('aria-pressed', 'true')
    expect(cards()).toHaveLength(1)
    await userEvent.click(within(group).getByRole('button', { name: 'Backend only' }))
    expect(screen.getByRole('heading', { name: 'orphan' })).toBeInTheDocument()
    await userEvent.click(within(group).getByRole('button', { name: 'In progress' }))
    expect(screen.getByRole('heading', { name: 'Loja' })).toBeInTheDocument()
  })

  test('search ignores case and accents and announces the count', async () => {
    seed()
    setup()
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search projects' }), 'CLINICA')
    expect(cards()).toHaveLength(1)
    expect(screen.getByText('1 project')).toBeInTheDocument()
  })

  test('no match shows the empty state with the New project action', async () => {
    seed()
    setup()
    await userEvent.type(screen.getByRole('searchbox'), 'zzzz')
    expect(screen.getByRole('heading', { name: 'No project matches' })).toBeInTheDocument()
    // the page header action and the empty-state action both lead to the chat
    const links = screen.getAllByRole('link', { name: 'New project' })
    expect(links).toHaveLength(2)
    for (const link of links) expect(link).toHaveAttribute('href', '/')
  })
})

describe('ProjectsPage states', () => {
  test('nothing built yet: the first-run empty state', () => {
    setup()
    expect(screen.getByRole('heading', { name: 'No projects yet' })).toBeInTheDocument()
    // header action and empty-state action both lead to the chat
    for (const link of screen.getAllByRole('link', { name: 'New project' })) expect(link).toHaveAttribute('href', '/')
  })

  test('loading shows placeholders, not an empty state', () => {
    h.sessions.isPending = true
    setup()
    expect(screen.queryByRole('heading', { name: 'No projects yet' })).not.toBeInTheDocument()
    expect(document.querySelector('[aria-busy="true"]')).not.toBeNull()
  })

  test('a load failure shows an alert with a retry that refetches both queries', async () => {
    h.apps.isError = true
    setup()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load your projects.')
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(h.sessions.refetch).toHaveBeenCalledTimes(1)
    expect(h.apps.refetch).toHaveBeenCalledTimes(1)
  })
})

describe('ProjectsPage project with no backend', () => {
  test('its card says so and has no backend link', () => {
    h.apps.data = [app('a1', 'clinic')]
    h.sessions.data = [session({ id: 'site', title: 'Padaria Central', backend_app_id: null, backend_origin: 'none' })]
    setup()
    const card = screen.getByRole('heading', { name: 'Padaria Central' }).closest('article') as HTMLElement
    expect(within(card).getByText('No backend')).toBeInTheDocument()
    expect(within(card).getByRole('link', { name: 'Open chat' })).toHaveAttribute('href', '/builder/site')
    expect(within(card).queryByRole('link', { name: 'Backend' })).not.toBeInTheDocument()
    // The visible backend that no session uses still gets its own card.
    expect(screen.getByRole('heading', { name: 'clinic' })).toBeInTheDocument()
  })
})
