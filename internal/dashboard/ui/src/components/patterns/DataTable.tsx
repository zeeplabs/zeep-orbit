import { ReactNode } from 'react'
import { Icon } from '@/components/ui/icon'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorState, LoadingState } from './states'
import { cn } from '@/lib/utils'

export interface Column<T> {
  key: string
  header: ReactNode
  render?: (row: T) => ReactNode
  sortable?: boolean
  align?: 'left' | 'right' | 'center'
  className?: string
  width?: string | number
}

export type SortDir = 'asc' | 'desc'

export interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  getRowId: (row: T) => string
  loading?: boolean
  error?: boolean
  /** Config do estado vazio. */
  empty?: { title: string; description?: ReactNode; icon?: string; action?: { label: string; onClick: () => void } }
  /** Config do estado de erro. */
  errorState?: { title: string; description?: ReactNode; action?: { label: string; onClick: () => void } }
  /** Ordenação controlada. */
  sort?: { key: string; dir: SortDir }
  onSort?: (key: string) => void
  /** Ações por linha (ícones à direita). */
  rowActions?: (row: T) => ReactNode
  onRowClick?: (row: T) => void
  /** Paginação controlada. */
  pagination?: {
    page: number
    pageCount: number
    onPageChange: (page: number) => void
    prevLabel: string
    nextLabel: string
    label?: ReactNode
  }
  className?: string
}

/**
 * Tabela de dados única do dashboard: sort + paginação + row-actions +
 * estados vazio/erro/loading embutidos. Telas NÃO reimplementam `<table>`,
 * estados, nem controles de paginação — compõem este componente.
 */
export function DataTable<T>({
  columns,
  rows,
  getRowId,
  loading,
  error,
  empty,
  errorState,
  sort,
  onSort,
  rowActions,
  onRowClick,
  pagination,
  className,
}: DataTableProps<T>) {
  const alignClass = (a?: Column<T>['align']) =>
    a === 'right' ? 'text-right' : a === 'center' ? 'text-center' : 'text-left'

  return (
    <div
      className={cn(
        'overflow-hidden rounded-2xl border border-[var(--border)] bg-[var(--surface)]',
        className
      )}
    >
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-[var(--border)]">
              {columns.map((col) => {
                const active = sort?.key === col.key
                return (
                  <th
                    key={col.key}
                    style={{ width: col.width }}
                    className={cn(
                      'px-4 py-3 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]',
                      alignClass(col.align),
                      col.sortable && onSort && 'cursor-pointer select-none',
                      col.className
                    )}
                    onClick={() => col.sortable && onSort?.(col.key)}
                  >
                    <span className="inline-flex items-center gap-1">
                      {col.header}
                      {col.sortable && (
                        <Icon
                          name={active ? (sort?.dir === 'asc' ? 'arrow_upward' : 'arrow_downward') : 'unfold_more'}
                          size={14}
                          className={active ? 'text-[var(--text-secondary)]' : 'text-[var(--text-tertiary)] opacity-50'}
                        />
                      )}
                    </span>
                  </th>
                )
              })}
              {rowActions && <th className="w-px px-4 py-3" />}
            </tr>
          </thead>
          {!loading && !error && rows.length > 0 && (
            <tbody>
              {rows.map((row) => (
                <tr
                  key={getRowId(row)}
                  className={cn(
                    'border-b border-[var(--border)] last:border-0 transition-colors',
                    onRowClick && 'cursor-pointer hover:bg-[var(--hover-surface)]'
                  )}
                  onClick={() => onRowClick?.(row)}
                >
                  {columns.map((col) => (
                    <td
                      key={col.key}
                      className={cn('px-4 py-3 text-[var(--text-primary)]', alignClass(col.align), col.className)}
                    >
                      {col.render ? col.render(row) : String((row as Record<string, unknown>)[col.key] ?? '')}
                    </td>
                  ))}
                  {rowActions && (
                    <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-end gap-1">{rowActions(row)}</div>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          )}
        </table>
      </div>

      {loading && <LoadingState rows={6} />}
      {!loading && error && (
        <ErrorState
          title={errorState?.title ?? 'Failed to load'}
          description={errorState?.description}
          action={errorState?.action}
        />
      )}
      {!loading && !error && rows.length === 0 && (
        <EmptyState
          icon={empty?.icon}
          title={empty?.title ?? 'Nothing here yet'}
          description={empty?.description}
          action={empty?.action}
        />
      )}

      {pagination && pagination.pageCount > 1 && (
        <div className="flex items-center justify-between gap-3 border-t border-[var(--border)] px-4 py-3">
          <div className="text-xs text-[var(--text-tertiary)]">{pagination.label}</div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={pagination.page <= 1}
              onClick={() => pagination.onPageChange(pagination.page - 1)}
            >
              <Icon name="chevron_left" size={16} />
              {pagination.prevLabel}
            </Button>
            <span className="text-xs text-[var(--text-secondary)]">
              {pagination.page} / {pagination.pageCount}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={pagination.page >= pagination.pageCount}
              onClick={() => pagination.onPageChange(pagination.page + 1)}
            >
              {pagination.nextLabel}
              <Icon name="chevron_right" size={16} />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
