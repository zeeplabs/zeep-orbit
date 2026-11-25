import type { AppDef, BuilderSession } from './api'

/**
 * A "project" on the Meus projetos screen: a view model over the builder sessions the user
 * owns and the backend apps they can see, not a stored entity (home-chat design, C5).
 */
export type ProjectStatus = 'building' | 'published' | 'idle' | 'backend-only'

export interface ProjectCard {
  kind: 'session' | 'backend-only'
  /** Stable key for lists. */
  key: string
  sessionId?: string
  title: string
  /** null for a project with no backend. */
  backendAppId: string | null
  /** Empty when the backend is not in the visible apps list, or there is none. */
  backendName: string
  status: ProjectStatus
  milestone?: { done: number; total: number }
  /** ISO timestamp; the session's last update, or the app's creation for a backend-only card. */
  updatedAt: string
}

export type ProjectFilter = 'all' | 'building' | 'published' | 'backend-only'

/** HCH-17: published wins over a brief in progress; a session with neither is idle. */
export function sessionStatus(s: BuilderSession): ProjectStatus {
  if (s.published_sha !== '' || s.frontend_app_id) return 'published'
  if ((s.brief_version ?? 0) > 0 && (s.milestones_total ?? 0) > 0) return 'building'
  return 'idle'
}

/**
 * Sessions first (latest update first), then the backends no session of the user uses
 * (by name). `apps` are the backend apps the user can see.
 */
export function buildProjectCards(sessions: BuilderSession[], apps: AppDef[]): ProjectCard[] {
  const appById = new Map(apps.map((a) => [a.id, a]))
  const used = new Set(sessions.map((s) => s.backend_app_id).filter((id): id is string => id !== null))

  const fromSessions: ProjectCard[] = [...sessions]
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .map((s) => {
      const status = sessionStatus(s)
      return {
        kind: 'session' as const,
        key: `session:${s.id}`,
        sessionId: s.id,
        title: s.title,
        backendAppId: s.backend_app_id,
        backendName: (s.backend_app_id && appById.get(s.backend_app_id)?.name) || '',
        status,
        ...(status === 'building'
          ? { milestone: { done: s.milestones_done ?? 0, total: s.milestones_total ?? 0 } }
          : {}),
        updatedAt: s.updated_at,
      }
    })

  const backendOnly: ProjectCard[] = apps
    .filter((a) => !used.has(a.id))
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((a) => ({
      kind: 'backend-only' as const,
      key: `app:${a.id}`,
      title: a.name,
      backendAppId: a.id,
      backendName: a.name,
      status: 'backend-only' as const,
      updatedAt: a.created_at,
    }))

  return [...fromSessions, ...backendOnly]
}

function normalize(s: string): string {
  return s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase()
}

/** HCH-18: client-side filter and search (title and backend name, accent- and case-insensitive). */
export function filterProjects(cards: ProjectCard[], filter: ProjectFilter, query: string): ProjectCard[] {
  const q = normalize(query.trim())
  return cards.filter((c) => {
    if (filter !== 'all' && c.status !== filter) return false
    if (!q) return true
    return normalize(c.title).includes(q) || normalize(c.backendName).includes(q)
  })
}
