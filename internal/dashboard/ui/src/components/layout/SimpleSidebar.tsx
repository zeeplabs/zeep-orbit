import { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { useBuilderSessions, type BuilderSession } from '@/lib/api'
import orbitWordmark from '@/assets/images/logo/orbit-transparent.png'

const RECENT_LIMIT = 20

/** Latest sessions first, capped. Pure so the ordering is testable. */
export function recentSessions(sessions: BuilderSession[], limit = RECENT_LIMIT): BuilderSession[] {
  return [...sessions]
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .slice(0, limit)
}

/**
 * Sidebar of the simple shell, expanded variant (260 px): logo, "Novo projeto",
 * "Recentes" and the user footer. The collapsed variant for the builder route
 * arrives with H06.
 */
export function SimpleSidebar({ footer }: { footer: ReactNode }) {
  const { t } = useTranslation()
  const sessions = useBuilderSessions().data ?? []
  const recents = recentSessions(sessions)

  return (
    <aside className="sticky top-0 hidden h-screen w-[260px] flex-col border-r border-[var(--border)] bg-[var(--surface)] px-3.5 py-5 md:flex">
      <Link to="/" className="mb-4 flex flex-col px-2 no-underline">
        <img src={orbitWordmark} alt="Orbit" className="mb-0.5 h-[20px] w-auto self-start object-contain" />
        <span className="text-[12px] text-[var(--text-tertiary)]">{t('app.subtitle')}</span>
      </Link>

      <Link
        to="/"
        className="mb-4 flex items-center justify-center gap-2 rounded-[8px] bg-[var(--primary)] px-3 py-2 text-sm font-semibold text-white no-underline transition-opacity hover:opacity-90"
      >
        <Icon name="add" size={18} />
        {t('nav.simple.newProject')}
      </Link>

      <nav aria-label={t('nav.simple.recents')} className="flex min-h-0 flex-1 flex-col">
        <span className="mb-1 px-3 text-[12px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
          {t('nav.simple.recents')}
        </span>
        <ul className="m-0 flex min-h-0 flex-1 list-none flex-col gap-0.5 overflow-y-auto p-0">
          {recents.map((s) => (
            <li key={s.id}>
              <NavLink
                to={`/builder/${s.id}`}
                className="flex items-center gap-2.5 rounded-[8px] px-3 py-2 text-sm text-[var(--text-secondary)] no-underline transition-colors hover:bg-[var(--hover-surface)] aria-[current=page]:bg-[var(--primary-tint)] aria-[current=page]:font-semibold aria-[current=page]:text-[var(--text-primary)]"
              >
                <Icon name="chat_bubble" size={16} className="shrink-0" />
                <span className="truncate">{s.title || t('nav.simple.untitled')}</span>
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>

      {footer}
    </aside>
  )
}
