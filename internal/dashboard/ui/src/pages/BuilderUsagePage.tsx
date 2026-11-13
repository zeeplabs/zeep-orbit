import { useEffect, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { DataTable, PageHeader, type Column } from '@/components/patterns'
import { useCostText } from '@/components/patterns/builder/UsageSummary'
import {
  BUILDER_USAGE_LIMITS,
  downloadBuilderUsageCSV,
  useBuilderUsageReport,
  type BuilderUsageGroupBy,
  type BuilderUsageReportRow,
  type BuilderUsageTotals,
} from '@/lib/api'
import i18n from '@/lib/i18n'
import { formatMoney, formatTokens } from '@/lib/money'
import { hasPlatformPermission } from '@/lib/permissions'
import { useCurrentRole } from '@/lib/useCurrentRole'
import { cn } from '@/lib/utils'

// BuilderUsagePage — ai-builder-costs C11 (ABC-22, ABC-23). Usage and cost of a date range,
// grouped by day, model, session or (superadmin) user, with a CSV export. Every figure comes
// from the usage API; the only arithmetic here is the compaction share of the tokens.

const DAY_MS = 24 * 60 * 60 * 1000
const PAGE_SIZE = 50
const DAY_RE = /^\d{4}-\d{2}-\d{2}$/

// The report counts UTC days, so the range is built and shown in UTC as well.
function utcDay(date: Date): string {
  return date.toISOString().slice(0, 10)
}

function dayMs(day: string): number {
  return Date.parse(`${day}T00:00:00Z`)
}

function isDay(value: string | null): value is string {
  return value !== null && DAY_RE.test(value) && !Number.isNaN(dayMs(value))
}

function addDays(day: string, days: number): string {
  return utcDay(new Date(dayMs(day) + days * DAY_MS))
}

function formatDay(day: string): string {
  return new Intl.DateTimeFormat(i18n.language || 'en', {
    timeZone: 'UTC',
    day: '2-digit',
    month: 'short',
    year: 'numeric',
  }).format(new Date(dayMs(day)))
}

function formatPercent(part: number, whole: number): string {
  return new Intl.NumberFormat(i18n.language || 'en', { style: 'percent', maximumFractionDigits: 1 }).format(
    whole > 0 ? part / whole : 0,
  )
}

type Preset = '7' | '30' | '90' | 'month'
const PRESETS: Preset[] = ['7', '30', '90', 'month']

function presetRange(preset: Preset, today: string): { from: string; to: string } {
  if (preset === 'month') return { from: `${today.slice(0, 8)}01`, to: today }
  return { from: addDays(today, -(Number(preset) - 1)), to: today }
}

const GROUPS: BuilderUsageGroupBy[] = ['day', 'model', 'session', 'user']

function Tile({ label, value, detail }: { label: string; value: ReactNode; detail?: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] px-4 py-3">
      <span className="text-[12px] font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">{label}</span>
      <span className="text-[20px] font-bold tabular-nums text-[var(--text-primary)]">{value}</span>
      {detail && <span className="text-[12px] tabular-nums text-[var(--text-secondary)]">{detail}</span>}
    </div>
  )
}

