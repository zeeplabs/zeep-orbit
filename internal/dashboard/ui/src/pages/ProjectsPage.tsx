import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { Skeleton } from '@/components/ui/skeleton'
import { useApps, useBuilderSessions, useCreateBuilderSession } from '@/lib/api'
import {
  buildProjectCards,
  filterProjects,
  type ProjectCard,
  type ProjectFilter,
  type ProjectStatus,
} from '@/lib/projects'
import { cn } from '@/lib/utils'

const FILTERS: { value: ProjectFilter; labelKey: string }[] = [
  { value: 'all', labelKey: 'projects.filter.all' },
  { value: 'building', labelKey: 'projects.filter.building' },
  { value: 'published', labelKey: 'projects.filter.published' },
  { value: 'backend-only', labelKey: 'projects.filter.backendOnly' },
]

const STATUS_KEY: Record<ProjectStatus, string> = {
  published: 'projects.status.published',
  building: 'projects.status.building',
  idle: 'projects.status.idle',
  'backend-only': 'projects.status.backendOnly',
}

const STATUS_STYLE: Record<ProjectStatus, string> = {
  published: 'bg-[var(--success-tint)] text-[var(--success-text)]',
  building: 'bg-[var(--warning-tint)] text-[var(--warning-text)]',
  idle: 'bg-[var(--sunken)] text-[var(--text-secondary)]',
  'backend-only': 'bg-[var(--primary-tint)] text-[var(--primary-text)]',
}

const ACTION_CLASS =
  'inline-flex items-center justify-center rounded-[8px] border border-[var(--border-strong)] bg-transparent px-3 py-1.5 text-[13px] font-medium text-[var(--text-primary)] no-underline transition-colors hover:bg-[var(--hover-surface)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50'

function StatusPill({ card }: { card: ProjectCard }) {
  const { t } = useTranslation()
  const m = card.milestone
  let detail = ''
  if (card.status === 'building' && m) {
    detail =
      m.done >= m.total
        ? t('projects.milestonesDone', { total: m.total })
        : t('projects.milestone', { current: m.done + 1, total: m.total })
  }
  return (
    <span className={cn('inline-flex w-fit items-center rounded-full px-2.5 py-0.5 text-[12px] font-semibold', STATUS_STYLE[card.status])}>
      {t(STATUS_KEY[card.status])}
      {detail && <span className="ml-1 font-normal">· {detail}</span>}
    </span>
  )
}

function ProjectCardView({
  card,
  busy,
  creating,
  onCreateFrontend,
}: {
  card: ProjectCard
  /** Some card is creating a session: every create button waits. */
  busy: boolean
  /** This card is the one creating it. */
  creating: boolean
  onCreateFrontend: (appId: string) => void
}) {
  const { t, i18n } = useTranslation()
  const date = new Date(card.updatedAt).toLocaleDateString(i18n.language, { dateStyle: 'medium' })
  return (
    <li className="min-w-0">
      <article className="flex h-full flex-col gap-3 rounded-[12px] border border-[var(--border)] bg-[var(--surface)] p-4">
        <div className="flex min-w-0 flex-col gap-1.5">
          <h3 className="m-0 truncate text-[15px] font-semibold text-[var(--text-primary)]" title={card.title}>
            {card.title || t('nav.simple.untitled')}
          </h3>
          <StatusPill card={card} />
        </div>
        <div className="flex min-w-0 flex-col gap-0.5 text-[12px] text-[var(--text-tertiary)]">
          {card.backendName && (
            <span className="truncate">
              {t('projects.backendLabel')}: <span className="font-mono text-[var(--text-secondary)]">{card.backendName}</span>
            </span>
          )}
          <span>{t('projects.updatedAt', { date })}</span>
        </div>
        <div className="mt-auto flex flex-wrap gap-2">
          {card.kind === 'session' ? (
            <>
              <Link to={`/builder/${card.sessionId}`} className={ACTION_CLASS}>
                {t('projects.openChat')}
              </Link>
              <Link to={`/apps/${card.backendAppId}`} className={ACTION_CLASS}>
                {t('projects.backend')}
              </Link>
            </>
          ) : (
            <>
              <Link to={`/apps/${card.backendAppId}`} className={ACTION_CLASS}>
                {t('projects.openBackend')}
              </Link>
              <button
                type="button"
                className={ACTION_CLASS}
                disabled={busy}
                onClick={() => onCreateFrontend(card.backendAppId)}
              >
                {creating ? t('projects.creating') : t('projects.createFrontend')}
              </button>
            </>
          )}
        </div>
      </article>
    </li>
  )
}

