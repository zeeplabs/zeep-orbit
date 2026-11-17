import { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useBuilderSessions, type BuilderSession } from '@/lib/api'
import { badgeTint, projectInitial } from '@/lib/projectBadge'
import orbitWordmark from '@/assets/images/logo/orbit-transparent.png'
import orbitIcon from '@/assets/images/logo/orbit-icon-transparent.png'

const RECENT_LIMIT = 20

/** Latest sessions first, capped. Pure so the ordering is testable. */
export function recentSessions(sessions: BuilderSession[], limit = RECENT_LIMIT): BuilderSession[] {
  return [...sessions]
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .slice(0, limit)
}

/**
 * Sidebar of the simple shell. Expanded (260 px): logo, "Novo projeto",
 * "Recentes" and the user footer. Collapsed (68 px, used on the builder route,
 * HCH-13): the same entries as icons, each with an accessible name and a
 * tooltip (HCH-14). Collapsing is decided by the route, not by a setting
 * (HCH-15).
 */
export function SimpleSidebar({ footer, collapsed = false }: { footer: ReactNode; collapsed?: boolean }) {
  const { t } = useTranslation()
  const sessions = useBuilderSessions().data ?? []
  const recents = recentSessions(sessions)

  if (collapsed) return <CollapsedSidebar recents={recents} footer={footer} />

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

const ICON_LINK =
  'flex h-10 w-10 items-center justify-center rounded-[8px] text-[var(--text-secondary)] no-underline transition-colors hover:bg-[var(--hover-surface)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring aria-[current=page]:bg-[var(--primary-tint)] aria-[current=page]:text-[var(--text-primary)] aria-[current=page]:ring-2 aria-[current=page]:ring-[var(--primary)]'

function IconEntry({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}

function CollapsedSidebar({ recents, footer }: { recents: BuilderSession[]; footer: ReactNode }) {
  const { t } = useTranslation()

  return (
    <TooltipProvider delayDuration={200}>
      <aside className="sticky top-0 hidden h-screen w-[68px] flex-col items-center border-r border-[var(--border)] bg-[var(--surface)] px-2 py-5 md:flex">
        <Link to="/" aria-label="Orbit" className="mb-4 flex h-10 w-10 items-center justify-center">
          <img src={orbitIcon} alt="" className="h-7 w-7 object-contain" />
        </Link>

        <IconEntry label={t('nav.simple.newProject')}>
          <Link
            to="/"
            aria-label={t('nav.simple.newProject')}
            className="mb-4 flex h-10 w-10 items-center justify-center rounded-[8px] bg-[var(--primary)] text-white no-underline transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Icon name="add" size={20} />
          </Link>
        </IconEntry>

        <nav aria-label={t('nav.simple.recents')} className="flex min-h-0 flex-1 flex-col">
          <ul className="m-0 flex min-h-0 flex-1 list-none flex-col gap-1 overflow-y-auto p-0">
            {recents.map((s) => {
              const title = s.title || t('nav.simple.untitled')
              const initial = projectInitial(s.title)
              return (
                <li key={s.id}>
                  <IconEntry label={title}>
                    <NavLink to={`/builder/${s.id}`} aria-label={title} className={ICON_LINK}>
                      {initial ? (
                        <span
                          aria-hidden="true"
                          className={`flex h-8 w-8 items-center justify-center rounded-[8px] text-sm font-semibold text-[var(--text-primary)] ${badgeTint(s.id)}`}
                        >
                          {initial}
                        </span>
                      ) : (
                        <Icon name="chat_bubble" size={18} />
                      )}
                    </NavLink>
                  </IconEntry>
                </li>
              )
            })}
          </ul>
        </nav>

        {footer}
      </aside>
    </TooltipProvider>
  )
}
