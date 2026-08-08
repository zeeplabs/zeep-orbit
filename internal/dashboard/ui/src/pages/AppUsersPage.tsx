import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  useApp,
  useAppUsers,
  useDeactivateAppUser,
  useActivateAppUser,
  useResetAppUserSessions,
} from '../lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Icon } from '@/components/ui/icon'
import { PageHeader, EmptyState, DataTable, StatusPill } from '@/components/patterns'
import type { Column } from '@/components/patterns'
import { cn } from '@/lib/utils'

interface AppUser {
  id: string
  name: string | null
  email: string
  phone: string | null
  provider: string
  role: string
  active: boolean
  last_sign_in_at: string | null
  created_at: string
  avatar_url: string | null
}

interface ProviderCount {
  provider: string
  count: number
}

function formatDate(iso: string, lang: string) {
  return new Date(iso).toLocaleDateString(lang, {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
  })
}

function formatRelativeTime(iso: string | null, t: (k: string, opts?: Record<string, unknown>) => string): string {
  if (!iso) return '—'
  const diffMs = Date.now() - new Date(iso).getTime()
  const minutes = Math.floor(diffMs / 60000)
  if (minutes < 1) return t('appUsers.justNow')
  if (minutes < 60) return t('appUsers.minutesAgo', { count: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('appUsers.hoursAgo', { count: hours })
  const days = Math.floor(hours / 24)
  return t('appUsers.daysAgo', { count: days })
}

const providerIcon = (p: string) => (p === 'google' ? 'verified_user' : 'mail')

function ProviderCell({ provider }: { provider: string }) {
  const isGoogle = provider === 'google'
  return (
    <div
      className="inline-flex items-center gap-1.5 text-[12px]"
      style={{ color: 'var(--text-secondary)' }}
    >
      {isGoogle ? (
        <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true">
          <path fill="#4285F4" d="M15.68 8.18c0-.58-.05-1.14-.15-1.68H8v3.18h4.3a3.68 3.68 0 0 1-1.6 2.42v2h2.6c1.52-1.4 2.38-3.46 2.38-5.92z" />
          <path fill="#34A853" d="M8 16c2.16 0 3.97-.72 5.3-1.9l-2.6-2c-.72.48-1.64.77-2.7.77-2.08 0-3.84-1.4-4.47-3.3H.85v2.07A8 8 0 0 0 8 16z" />
          <path fill="#FBBC05" d="M3.53 9.57A4.8 4.8 0 0 1 3.28 8c0-.55.1-1.08.25-1.57V4.36H.85A8 8 0 0 0 0 8c0 1.29.31 2.5.85 3.64l2.68-2.07z" />
          <path fill="#EA4335" d="M8 3.18c1.18 0 2.23.4 3.06 1.2l2.3-2.3C11.96.9 10.15.14 8 .14A8 8 0 0 0 .85 4.36l2.68 2.07C4.16 4.53 5.92 3.18 8 3.18z" />
        </svg>
      ) : (
        <Icon name="mail" size={14} style={{ color: 'var(--text-tertiary)' }} />
      )}
      {provider === 'google' ? 'Google' : 'Email'}
    </div>
  )
}

export default function AppUsersPage() {
  const { t, i18n } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const { data: app } = useApp(id || '')
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [page, setPage] = useState(0)
  const pageSize = 50

  const { data, isLoading, isFetching, error, refetch } = useAppUsers(
    id || '',
    debouncedSearch || undefined,
    pageSize,
    page * pageSize,
  )
  const deactivate = useDeactivateAppUser()
  const activate = useActivateAppUser()
  const resetSessions = useResetAppUserSessions()

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(search)
      setPage(0)
    }, 300)
    return () => clearTimeout(timer)
  }, [search])

  const clearSearch = () => {
    setSearch('')
    setDebouncedSearch('')
    setPage(0)
  }

  const users: AppUser[] = (data?.data as AppUser[] | undefined) || []
  const total = data?.total || 0
  const providerCounts: ProviderCount[] = (data?.providerCounts as ProviderCount[] | undefined) || []
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const currentPage = page + 1

  const columns: Column<AppUser>[] = [
    {
      key: 'name',
      header: t('appUsers.table.name'),
      render: (u) => (
        <div className="flex items-center gap-2.5">
          {u.avatar_url ? (
            <img src={u.avatar_url} alt="" className="size-7 rounded-full object-cover" />
          ) : (
            <div
              className="flex size-7 items-center justify-center rounded-full text-[11px] font-bold"
              style={{
                background: 'linear-gradient(135deg, var(--primary), var(--accent))',
                color: '#fff',
              }}
            >
              {(u.name || u.email).charAt(0).toUpperCase()}
            </div>
          )}
          <div className="min-w-0">
            <div className="truncate text-[13px] font-bold">{u.name || '—'}</div>
            <div className="truncate text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
              {u.email}
            </div>
          </div>
        </div>
      ),
    },
    {
      key: 'phone',
      header: t('appUsers.table.phone'),
      render: (u) => (
        <span className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
          {u.phone || '—'}
        </span>
      ),
    },
    {
      key: 'provider',
      header: t('appUsers.table.provider'),
      render: (u) => <ProviderCell provider={u.provider} />,
    },
    {
      key: 'role',
      header: t('appUsers.table.role'),
      render: (u) => (
        <span className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
          {u.role}
        </span>
      ),
    },
    {
      key: 'status',
      header: t('appUsers.table.status'),
      render: (u) => (
        <StatusPill
          label={u.active ? t('appUsers.active') : t('appUsers.inactive')}
          tone={u.active ? 'success' : 'neutral'}
          dot={false}
        />
      ),
    },
    {
      key: 'lastAccess',
      header: t('appUsers.table.lastAccess'),
      render: (u) => (
        <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {formatRelativeTime(u.last_sign_in_at, t)}
        </span>
      ),
    },
    {
      key: 'createdAt',
      header: t('appUsers.table.createdAt'),
      render: (u) => (
        <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {formatDate(u.created_at, i18n.language)}
        </span>
      ),
    },
    {
      key: 'actions',
      header: t('appUsers.table.actions'),
      className: 'text-right',
      render: (u) => {
        const isDeactivating = deactivate.isPending && deactivate.variables?.userId === u.id
        const isActivating = activate.isPending && activate.variables?.userId === u.id
        const isResetting = resetSessions.isPending && resetSessions.variables?.userId === u.id
        return (
          <div className="flex items-center justify-end gap-1">
            {u.active && (
              <Button
                variant="outline"
                size="icon"
                onClick={() => deactivate.mutate({ appId: id!, userId: u.id })}
                disabled={deactivate.isPending}
                title={t('appUsers.deactivateTitle')}
                className="size-7 rounded-[8px]"
                style={{
                  borderColor: 'var(--border)',
                  color: 'var(--text-tertiary)',
                }}
              >
                {isDeactivating ? (
                  <Icon name="progress_activity" size={12} className="animate-spin" />
                ) : (
                  <Icon name="block" size={12} />
                )}
              </Button>
            )}
            {!u.active && (
              <Button
                variant="outline"
                size="icon"
                onClick={() => activate.mutate({ appId: id!, userId: u.id })}
                title={t('appUsers.activateTitle')}
                className="size-7 rounded-[8px]"
                style={{
                  borderColor: 'var(--success)',
                  color: 'var(--success)',
                }}
              >
                {isActivating ? (
                  <Icon name="progress_activity" size={12} className="animate-spin" />
                ) : (
                  <Icon name="check_circle" size={12} />
                )}
              </Button>
            )}
            <Button
              variant="outline"
              size="icon"
              onClick={() => resetSessions.mutate({ appId: id!, userId: u.id })}
              disabled={resetSessions.isPending}
              title={t('appUsers.resetTitle')}
              className="size-7 rounded-[8px]"
              style={{
                borderColor: 'var(--border)',
                color: 'var(--text-tertiary)',
              }}
            >
              {isResetting ? (
                <Icon name="progress_activity" size={12} className="animate-spin" />
              ) : (
                <Icon name="refresh" size={12} />
              )}
            </Button>
          </div>
        )
      },
    },
  ]

  return (
    <>
      <Link
        to={`/apps/${id}`}
        className="mb-5 inline-flex items-center gap-1.5 text-[13px] font-semibold no-underline transition-colors"
        style={{ color: 'var(--text-secondary)' }}
      >
        <Icon name="arrow_back" size={17} />
        {t('appUsers.back')}
      </Link>

      <PageHeader title={t('appUsers.title')} subtitle={t('appUsers.subtitle', { app: app?.name || id })} />

      <div className="mb-5 flex flex-wrap items-center gap-3">
        {providerCounts.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            {providerCounts.map((pc) => (
              <div
                key={pc.provider}
                className="flex items-center gap-2 rounded-full px-3 py-1.5"
                style={{ background: 'var(--bg-sunken)' }}
              >
                <Icon
                  name={providerIcon(pc.provider)}
                  size={14}
                  style={{ color: 'var(--text-tertiary)' }}
                />
                <span className="text-[12px] font-bold" style={{ color: 'var(--text-secondary)' }}>
                  {pc.provider} · {pc.count}
                </span>
              </div>
            ))}
          </div>
        )}

        <div className="relative ml-auto w-[260px]">
          <Input
            type="text"
            placeholder={t('appUsers.search')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-9 rounded-[10px] border pr-8 text-[12.5px]"
          />
          {search && (
            <button
              type="button"
              onClick={clearSearch}
              title={t('appUsers.clearSearch')}
              aria-label={t('appUsers.clearSearch')}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 flex items-center justify-center"
              style={{ color: 'var(--text-tertiary)' }}
            >
              <Icon name="close" size={15} />
            </button>
          )}
        </div>

        <Button
          variant="outline"
          size="icon"
          onClick={() => refetch()}
          disabled={isFetching}
          title={t('appUsers.refresh')}
          className="size-9 shrink-0 rounded-[8px]"
        >
          <Icon
            name="refresh"
            size={15}
            className={cn(isFetching ? 'animate-spin' : undefined)}
          />
        </Button>
      </div>

      <DataTable<AppUser>
        columns={columns}
        rows={users}
        getRowId={(u) => u.id}
        loading={isLoading}
        error={!!error}
        empty={
          debouncedSearch
            ? { title: t('appUsers.emptySearch'), icon: 'search_off' }
            : { title: t('appUsers.empty'), icon: 'group' }
        }
        pagination={
          total > 0
            ? {
                page: currentPage,
                pageCount: totalPages,
                onPageChange: (p) => setPage(p - 1),
                prevLabel: t('appUsers.pagination.prev'),
                nextLabel: t('appUsers.pagination.next'),
                label: t('appUsers.pagination.range', {
                  start: page * pageSize + 1,
                  end: Math.min((page + 1) * pageSize, total),
                  total,
                }),
              }
            : undefined
        }
      />
    </>
  )
}