/** `/projetos`: everything the user built, as cards, with client-side filter and search (HCH-16 to HCH-18). */
export default function ProjectsPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const sessionsQuery = useBuilderSessions()
  const appsQuery = useApps()
  const createSession = useCreateBuilderSession()
  const [filter, setFilter] = useState<ProjectFilter>('all')
  const [query, setQuery] = useState('')

  const loading = sessionsQuery.isPending || appsQuery.isPending
  const failed = sessionsQuery.isError || appsQuery.isError

  const cards = useMemo(
    () => buildProjectCards(sessionsQuery.data ?? [], appsQuery.data ?? []),
    [sessionsQuery.data, appsQuery.data],
  )
  const visible = useMemo(() => filterProjects(cards, filter, query), [cards, filter, query])

  const vars = createSession.variables?.backend
  const creatingAppId = vars?.mode === 'existing' ? vars.app_id : undefined

  function createFrontend(appId: string) {
    createSession.mutate(
      { backend: { mode: 'existing', app_id: appId }, locale: i18n.language },
      { onSuccess: (session) => navigate(`/builder/${session.id}`) },
    )
  }

  return (
    <div className="mx-auto w-full max-w-[1100px] px-6 py-10 max-md:px-4 max-md:py-6">
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="m-0 text-[28px] font-bold text-[var(--text-primary)]">{t('projects.title')}</h1>
          <p className="mb-0 mt-1 text-[14px] text-[var(--text-secondary)]">{t('projects.subtitle')}</p>
        </div>
        <Link
          to="/"
          className="inline-flex items-center gap-2 rounded-[8px] bg-[var(--primary)] px-4 py-2 text-sm font-semibold text-white no-underline transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <Icon name="add" size={18} />
          {t('projects.new')}
        </Link>
      </div>

      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="min-w-[220px] flex-1">
          <label htmlFor="projects-search" className="sr-only">
            {t('projects.searchLabel')}
          </label>
          <input
            id="projects-search"
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('projects.searchPlaceholder')}
            className="w-full rounded-[10px] border border-[var(--border-strong)] bg-transparent px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </div>
        <div role="group" aria-label={t('projects.filtersLabel')} className="flex flex-wrap gap-2">
          {FILTERS.map((f) => (
            <button
              key={f.value}
              type="button"
              aria-pressed={filter === f.value}
              onClick={() => setFilter(f.value)}
              className="rounded-full border border-[var(--border)] bg-transparent px-3.5 py-1.5 text-[13px] text-[var(--text-secondary)] transition-colors hover:bg-[var(--hover-surface)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring aria-pressed:border-[var(--primary)] aria-pressed:bg-[var(--primary-tint)] aria-pressed:font-semibold aria-pressed:text-[var(--text-primary)]"
            >
              {t(f.labelKey)}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <ul aria-busy="true" className="m-0 grid list-none grid-cols-3 gap-4 p-0 max-lg:grid-cols-2 max-md:grid-cols-1">
          {[0, 1, 2].map((i) => (
            <li key={i}>
              <Skeleton className="h-[148px] w-full rounded-[12px]" />
            </li>
          ))}
        </ul>
      ) : failed ? (
        <div role="alert" className="rounded-[12px] border border-[var(--border)] bg-[var(--surface)] p-6 text-center">
          <p className="m-0 mb-3 text-sm text-[var(--text-secondary)]">{t('projects.loadError')}</p>
          <button
            type="button"
            className={ACTION_CLASS}
            onClick={() => {
              void sessionsQuery.refetch()
              void appsQuery.refetch()
            }}
          >
            {t('projects.retry')}
          </button>
        </div>
      ) : visible.length === 0 ? (
        <div className="rounded-[12px] border border-dashed border-[var(--border-strong)] p-10 text-center">
          <h2 className="m-0 text-[16px] font-semibold text-[var(--text-primary)]">
            {t(cards.length === 0 ? 'projects.emptyTitle' : 'projects.noResultsTitle')}
          </h2>
          <p className="mb-4 mt-1 text-sm text-[var(--text-secondary)]">
            {t(cards.length === 0 ? 'projects.emptyText' : 'projects.noResultsText')}
          </p>
          <Link to="/" className={ACTION_CLASS}>
            {t('projects.new')}
          </Link>
        </div>
      ) : (
        <>
          <p aria-live="polite" className="mb-3 mt-0 text-[13px] text-[var(--text-tertiary)]">
            {t('projects.count', { count: visible.length })}
          </p>
          <ul className="m-0 grid list-none grid-cols-3 gap-4 p-0 max-lg:grid-cols-2 max-md:grid-cols-1">
            {visible.map((card) => (
              <ProjectCardView
                key={card.key}
                card={card}
                busy={createSession.isPending}
                creating={createSession.isPending && creatingAppId === card.backendAppId}
                onCreateFrontend={createFrontend}
              />
            ))}
          </ul>
        </>
      )}
    </div>
  )
}
