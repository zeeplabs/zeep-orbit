import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { formatMoney, formatTokens } from '@/lib/money'
import type { BuilderCapUsage, BuilderSessionUsageDetail, BuilderTurnUsage, BuilderUsageTotals } from '@/lib/api'
import { BUDGET_CAPS, knownCode } from './builderModel'

// Usage and cost of the AI builder page (ai-builder-costs C10). Every figure comes from the
// usage API; nothing here multiplies tokens by a price. A cost is shown in one of three ways:
// the full cost, the known cost plus a marker that some calls have no price, or "no price set".

/** The cost of a set of calls, never a partial sum presented as the full cost. */
function useCostText(): (usage: BuilderUsageTotals, currency: string) => string {
  const { t } = useTranslation()
  return (usage, currency) => {
    if (usage.cost_micros === null) return t('builder.page.usage.noPrice')
    const cost = formatMoney(usage.cost_micros, currency)
    return usage.unpriced_calls > 0
      ? t('builder.page.usage.costPartial', { cost, count: usage.unpriced_calls })
      : cost
  }
}

function useDurationText(): (ms: number) => string {
  const { t } = useTranslation()
  return (ms) => {
    const total = Math.max(0, Math.round(ms / 1000))
    return total < 60
      ? t('builder.page.usage.durationSeconds', { s: total })
      : t('builder.page.usage.durationMinutes', { m: Math.floor(total / 60), s: total % 60 })
  }
}

/** One line under a finished turn ("12.4K tokens · $0.08"), expandable into the receipt. */
export function TurnUsageLine({ usage, currency }: { usage: BuilderTurnUsage; currency: string }) {
  const { t } = useTranslation()
  const costText = useCostText()
  const durationText = useDurationText()
  const regionId = useId()
  const [open, setOpen] = useState(false)

  const rows: Array<[string, string]> = [
    [t('builder.page.usage.receipt.calls'), formatTokens(usage.calls)],
    [t('builder.page.usage.receipt.input'), formatTokens(usage.input_tokens)],
  ]
  if (usage.cached_input_tokens > 0) {
    rows.push([t('builder.page.usage.receipt.cachedInput'), formatTokens(usage.cached_input_tokens)])
  }
  if (usage.cache_write_tokens > 0) {
    rows.push([t('builder.page.usage.receipt.cacheWrite'), formatTokens(usage.cache_write_tokens)])
  }
  rows.push([t('builder.page.usage.receipt.output'), formatTokens(usage.output_tokens)])
  if (usage.compaction_calls > 0) {
    rows.push([
      t('builder.page.usage.receipt.compaction'),
      t('builder.page.usage.receipt.compactionValue', {
        count: usage.compaction_calls,
        tokens: formatTokens(usage.compaction_tokens),
      }),
    ])
  }
  rows.push([t('builder.page.usage.receipt.cost'), costText(usage, currency)])
  if (usage.duration_ms !== null) {
    rows.push([t('builder.page.usage.receipt.duration'), durationText(usage.duration_ms)])
  }
  rows.push([t('builder.page.usage.receipt.files'), formatTokens(usage.files_changed)])
  rows.push([
    t('builder.page.usage.receipt.builds'),
    usage.builds_failed > 0
      ? t('builder.page.usage.receipt.buildsValue', { run: usage.builds_run, failed: usage.builds_failed })
      : formatTokens(usage.builds_run),
  ])
  if (usage.models.length > 0) {
    rows.push([t('builder.page.usage.receipt.model'), usage.models.map((m) => m.model).join(', ')])
  }

  return (
    <div className="flex flex-col gap-1.5 text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls={regionId}
        aria-label={t('builder.page.usage.receipt.toggle', { seq: usage.seq })}
        className="inline-flex items-center gap-1.5 self-start rounded-[8px] px-1 py-0.5 tabular-nums underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Icon name="receipt_long" size={13} />
        <span>
          {t('builder.page.usage.tokens', { tokens: formatTokens(usage.total_tokens, true) })}
          {' · '}
          {costText(usage, currency)}
        </span>
        <Icon name={open ? 'expand_less' : 'expand_more'} size={14} />
      </button>
      {open && (
        <div
          id={regionId}
          className="rounded-[10px] border px-3 py-2"
          style={{ background: 'var(--surface)', borderColor: 'var(--border)', color: 'var(--text-secondary)' }}
        >
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1">
            {rows.map(([label, value]) => (
              <div key={label} className="contents">
                <dt style={{ color: 'var(--text-tertiary)' }}>{label}</dt>
                <dd className="break-words text-right tabular-nums">{value}</dd>
              </div>
            ))}
          </dl>
          {usage.unreported_calls > 0 && (
            <p className="mt-1.5" style={{ color: 'var(--warning-text)' }}>
              {t('builder.page.usage.unreported', { count: usage.unreported_calls })}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function CapBar({ cap, currency }: { cap: BuilderCapUsage; currency: string }) {
  const { t } = useTranslation()
  const kind = knownCode(cap.cap, BUDGET_CAPS, 'unknown')
  const isCost = kind === 'session_cost' || kind === 'month_cost'
  const percent = cap.limit > 0 ? Math.floor((cap.used * 100) / cap.limit) : 0
  const color = percent >= 100 ? 'var(--danger-text)' : percent >= 80 ? 'var(--warning-text)' : 'var(--primary)'
  const fmt = (n: number) => (isCost ? formatMoney(n, currency) : formatTokens(n, true))
  const label = t(`builder.page.usage.cap.${kind}`)
  return (
    <div className="flex min-w-[150px] flex-col gap-1">
      <div className="flex items-baseline justify-between gap-3">
        <span>{label}</span>
        <span className="tabular-nums" style={{ color: percent >= 80 ? color : undefined }}>
          {t('builder.page.usage.cap.value', { used: fmt(cap.used), limit: fmt(cap.limit) })}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.min(percent, 100)}
        aria-valuetext={t('builder.page.usage.cap.percent', { percent })}
        className="h-1 overflow-hidden rounded-full"
        style={{ background: 'var(--hover-surface)' }}
      >
        <div className="h-full rounded-full" style={{ width: `${Math.min(percent, 100)}%`, background: color }} />
      </div>
    </div>
  )
}

/** Session total and, when caps apply, how much of each is used. Nothing before the first call. */
export function SessionUsageStrip({ usage }: { usage: BuilderSessionUsageDetail | undefined }) {
  const { t } = useTranslation()
  const costText = useCostText()
  if (!usage || (usage.totals.calls === 0 && usage.caps.length === 0)) return null
  return (
    <div
      aria-label={t('builder.page.usage.sessionLabel')}
      role="group"
      className="mb-4 flex flex-wrap items-center gap-x-6 gap-y-2 rounded-[10px] border px-3 py-2 text-[12px]"
      style={{ background: 'var(--surface)', borderColor: 'var(--border)', color: 'var(--text-secondary)' }}
    >
      <span className="inline-flex items-center gap-1.5 tabular-nums">
        <Icon name="receipt_long" size={14} style={{ color: 'var(--text-tertiary)' }} />
        <span className="font-medium" style={{ color: 'var(--text-primary)' }}>
          {t('builder.page.usage.sessionTotal')}
        </span>
        {t('builder.page.usage.tokens', { tokens: formatTokens(usage.totals.total_tokens, true) })}
        {usage.totals.calls > 0 && (
          <>
            {' · '}
            {costText(usage.totals, usage.currency)}
          </>
        )}
      </span>
      {usage.caps.map((cap) => (
        <CapBar key={cap.cap} cap={cap} currency={usage.currency} />
      ))}
    </div>
  )
}
