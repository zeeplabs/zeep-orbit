import { describe, test, expect, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { BuilderSession } from '@/lib/api'

const sessions = vi.hoisted(() => ({ data: [] as unknown[] }))
vi.mock('@/lib/api', async (orig) => ({
  ...(await orig<typeof import('@/lib/api')>()),
  useBuilderSessions: () => ({ data: sessions.data }),
}))

import { SimpleSidebar, recentSessions } from './SimpleSidebar'

const s = (id: string, updated_at: string, title = id) => ({ id, title, updated_at }) as BuilderSession

describe('recentSessions', () => {
  test('latest first, does not mutate the input', () => {
    const input = [s('a', '2026-01-01T00:00:00Z'), s('c', '2026-03-01T00:00:00Z'), s('b', '2026-02-01T00:00:00Z')]
    expect(recentSessions(input).map((x) => x.id)).toEqual(['c', 'b', 'a'])
    expect(input.map((x) => x.id)).toEqual(['a', 'c', 'b'])
  })

  test('caps at 20 by default', () => {
    const many = Array.from({ length: 25 }, (_, i) => s(`s${i}`, `2026-01-${String(i + 1).padStart(2, '0')}T00:00:00Z`))
    expect(recentSessions(many)).toHaveLength(20)
    expect(recentSessions(many)[0].id).toBe('s24')
  })
})

describe('SimpleSidebar', () => {
  function setup() {
    return render(
      <MemoryRouter initialEntries={['/builder/b']}>
        <SimpleSidebar footer={<div>footer</div>} />
      </MemoryRouter>,
    )
  }

  test('lists recents as links, marks the current one, falls back for an empty title', () => {
    sessions.data = [s('a', '2026-01-01T00:00:00Z', 'Clinic app'), s('b', '2026-02-01T00:00:00Z', '')]
    setup()
    const nav = screen.getByRole('navigation', { name: 'Recent' })
    const clinic = within(nav).getByRole('link', { name: 'Clinic app' })
    expect(clinic).toHaveAttribute('href', '/builder/a')
    const untitled = within(nav).getByRole('link', { name: 'Untitled project' })
    expect(untitled).toHaveAttribute('aria-current', 'page')
  })

  test('has the New project action and the footer', () => {
    sessions.data = []
    setup()
    expect(screen.getByRole('link', { name: 'New project' })).toHaveAttribute('href', '/')
    expect(screen.getByText('footer')).toBeInTheDocument()
  })
})
