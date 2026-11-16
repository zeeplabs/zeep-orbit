import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorState, LoadingState } from '@/components/patterns'
import { useBuilderCatalog } from '@/lib/api'
import type { BuilderCatalogModel, BuilderCatalogPrice, BuilderCatalogTier } from '@/lib/api'
import { formatCalendarDate } from '@/lib/format'
import { formatMoney, microsFromDecimal } from '@/lib/money'

// The read-only prices of the model catalog (ai-model-catalog AMC-08): OpenAI and Anthropic
// models and their list prices ship with Orbit and cannot be edited.

/** The version in force now and the ones that start later (prices are in ascending order). */
function splitPrices(prices: BuilderCatalogPrice[], now: number) {
  let current: BuilderCatalogPrice | null = null
  const upcoming: BuilderCatalogPrice[] = []
  for (const p of prices) {
    if (Date.parse(p.effective_from) <= now) current = p
    else upcoming.push(p)
  }
  return { current, upcoming }
}

interface Row {
  key: string
  model: BuilderCatalogModel
  provider: string
  tier: BuilderCatalogTier
  /** Lower bound of the tier (0 for the first one). */
  above: number
  tiered: boolean
  from: string | null
}

function rowsOf(provider: string, model: BuilderCatalogModel, now: number): Row[] {
  const { current, upcoming } = splitPrices(model.prices, now)
  const out: Row[] = []
  const add = (price: BuilderCatalogPrice, from: string | null) =>
    price.tiers.forEach((tier, i) =>
      out.push({
        key: `${provider}/${model.id}/${price.effective_from}/${i}`,
        model,
        provider,
        tier,
        above: i > 0 ? price.tiers[i - 1].max_prompt_tokens : 0,
        tiered: price.tiers.length > 1,
        from,
      }),
    )
  if (current) add(current, null)
  upcoming.forEach((p) => add(p, p.effective_from))
  return out
}

export function CatalogPricesTable() {
  const { t, i18n } = useTranslation()
  const catalogQuery = useBuilderCatalog()

  if (catalogQuery.isLoading) return <LoadingState rows={3} />
  if (catalogQuery.isError || !catalogQuery.data) {
    return (
      <ErrorState
        title={t('settings.aiBuilder.loadError')}
        description={catalogQuery.error?.message}
        action={{ label: t('settings.aiBuilder.retry'), onClick: () => void catalogQuery.refetch() }}
      />
    )
  }

  const { currency, providers } = catalogQuery.data
  const now = Date.now()
  const tokens = (n: number) => n.toLocaleString(i18n.language || 'en')
  const money = (value: string | null) => {
    const micros = value === null ? null : microsFromDecimal(value)
    return micros === null ? (
      <span style={{ color: 'var(--text-tertiary)' }}>{t('settings.aiBuilder.prices.sameAsInput')}</span>
    ) : (
      formatMoney(micros, currency)
    )
  }
  const providerName = (p: string) => (p === 'anthropic' ? 'Anthropic' : 'OpenAI')

  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('settings.aiBuilder.prices.model')}</TableHead>
            <TableHead className="text-right">{t('settings.aiBuilder.prices.colInput')}</TableHead>
            <TableHead className="text-right">{t('settings.aiBuilder.prices.colCachedInput')}</TableHead>
            <TableHead className="text-right">{t('settings.aiBuilder.prices.colCacheWrite')}</TableHead>
            <TableHead className="text-right">{t('settings.aiBuilder.prices.colOutput')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {providers.flatMap((p) =>
            p.models.flatMap((m) =>
              rowsOf(p.provider, m, now).map((row) => (
                <TableRow key={row.key}>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="break-all font-mono text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
                        {row.model.id}
                      </span>
                      {row.model.deprecated && <Badge variant="outline">{t('settings.aiBuilder.prices.deprecated')}</Badge>}
                    </div>
                    <div className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                      {providerName(row.provider)}
                      {row.tiered &&
                        ` · ${
                          row.tier.max_prompt_tokens > 0
                            ? t('settings.aiBuilder.prices.tierUpTo', { tokens: tokens(row.tier.max_prompt_tokens) })
                            : t('settings.aiBuilder.prices.tierAbove', { tokens: tokens(row.above) })
                        }`}
                      {row.from && ` · ${t('settings.aiBuilder.prices.fromDate', { date: formatCalendarDate(row.from) })}`}
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{money(row.tier.input_per_mtok)}</TableCell>
                  <TableCell className="text-right tabular-nums">{money(row.tier.cached_input_per_mtok)}</TableCell>
                  <TableCell className="text-right tabular-nums">{money(row.tier.cache_write_per_mtok)}</TableCell>
                  <TableCell className="text-right tabular-nums">{money(row.tier.output_per_mtok)}</TableCell>
                </TableRow>
              )),
            ),
          )}
        </TableBody>
      </Table>
    </div>
  )
}
