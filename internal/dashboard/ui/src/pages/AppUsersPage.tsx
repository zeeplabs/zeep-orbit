import { useState } from 'react'
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

function formatDateTime(iso: string | null, lang: string) {
  if (!iso) return '—'
  return new Date(iso).toLocaleString(lang)
}

const providerIcon = (p: string) => (p === 'google' ? 'verified_user' : 'mail')

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

  const handleSearch = () => {
    setDebouncedSearch(search)
    setPage(0)
  }

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
      render: (u) => (
        <div
          className="inline-flex items-center gap-1.5 text-[12px]"
          style={{ color: 'var(--text-secondary)' }}
        >
          <Icon name={providerIcon(u.provider)} size={14} style={{ color: 'var(--text-tertiary)' }} />
          {u.provider}
        </div>
      ),
    },
    {
      key: 'status',
      header: t('appUsers.table.status'),
      render: (u) => (
        <StatusPill
          label={u.active ? t('appUsers.active') : t('appUsers.inactive')}
          tone={u.active ? 'success' : 'danger'}
        />
      ),
    },
    {
      key: 'lastAccess',
      header: t('appUsers.table.lastAccess'),
      render: (u) => (
        <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {formatDateTime(u.last_sign_in_at, i18n.language)}
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

      {/* Toolbar: provider counts + search + refresh */}
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

        <div className="flex flex-1 items-center gap-2" style={{ minWidth: 240 }}>
          <div className="relative max-w-sm flex-1">
            <Input
              type="text"
              placeholder={t('appUsers.search')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSearch()
              }}
              className="h-9 rounded-[10px] border pl-9 pr-9 text-[12.5px]"
            />
            <Icon
              name="search"
              size={15}
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2"
              style={{ color: 'var(--text-tertiary)' }}
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
            size="sm"
            onClick={handleSearch}
            className="h-9 shrink-0 rounded-[8px]"
          >
            {t('appUsers.searchBtn')}
          </Button>
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
          total > pageSize
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
