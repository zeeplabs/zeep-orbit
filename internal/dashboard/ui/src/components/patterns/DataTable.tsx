import { Fragment, KeyboardEvent, ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
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
  empty?: { title: string; description?: ReactNode; icon?: string; action?: { label: string; onClick: () => void } }
  errorState?: { title: string; description?: ReactNode; action?: { label: string; onClick: () => void } }
  sort?: { key: string; dir: SortDir }
  onSort?: (key: string) => void
  /** Ações por linha (ícones à direita). */
  rowActions?: (row: T) => ReactNode
  /** Linha clicável. Também acionável por teclado (Enter/Espaço com a linha focada). */
  onRowClick?: (row: T) => void
  /**
   * Detalhe expansível por linha. Quando definido, cada linha ganha um botão chevron
   * (`aria-expanded`, acionável por teclado) e o click na linha também alterna a
   * expansão (controlada via `expandedRowId`/`onToggleExpand`).
   * Substitui o click simples (`onRowClick` é ignorado enquanto expansível).
   */
  renderExpanded?: (row: T) => ReactNode
  expandedRowId?: string | null
  onToggleExpand?: (id: string) => void
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
 *
 * Teclado: cabeçalhos ordenáveis são `<button>` com `aria-sort` no `<th>`; a
 * expansão usa um botão com `aria-expanded`; `onRowClick` torna a linha focável.
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
  renderExpanded,
  expandedRowId,
  onToggleExpand,
  pagination,
  className,
}: DataTableProps<T>) {
  const { t } = useTranslation()
  const alignClass = (a?: Column<T>['align']) =>
    a === 'right' ? 'text-right' : a === 'center' ? 'text-center' : 'text-left'
  const justifyClass = (a?: Column<T>['align']) =>
    a === 'right' ? 'justify-end' : a === 'center' ? 'justify-center' : 'justify-start'
  const expandable = Boolean(renderExpanded)
  const rowClickable = !expandable && Boolean(onRowClick)
  const colCount = columns.length + (expandable ? 1 : 0) + (rowActions ? 1 : 0)

  // Só reage ao teclado quando o foco está na própria linha — botões/links
  // dentro da linha mantêm o Enter/Espaço deles.
  const onRowKeyDown = (e: KeyboardEvent<HTMLTableRowElement>, row: T) => {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onRowClick?.(row)
    }
  }

  return (
    <div className={className}>
      <div
        className="overflow-hidden rounded-[14px] border border-[var(--border)] bg-[var(--surface)]"
        aria-busy={loading || undefined}
      >
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-[var(--border)]">
              {expandable && <th className="w-8 px-2 py-3" />}
              {columns.map((col) => {
                const active = sort?.key === col.key
                const interactive = Boolean(col.sortable && onSort)
                const content = (
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
                )
                return (
                  <th
                    key={col.key}
                    scope="col"
                    style={{ width: col.width }}
                    aria-sort={
                      interactive ? (active ? (sort?.dir === 'asc' ? 'ascending' : 'descending') : 'none') : undefined
                    }
                    className={cn(
                      'text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]',
                      !interactive && 'px-4 py-3',
                      alignClass(col.align),
                      col.className
                    )}
                  >
                    {interactive ? (
                      // O botão ocupa a célula toda (área de clique igual à do th antigo).
                      // Preflight zera text-transform em <button>, daí o `uppercase` explícito.
                      <button
                        type="button"
                        onClick={() => onSort?.(col.key)}
                        className={cn(
                          'flex w-full select-none items-center px-4 py-3 uppercase tracking-wide transition-colors hover:text-[var(--text-secondary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring',
                          justifyClass(col.align)
                        )}
                      >
                        {content}
                      </button>
                    ) : (
                      content
                    )}
                  </th>
                )
              })}
              {rowActions && <th className="w-px px-4 py-3" />}
            </tr>
          </thead>
          {!loading && !error && rows.length > 0 && (
            <tbody>
              {rows.map((row) => {
                const id = getRowId(row)
                const expanded = expandable && expandedRowId === id
                const clickable = expandable || rowClickable
                return (
                  <Fragment key={id}>
                    <tr
                      tabIndex={rowClickable ? 0 : undefined}
                      onKeyDown={rowClickable ? (e) => onRowKeyDown(e, row) : undefined}
                      className={cn(
                        'border-b border-[var(--border)] last:border-0 transition-colors',
                        clickable && 'cursor-pointer hover:bg-[var(--hover-surface)]',
                        rowClickable &&
                          'focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary',
                        expanded && 'bg-[var(--hover-surface)]'
                      )}
                      onClick={() => {
                        if (expandable) onToggleExpand?.(id)
                        else onRowClick?.(row)
                      }}
                    >
                      {expandable && (
                        <td className="w-8 px-2 py-3 text-[var(--text-tertiary)]">
                          <button
                            type="button"
                            aria-expanded={expanded}
                            aria-label={expanded ? t('dataTable.collapseRow') : t('dataTable.expandRow')}
                            // O click na linha já alterna a expansão; sem stopPropagation o
                            // botão alternaria duas vezes (abre e fecha).
                            onClick={(e) => {
                              e.stopPropagation()
                              onToggleExpand?.(id)
                            }}
                            className="flex size-6 items-center justify-center rounded-[6px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          >
                            <Icon name={expanded ? 'expand_less' : 'expand_more'} size={16} />
                          </button>
                        </td>
                      )}
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
                    {expanded && (
                      <tr className="border-b border-[var(--border)] last:border-0">
                        <td colSpan={colCount} className="bg-[var(--bg)] px-4 py-4">
                          {renderExpanded!(row)}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          )}
        </table>
      </div>

      {loading && (
        <div role="status">
          <span className="sr-only">{t('dataTable.loading')}</span>
          <LoadingState rows={6} />
        </div>
      )}
      {!loading && error && (
        <ErrorState
          title={errorState?.title ?? t('dataTable.loadFailed')}
          description={errorState?.description}
          action={errorState?.action}
        />
      )}
      {!loading && !error && rows.length === 0 && (
        <EmptyState
          icon={empty?.icon}
          title={empty?.title ?? t('dataTable.empty')}
          description={empty?.description}
          action={empty?.action}
        />
      )}
      </div>

      {pagination && (
        <div className="flex items-center justify-between gap-3 pt-3">
          <div className="text-xs text-[var(--text-tertiary)]">{pagination.label}</div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon"
              title={pagination.prevLabel}
              aria-label={pagination.prevLabel}
              disabled={pagination.page <= 1}
              onClick={() => pagination.onPageChange(pagination.page - 1)}
            >
              <Icon name="chevron_left" size={16} />
            </Button>
            <Button
              variant="outline"
              size="icon"
              title={pagination.nextLabel}
              aria-label={pagination.nextLabel}
              disabled={pagination.page >= pagination.pageCount}
              onClick={() => pagination.onPageChange(pagination.page + 1)}
            >
              <Icon name="chevron_right" size={16} />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