function Segmented<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: Array<{ value: T; label: string }>
  value: T | null
  onChange: (value: T) => void
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className="flex flex-wrap items-center gap-1.5 rounded-[10px] border border-[var(--border)] bg-[var(--surface)] p-1"
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
          className={cn(
            'cursor-pointer rounded-[8px] px-3 py-1.5 text-[13px] font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            value === option.value
              ? 'bg-[var(--primary)] text-white'
              : 'text-[var(--text-secondary)] hover:bg-[var(--hover-surface)]',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

export default function BuilderUsagePage() {
  const { t } = useTranslation()
  const costText = useCostText()
  const seesAll = hasPlatformPermission(useCurrentRole(), 'integrations')

  // Range and grouping live in the URL (?from=&to=&group=), so reload, back and a shared link
  // keep the view. The user filter holds an email for display, so it stays out of the URL.
  const [params, setParams] = useSearchParams()
  const [userFilter, setUserFilter] = useState<{ id: string; label: string } | null>(null)
  const [page, setPage] = useState(1)
  const [downloading, setDownloading] = useState(false)

  const today = utcDay(new Date())
  const defaults = presetRange('30', today)
  const from = isDay(params.get('from')) ? (params.get('from') as string) : defaults.from
  const to = isDay(params.get('to')) ? (params.get('to') as string) : defaults.to
  const requestedGroup = params.get('group') as BuilderUsageGroupBy | null
  const groups = GROUPS.filter((g) => g !== 'user' || (seesAll && !userFilter))
  const groupBy = requestedGroup && groups.includes(requestedGroup) ? requestedGroup : 'day'

  const updateParams = (changes: Record<string, string | null>) => {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries(changes)) {
      if (v) next.set(k, v)
      else next.delete(k)
    }
    setParams(next, { replace: true })
  }

  const rangeDays = Math.round((dayMs(to) - dayMs(from)) / DAY_MS) + 1
  const rangeError =
    rangeDays < 1
      ? t('builder.usage.range.orderError')
      : rangeDays > BUILDER_USAGE_LIMITS.maxRangeDays
        ? t('builder.usage.range.tooLongError', { count: BUILDER_USAGE_LIMITS.maxRangeDays })
        : null
  const activePreset = PRESETS.find((p) => {
    const r = presetRange(p, today)
    return r.from === from && r.to === to
  })

  const range = { from, to, userId: seesAll ? userFilter?.id : undefined }
  const query = useBuilderUsageReport(range, groupBy, rangeError === null)
  const report = rangeError === null ? query.data : undefined
  const currency = report?.currency ?? ''

  useEffect(() => setPage(1), [from, to, groupBy, userFilter?.id])

  const download = async () => {
    setDownloading(true)
    try {
      await downloadBuilderUsageCSV(range)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('builder.usage.csvError'))
    } finally {
      setDownloading(false)
    }
  }

  const groupCell = (row: BuilderUsageReportRow): ReactNode => {
    switch (report?.group_by) {
      case 'user':
        return row.label ? (
          <span className="text-[13px]">{row.label}</span>
        ) : (
          <span className="text-[13px] text-[var(--text-tertiary)]">
            {t('builder.usage.group.deletedUser')} <span className="font-mono text-[12px]">{row.key.slice(0, 8)}</span>
          </span>
        )
      case 'model':
        return (
          <span className="text-[13px]">
            <span className="font-mono text-[12.5px]">{row.model ?? row.key}</span>
            {row.provider && <span className="ml-2 text-[12px] text-[var(--text-tertiary)]">{row.provider}</span>}
          </span>
        )
      case 'session': {
        if (row.key === '') {
          return <span className="text-[13px] text-[var(--text-tertiary)]">{t('builder.usage.group.noSession')}</span>
        }
        const name = row.label ?? <span className="font-mono text-[12px]">{row.key.slice(0, 8)}</span>
        return (
          <span className="text-[13px]">
            {row.label && !row.deleted ? (
              <Link
                to={`/builder/${encodeURIComponent(row.key)}`}
                className="font-medium text-[var(--primary)] underline-offset-2 hover:underline"
              >
                {name}
              </Link>
            ) : (
              name
            )}
            {row.deleted && (
              <span className="ml-2 text-[12px] text-[var(--text-tertiary)]">
                {t('builder.usage.group.sessionGone')}
              </span>
            )}
          </span>
        )
      }
      default:
        return <span className="whitespace-nowrap text-[13px] tabular-nums">{formatDay(row.key)}</span>
    }
  }

  const tokens = (key: string, pick: (row: BuilderUsageReportRow) => number): Column<BuilderUsageReportRow> => ({
    key,
    header: t(`builder.usage.col.${key}`),
    align: 'right',
    render: (row) => <span className="tabular-nums text-[13px]">{formatTokens(pick(row))}</span>,
  })
  const columns: Column<BuilderUsageReportRow>[] = [
    { key: 'group', header: t(`builder.usage.groupBy.${report?.group_by ?? groupBy}`), render: groupCell },
    tokens('calls', (r) => r.calls),
    tokens('input', (r) => r.input_tokens),
    tokens('cachedInput', (r) => r.cached_input_tokens),
    tokens('cacheWrite', (r) => r.cache_write_tokens),
    tokens('output', (r) => r.output_tokens),
    tokens('total', (r) => r.total_tokens),
    {
      key: 'compaction',
      header: t('builder.usage.col.compaction'),
      align: 'right',
      render: (row) => (
        <span className="tabular-nums text-[13px] text-[var(--text-secondary)]">
          {row.compaction_tokens > 0 ? formatPercent(row.compaction_tokens, row.total_tokens) : '—'}
        </span>
      ),
    },
    {
      key: 'cost',
      header: t('builder.usage.col.cost'),
      align: 'right',
      render: (row) => (
        <span className="whitespace-nowrap text-[13px] font-medium tabular-nums">{costText(row, currency)}</span>
      ),
    },
  ]

  const rows = report?.rows ?? []
  const pageCount = Math.ceil(rows.length / PAGE_SIZE)
  const pageRows = rows.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
  const totals: BuilderUsageTotals | undefined = report?.totals
  const hasUsage = (totals?.calls ?? 0) > 0
  // Amounts recorded before a currency change keep their old number: flag it when the change
  // is not older than the range.
  const currencyChangedDay = report?.currency_changed_at ? report.currency_changed_at.slice(0, 10) : null
  const currencyChanged = hasUsage && currencyChangedDay !== null && currencyChangedDay >= from

  return (
    <>
      <PageHeader
        title={t('builder.usage.title')}
        subtitle={t(seesAll ? 'builder.usage.subtitleAll' : 'builder.usage.subtitleOwn')}
        actions={
          <Button
            variant="outline"
            onClick={() => void download()}
            disabled={downloading || rangeError !== null || !hasUsage}
          >
            <Icon name="download" size={16} className="mr-1.5" />
            {downloading ? t('builder.usage.csvDownloading') : t('builder.usage.csv')}
          </Button>
        }
      />

      <div className="mb-2 flex flex-wrap items-end gap-3">
        <Segmented
          label={t('builder.usage.range.label')}
          options={PRESETS.map((p) => ({ value: p, label: t(`builder.usage.range.preset.${p}`) }))}
          value={activePreset ?? null}
          onChange={(p) => updateParams(presetRange(p, today))}
        />
        <div className="flex items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="usage-from" className="text-[12px] text-[var(--text-tertiary)]">
              {t('builder.usage.range.from')}
            </Label>
            <Input
              id="usage-from"
              type="date"
              value={from}
              max={today}
              aria-invalid={rangeError !== null}
              onChange={(e) => e.target.value && updateParams({ from: e.target.value })}
              className="h-9 w-[150px] text-[12.5px]"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="usage-to" className="text-[12px] text-[var(--text-tertiary)]">
              {t('builder.usage.range.to')}
            </Label>
            <Input
              id="usage-to"
              type="date"
              value={to}
              max={today}
              aria-invalid={rangeError !== null}
              onChange={(e) => e.target.value && updateParams({ to: e.target.value })}
              className="h-9 w-[150px] text-[12.5px]"
            />
          </div>
        </div>
        <div className="ml-auto">
          <Segmented
            label={t('builder.usage.groupBy.label')}
            options={groups.map((g) => ({ value: g, label: t(`builder.usage.groupBy.${g}`) }))}
            value={groupBy}
            onChange={(g) => updateParams({ group: g === 'day' ? null : g })}
          />
        </div>
      </div>
      <p className="mb-4 text-[12px] text-[var(--text-tertiary)]" role={rangeError ? 'alert' : undefined}>
        {rangeError ? <span className="text-[var(--danger)]">{rangeError}</span> : t('builder.usage.range.utcNote')}
      </p>

      {userFilter && (
        <div className="mb-4 flex flex-wrap items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--surface)] px-3 py-2 text-[13px] text-[var(--text-secondary)]">
          <Icon name="person" size={16} className="text-[var(--text-tertiary)]" />
          <span>{t('builder.usage.userFilter', { user: userFilter.label })}</span>
          <Button variant="ghost" size="sm" className="ml-auto h-7" onClick={() => setUserFilter(null)}>
            {t('builder.usage.userFilterClear')}
          </Button>
        </div>
      )}

      <div
        className={cn(
          'mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4',
          query.isPlaceholderData && 'opacity-60 transition-opacity',
        )}
      >
        <Tile
          label={t('builder.usage.tile.cost')}
          value={
            !totals || !hasUsage
              ? '—'
              : totals.cost_micros === null
                ? t('builder.page.usage.noPrice')
                : formatMoney(totals.cost_micros, currency)
          }
          detail={[
            totals && totals.cost_micros !== null && totals.unpriced_calls > 0
              ? t('builder.usage.tile.unpriced', { count: totals.unpriced_calls })
              : null,
            currency ? t('builder.usage.tile.currency', { currency }) : null,
          ]
            .filter(Boolean)
            .join(' · ')}
        />
        <Tile
          label={t('builder.usage.tile.tokens')}
          value={totals ? formatTokens(totals.total_tokens, true) : '—'}
          detail={
            totals &&
            t('builder.usage.tile.tokensDetail', {
              input: formatTokens(totals.input_tokens + totals.cached_input_tokens + totals.cache_write_tokens, true),
              cached: formatTokens(totals.cached_input_tokens, true),
              output: formatTokens(totals.output_tokens, true),
            })
          }
        />
        <Tile label={t('builder.usage.tile.calls')} value={totals ? formatTokens(totals.calls) : '—'} />
        <Tile
          label={t('builder.usage.tile.compaction')}
          value={totals ? formatPercent(totals.compaction_tokens, totals.total_tokens) : '—'}
          detail={
            totals &&
            (totals.compaction_cost_micros !== null
              ? t('builder.usage.tile.compactionCost', {
                  cost: formatMoney(totals.compaction_cost_micros, currency),
                  count: totals.compaction_calls,
                })
              : t('builder.usage.tile.compactionCalls', { count: totals.compaction_calls }))
          }
        />
      </div>

      {(currencyChanged || (totals?.unreported_calls ?? 0) > 0 || report?.truncated) && (
        <ul className="mb-4 flex flex-col gap-1 text-[12.5px] text-[var(--warning-text)]">
          {currencyChanged && currencyChangedDay && (
            <li>{t('builder.usage.note.currencyChanged', { currency, date: formatDay(currencyChangedDay) })}</li>
          )}
          {totals && totals.unreported_calls > 0 && (
            <li>{t('builder.page.usage.unreported', { count: totals.unreported_calls })}</li>
          )}
          {report?.truncated && <li>{t('builder.usage.note.truncated', { count: rows.length })}</li>}
        </ul>
      )}

      {rangeError === null && (
      <DataTable
        className={cn(query.isPlaceholderData && 'opacity-60 transition-opacity')}
        columns={columns}
        caption={t('builder.usage.title')}
        rows={pageRows}
        getRowId={(row) => row.key}
        loading={query.isLoading}
        error={query.isError}
        errorState={{
          title: t('builder.usage.error'),
          description: query.error?.message,
          action: { label: t('builder.usage.retry'), onClick: () => void query.refetch() },
        }}
        empty={{
          icon: 'receipt_long',
          title: t('builder.usage.empty'),
          description: t('builder.usage.emptyDesc'),
        }}
        onRowClick={
          report?.group_by === 'user'
            ? (row) => {
                setUserFilter({ id: row.key, label: row.label ?? row.key.slice(0, 8) })
                updateParams({ group: null })
              }
            : undefined
        }
        pagination={
          pageCount > 1
            ? {
                page,
                pageCount,
                onPageChange: setPage,
                prevLabel: t('common.previous'),
                nextLabel: t('common.next'),
                label: t('builder.usage.rows', { count: rows.length }),
              }
            : undefined
        }
      />
      )}
      {report?.group_by === 'user' && rows.length > 0 && (
        <p className="mt-2 text-[12px] text-[var(--text-tertiary)]">{t('builder.usage.userHint')}</p>
      )}
    </>
  )
}
