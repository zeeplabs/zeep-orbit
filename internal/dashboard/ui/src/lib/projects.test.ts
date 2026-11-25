import { describe, test, expect } from 'vitest'
import type { AppDef, BuilderSession } from './api'
import { buildProjectCards, filterProjects, sessionStatus } from './projects'

const session = (o: Partial<BuilderSession> & { id: string }): BuilderSession =>
  ({
    backend_app_id: 'a1',
    title: o.id,
    published_sha: '',
    frontend_app_id: null,
    updated_at: '2026-01-01T00:00:00Z',
    ...o,
  }) as BuilderSession

const app = (id: string, name: string, created_at = '2026-01-01T00:00:00Z') => ({ id, name, created_at }) as AppDef

describe('sessionStatus', () => {
  test('published by sha or by a frontend app, even with a brief in progress', () => {
    expect(sessionStatus(session({ id: 's', published_sha: 'abc' }))).toBe('published')
    expect(sessionStatus(session({ id: 's', frontend_app_id: 'f1' }))).toBe('published')
    expect(sessionStatus(session({ id: 's', published_sha: 'abc', brief_version: 1, milestones_total: 3 }))).toBe('published')
  })
  test('building when a brief with milestones exists', () => {
    expect(sessionStatus(session({ id: 's', brief_version: 1, milestones_total: 4, milestones_done: 1 }))).toBe('building')
  })
  test('idle with no brief, a zero brief or missing fields', () => {
    expect(sessionStatus(session({ id: 's' }))).toBe('idle')
    expect(sessionStatus(session({ id: 's', brief_version: 0, milestones_total: 0 }))).toBe('idle')
    expect(sessionStatus(session({ id: 's', brief_version: 1, milestones_total: 0 }))).toBe('idle')
  })
})

describe('buildProjectCards', () => {
  const apps = [app('a1', 'clinic'), app('a2', 'shop'), app('a3', 'blog')]

  test('a session with no backend has a null backend id and no name, and uses up no backend', () => {
    const cards = buildProjectCards([session({ id: 'site', backend_app_id: null, backend_origin: 'none' })], apps)
    expect(cards[0]).toMatchObject({ kind: 'session', sessionId: 'site', backendAppId: null, backendName: '' })
    // every visible backend still shows as backend-only
    expect(cards.filter((c) => c.kind === 'backend-only').map((c) => c.backendAppId)).toEqual(['a3', 'a1', 'a2'])
  })

  test('sessions newest first, then backends no session uses, by name', () => {
    const cards = buildProjectCards(
      [
        session({ id: 'old', backend_app_id: 'a1', updated_at: '2026-01-01T00:00:00Z' }),
        session({ id: 'new', backend_app_id: 'a2', updated_at: '2026-03-01T00:00:00Z' }),
      ],
      apps,
    )
    expect(cards.map((c) => c.key)).toEqual(['session:new', 'session:old', 'app:a3'])
    expect(cards[2]).toMatchObject({ kind: 'backend-only', status: 'backend-only', title: 'blog', backendAppId: 'a3' })
  })

  test('a session joins its backend name; a backend that is not visible leaves it empty', () => {
    const cards = buildProjectCards(
      [session({ id: 's1', backend_app_id: 'a1' }), session({ id: 's2', backend_app_id: 'gone' })],
      apps,
    )
    expect(cards.find((c) => c.sessionId === 's1')?.backendName).toBe('clinic')
    expect(cards.find((c) => c.sessionId === 's2')?.backendName).toBe('')
  })

  test('a backend used by two sessions is not listed again as backend-only', () => {
    const cards = buildProjectCards(
      [session({ id: 's1', backend_app_id: 'a1' }), session({ id: 's2', backend_app_id: 'a1' })],
      [app('a1', 'clinic')],
    )
    expect(cards.filter((c) => c.kind === 'backend-only')).toHaveLength(0)
    expect(cards).toHaveLength(2)
  })

  test('a building session carries its milestone counts; others do not', () => {
    const cards = buildProjectCards(
      [
        session({ id: 'b', backend_app_id: 'a1', brief_version: 1, milestones_total: 4, milestones_done: 1 }),
        session({ id: 'i', backend_app_id: 'a2' }),
      ],
      apps,
    )
    expect(cards.find((c) => c.sessionId === 'b')?.milestone).toEqual({ done: 1, total: 4 })
    expect(cards.find((c) => c.sessionId === 'i')?.milestone).toBeUndefined()
  })

  test('nothing at all gives no cards; sessions without apps still list', () => {
    expect(buildProjectCards([], [])).toEqual([])
    expect(buildProjectCards([session({ id: 's' })], [])).toHaveLength(1)
  })
})

describe('filterProjects', () => {
  const cards = buildProjectCards(
    [
      session({ id: 'p', title: 'Clínica Odonto', backend_app_id: 'a1', published_sha: 'x', updated_at: '2026-03-01T00:00:00Z' }),
      session({ id: 'b', title: 'Loja', backend_app_id: 'a2', brief_version: 1, milestones_total: 2, updated_at: '2026-02-01T00:00:00Z' }),
      session({ id: 'i', title: 'Rascunho', backend_app_id: 'a3', updated_at: '2026-01-01T00:00:00Z' }),
    ],
    [app('a1', 'clinic'), app('a2', 'shop'), app('a3', 'draft'), app('a4', 'orphan')],
  )

  test('all keeps everything, each filter keeps only its status', () => {
    expect(filterProjects(cards, 'all', '')).toHaveLength(4)
    expect(filterProjects(cards, 'published', '').map((c) => c.sessionId)).toEqual(['p'])
    expect(filterProjects(cards, 'building', '').map((c) => c.sessionId)).toEqual(['b'])
    expect(filterProjects(cards, 'backend-only', '').map((c) => c.title)).toEqual(['orphan'])
  })

  test('an idle session shows under all only', () => {
    for (const f of ['published', 'building', 'backend-only'] as const) {
      expect(filterProjects(cards, f, '').some((c) => c.sessionId === 'i')).toBe(false)
    }
    expect(filterProjects(cards, 'all', '').some((c) => c.sessionId === 'i')).toBe(true)
  })

  test('search matches title or backend name, ignoring case and accents', () => {
    expect(filterProjects(cards, 'all', 'clinica').map((c) => c.sessionId)).toEqual(['p'])
    expect(filterProjects(cards, 'all', 'SHOP').map((c) => c.sessionId)).toEqual(['b'])
    expect(filterProjects(cards, 'all', '  loja ').map((c) => c.sessionId)).toEqual(['b'])
  })

  test('search and filter combine; no match gives an empty list', () => {
    expect(filterProjects(cards, 'building', 'clinica')).toEqual([])
    expect(filterProjects(cards, 'all', 'zzz')).toEqual([])
  })
})
