import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ConfirmDialog, EmptyState, ErrorState, LoadingState } from '@/components/patterns'
import {
  BUILDER_MAX_COST_CAP_MICROS,
  catalogModelsOf,
  isCatalogProvider,
  useAIProviderStatus,
  useBuilderCatalog,
  useBuilderPrices,
  useBuilderSettings,
  useDeleteBuilderPrice,
  useUpdateBuilderSettings,
  useUpsertBuilderPrice,
} from '@/lib/api'
import type {
  BuilderModelPrice,
  BuilderPriceInput,
  BuilderSettings,
  BuilderSettingsUpdate,
} from '@/lib/api'
import { formatCalendarDate, formatDateTime } from '@/lib/format'
import { decimalFromMicros, formatMoney, isDecimalAmount, microsFromDecimal } from '@/lib/money'
import { CatalogPricesTable } from './CatalogPricesTable'
import { Notice, SectionCard } from './builderSettingsParts'

// Settings -> "AI Builder" -> costs (ai-builder-costs C09, ai-model-catalog AMC-08/10): the two
// money caps, in USD, and the prices. OpenAI and Anthropic prices come from the model catalog
// shipped with Orbit and are read-only; OpenAI-compatible servers are priced here by the
// superadmin, and such a model without a row shows "no price set".

const MODEL_MAX_LENGTH = 200

export function BuilderCostsSection() {
  return (
    <>
      <CostSettingsCard />
      <PriceTableCard />
    </>
  )
}

// ---------------------------------------------------------------------------
// Money caps
// ---------------------------------------------------------------------------

type CapKey = 'session_cost_cap_micros' | 'user_monthly_cost_cap_micros'
const CAP_KEYS: CapKey[] = ['session_cost_cap_micros', 'user_monthly_cost_cap_micros']

type CostForm = Record<CapKey, string>

function toCostForm(settings: BuilderSettings): CostForm {
  const cap = (micros: number) => (micros > 0 ? decimalFromMicros(micros) : '')
  return {
    session_cost_cap_micros: cap(settings.session_cost_cap_micros),
    user_monthly_cost_cap_micros: cap(settings.user_monthly_cost_cap_micros),
  }
}

/** Empty means no cap (0). null when the text is not an amount the server accepts. */
function parseCap(text: string): number | null {
  if (text.trim() === '') return 0
  const micros = microsFromDecimal(text)
  return micros === null || micros > BUILDER_MAX_COST_CAP_MICROS ? null : micros
}

function CostSettingsCard() {
  const { t } = useTranslation()
  const settingsQuery = useBuilderSettings()
  const pricesQuery = useBuilderPrices()
  const catalogQuery = useBuilderCatalog()
  const update = useUpdateBuilderSettings()
  const [form, setForm] = useState<CostForm | null>(null)
  const [submitted, setSubmitted] = useState(false)
  // The provider row names the model the builder calls; a hook cannot be conditional, so it
  // asks for the default provider until the settings arrive.
  const activeProvider = settingsQuery.data?.provider ?? 'openai'
  const providerStatus = useAIProviderStatus(activeProvider)

  // Hydrate once: a later refetch must not overwrite what is being typed.
  useEffect(() => {
    if (settingsQuery.data && form === null) setForm(toCostForm(settingsQuery.data))
  }, [settingsQuery.data, form])

  const title = t('settings.aiBuilder.costs.title')
  if (settingsQuery.isLoading || (settingsQuery.data && form === null)) {
    return (
      <SectionCard title={title}>
        <LoadingState rows={3} />
      </SectionCard>
    )
  }
  if (settingsQuery.isError || !settingsQuery.data || form === null) {
    return (
      <SectionCard title={title}>
        <ErrorState
          title={t('settings.aiBuilder.loadError')}
          description={settingsQuery.error?.message}
          action={{ label: t('settings.aiBuilder.retry'), onClick: () => void settingsQuery.refetch() }}
        />
      </SectionCard>
    )
  }

  const settings = settingsQuery.data
  const currency = settings.cost_currency
  const serverForm = toCostForm(settings)
  const caps = {
    session_cost_cap_micros: parseCap(form.session_cost_cap_micros),
    user_monthly_cost_cap_micros: parseCap(form.user_monthly_cost_cap_micros),
  }
  const capsValid = CAP_KEYS.every((key) => caps[key] !== null)
  // A pending review can be confirmed by saving the caps unchanged.
  const dirty = settings.cost_caps_review_pending || CAP_KEYS.some((key) => caps[key] !== settings[key])

  // A money cap only counts calls that have a cost, so warn when the model in use has none: a
  // catalog provider's model outside the catalog, or an OpenAI-compatible model with no row.
  const activeModel = providerStatus.data?.model ?? ''
  const anyCap = CAP_KEYS.some((key) => (caps[key] ?? 0) > 0)
  const activeModelUnpriced =
    anyCap &&
    activeModel !== '' &&
    (isCatalogProvider(settings.provider)
      ? catalogQuery.data !== undefined && !catalogModelsOf(catalogQuery.data, settings.provider).some((m) => m.id === activeModel)
      : pricesQuery.data !== undefined && !pricesQuery.data.prices.some((p) => p.provider === settings.provider && p.model === activeModel))

  const handleSave = () => {
    setSubmitted(true)
    if (!capsValid) return
    // Both caps are always sent: an omitted key keeps the stored value, so a cleared cap must
    // travel as an explicit 0. Sending them also confirms a pending review.
    const payload: BuilderSettingsUpdate = {
      session_cost_cap_micros: caps.session_cost_cap_micros ?? 0,
      user_monthly_cost_cap_micros: caps.user_monthly_cost_cap_micros ?? 0,
    }
    update.mutate(payload, {
      onSuccess: (saved) => {
        setForm(toCostForm(saved))
        setSubmitted(false)
        toast.success(t('settings.aiBuilder.saved'))
      },
    })
  }

  return (
    <SectionCard
      title={title}
      description={t('settings.aiBuilder.costs.desc', { currency })}
      action={
        <Link
          to="/builder/usage"
          className="shrink-0 text-[13px] font-semibold text-[var(--primary)] underline-offset-2 hover:underline"
        >
          {t('builder.usage.link')}
        </Link>
      }
    >
      {settings.cost_caps_review_pending && (
        <Notice tone="warning">
          {t('settings.aiBuilder.costs.reviewCaps', {
            currency,
            date: settings.cost_currency_changed_at ? formatDateTime(settings.cost_currency_changed_at) : '',
          })}
        </Notice>
      )}

      <fieldset className="flex flex-col gap-4 border-0 p-0">
        <legend className="mb-1 p-0 text-[12px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-tertiary)' }}>
          {t('settings.aiBuilder.costs.capsTitle')}
        </legend>
        <p className="-mt-2 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
          {t('settings.aiBuilder.costs.capsDesc')}
        </p>
        {activeModelUnpriced && (
          <Notice tone="warning">{t('settings.aiBuilder.costs.capUnpriced', { model: activeModel })}</Notice>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          {CAP_KEYS.map((key) => {
            const id = `builder-cost-${key}`
            const invalid = submitted && caps[key] === null
            return (
              <div key={key} className="flex flex-col gap-1.5">
                <Label htmlFor={id} className="text-[12px] font-semibold">
                  {t(`settings.aiBuilder.costs.${key}`, { currency })}
                </Label>
                <Input
                  id={id}
                  type="text"
                  inputMode="decimal"
                  autoComplete="off"
                  value={form[key]}
                  onChange={(e) => setForm({ ...form, [key]: e.target.value })}
                  aria-invalid={invalid ? true : undefined}
                  aria-describedby={`${id}-hint`}
                />
                <p
                  id={`${id}-hint`}
                  role={invalid ? 'alert' : undefined}
                  className="text-[12px]"
                  style={{ color: invalid ? 'var(--danger)' : 'var(--text-tertiary)' }}
                >
                  {invalid ? t('settings.aiBuilder.costs.capError') : t('settings.aiBuilder.costs.capHint')}
                </p>
              </div>
            )
          })}
        </div>
      </fieldset>

      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={handleSave} disabled={update.isPending || !dirty} className="gap-2">
          <Icon name={update.isPending ? 'progress_activity' : 'check'} size={14} className={update.isPending ? 'animate-spin' : undefined} />
          {update.isPending ? t('settings.aiBuilder.saving') : t('settings.aiBuilder.save')}
        </Button>
        {dirty && !update.isPending && !settings.cost_caps_review_pending && (
          <Button
            variant="ghost"
            onClick={() => {
              setForm(serverForm)
              setSubmitted(false)
            }}
          >
            {t('settings.aiBuilder.discard')}
          </Button>
        )}
      </div>
    </SectionCard>
  )
}

// ---------------------------------------------------------------------------
// Price table
// ---------------------------------------------------------------------------

/** The only provider kind priced here: the catalog providers are read-only. */
const PRICED_PROVIDER = 'openai_compatible'

interface PriceForm {
  /** Set when editing an existing row: the model is then read-only. */
  editing: boolean
  model: string
  input: string
  cachedInput: string
  cacheWrite: string
  output: string
}

const EMPTY_PRICE_FORM: PriceForm = {
  editing: false,
  model: '',
  input: '',
  cachedInput: '',
  cacheWrite: '',
  output: '',
}

type PriceField = 'model' | 'input' | 'cachedInput' | 'cacheWrite' | 'output'

function priceFormErrors(form: PriceForm): Partial<Record<PriceField, true>> {
  const errors: Partial<Record<PriceField, true>> = {}
  const model = form.model.trim()
  if (model === '' || model.length > MODEL_MAX_LENGTH) errors.model = true
  if (!isDecimalAmount(form.input)) errors.input = true
  if (!isDecimalAmount(form.output)) errors.output = true
  if (form.cachedInput.trim() !== '' && !isDecimalAmount(form.cachedInput)) errors.cachedInput = true
  if (form.cacheWrite.trim() !== '' && !isDecimalAmount(form.cacheWrite)) errors.cacheWrite = true
  return errors
}

function toPriceInput(form: PriceForm): BuilderPriceInput {
  const optional = (text: string) => (text.trim() === '' ? null : text.trim())
  // All four price keys are always sent; null clears an optional price.
  return {
    provider: PRICED_PROVIDER,
    model: form.model.trim(),
    input_per_mtok: form.input.trim(),
    cached_input_per_mtok: optional(form.cachedInput),
    cache_write_per_mtok: optional(form.cacheWrite),
    output_per_mtok: form.output.trim(),
  }
}

function PriceTableCard() {
  const { t } = useTranslation()
  const pricesQuery = useBuilderPrices()
  const catalogQuery = useBuilderCatalog()
  const upsert = useUpsertBuilderPrice()
  const del = useDeleteBuilderPrice()
  const [form, setForm] = useState<PriceForm | null>(null)
  const [submitted, setSubmitted] = useState(false)
  const [toDelete, setToDelete] = useState<BuilderModelPrice | null>(null)

  const title = t('settings.aiBuilder.prices.title')
  if (pricesQuery.isLoading) {
    return (
      <SectionCard title={title}>
        <LoadingState rows={3} />
      </SectionCard>
    )
  }
  if (pricesQuery.isError || !pricesQuery.data) {
    return (
      <SectionCard title={title}>
        <ErrorState
          title={t('settings.aiBuilder.loadError')}
          description={pricesQuery.error?.message}
          action={{ label: t('settings.aiBuilder.retry'), onClick: () => void pricesQuery.refetch() }}
        />
      </SectionCard>
    )
  }

  const { currency, prices, unpriced_models: unpriced } = pricesQuery.data
  const errors = form ? priceFormErrors(form) : {}
  const providerLabel = (kind: string) =>
    kind === 'openai' ? 'OpenAI' : kind === 'anthropic' ? 'Anthropic' : kind === 'openai_compatible' ? t('settings.aiBuilder.provider.openaiCompatible') : kind

  const openNew = (model?: string) => {
    setSubmitted(false)
    setForm({ ...EMPTY_PRICE_FORM, model: model ?? '' })
  }
  const openEdit = (price: BuilderModelPrice) => {
    setSubmitted(false)
    setForm({
      editing: true,
      model: price.model,
      input: price.input_per_mtok,
      cachedInput: price.cached_input_per_mtok ?? '',
      cacheWrite: price.cache_write_per_mtok ?? '',
      output: price.output_per_mtok,
    })
  }
  const handleSave = () => {
    if (!form) return
    setSubmitted(true)
    if (Object.keys(errors).length > 0) return
    upsert.mutate(toPriceInput(form), {
      onSuccess: (saved) => {
        toast.success(t('settings.aiBuilder.prices.saved', { model: saved.model }))
        setForm(null)
        setSubmitted(false)
      },
    })
  }

  const priceField = (field: Exclude<PriceField, 'model'>, optional: boolean) => {
    if (!form) return null
    const id = `builder-price-${field}`
    const invalid = submitted && errors[field]
    return (
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id} className="text-[12px] font-semibold">
          {t(`settings.aiBuilder.prices.${field}`)}
        </Label>
        <Input
          id={id}
          type="text"
          inputMode="decimal"
          autoComplete="off"
          value={form[field]}
          placeholder={optional ? t('settings.aiBuilder.prices.optionalPlaceholder') : undefined}
          onChange={(e) => setForm({ ...form, [field]: e.target.value })}
          aria-invalid={invalid ? true : undefined}
          aria-describedby={`${id}-hint`}
        />
        <p
          id={`${id}-hint`}
          role={invalid ? 'alert' : undefined}
          className="text-[12px]"
          style={{ color: invalid ? 'var(--danger)' : 'var(--text-tertiary)' }}
        >
          {invalid
            ? t('settings.aiBuilder.prices.priceError')
            : optional
              ? t('settings.aiBuilder.prices.optionalHint')
              : t('settings.aiBuilder.prices.priceHint', { currency })}
        </p>
      </div>
    )
  }

  return (
    <SectionCard
      title={title}
      description={t('settings.aiBuilder.prices.desc', { currency })}
    >
      {unpriced.length > 0 && (
        <Notice tone="warning">
          <p>{t('settings.aiBuilder.prices.unpricedTitle')}</p>
          <ul className="mt-1.5 flex flex-col gap-1">
            {unpriced.map((m) => (
              <li key={`${m.provider}/${m.model}`} className="flex flex-wrap items-center gap-2">
                <span className="break-all font-mono text-[12px]">
                  {providerLabel(m.provider)} · {m.model}
                </span>
                <span>{t('settings.aiBuilder.prices.unpricedCalls', { count: m.calls })}</span>
                {isCatalogProvider(m.provider) ? (
                  <Link
                    to={`/integracoes/github?tab=ai&provider=${m.provider}`}
                    className="font-semibold underline underline-offset-2"
                  >
                    {t('settings.aiBuilder.prices.notInCatalog')}
                  </Link>
                ) : (
                  <button
                    type="button"
                    className="font-semibold underline underline-offset-2"
                    onClick={() => openNew(m.model)}
                  >
                    {t('settings.aiBuilder.prices.setPrice')}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </Notice>
      )}

      <div>
        <h4 className="text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
          {t('settings.aiBuilder.prices.catalogTitle')}
        </h4>
        {catalogQuery.data && (
          <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
            {t('settings.aiBuilder.prices.catalogDesc', { date: formatCalendarDate(catalogQuery.data.reviewed_at) })}
          </p>
        )}
      </div>
      <CatalogPricesTable />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h4 className="text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
            {t('settings.aiBuilder.prices.compatibleTitle')}
          </h4>
          <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
            {t('settings.aiBuilder.prices.compatibleDesc')}
          </p>
        </div>
        {form === null && (
          <Button variant="outline" size="sm" className="shrink-0 gap-2" onClick={() => openNew()}>
            <Icon name="add" size={14} />
            {t('settings.aiBuilder.prices.add')}
          </Button>
        )}
      </div>

      {form !== null && (
        <div
          className="flex flex-col gap-4 rounded-[10px] border p-4"
          style={{ borderColor: 'var(--border)', background: 'var(--hover-surface)' }}
        >
          <div className="text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
            {form.editing ? t('settings.aiBuilder.prices.editTitle') : t('settings.aiBuilder.prices.addTitle')}
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5 sm:col-span-2">
              <Label htmlFor="builder-price-model" className="text-[12px] font-semibold">
                {t('settings.aiBuilder.prices.model')}
              </Label>
              <Input
                id="builder-price-model"
                value={form.model}
                maxLength={MODEL_MAX_LENGTH}
                autoComplete="off"
                spellCheck={false}
                disabled={form.editing}
                onChange={(e) => setForm({ ...form, model: e.target.value })}
                aria-invalid={submitted && errors.model ? true : undefined}
                aria-describedby="builder-price-model-hint"
              />
              <p
                id="builder-price-model-hint"
                role={submitted && errors.model ? 'alert' : undefined}
                className="text-[12px]"
                style={{ color: submitted && errors.model ? 'var(--danger)' : 'var(--text-tertiary)' }}
              >
                {submitted && errors.model
                  ? t('settings.aiBuilder.prices.modelError')
                  : t('settings.aiBuilder.prices.modelHint')}
              </p>
            </div>
            {priceField('input', false)}
            {priceField('output', false)}
            {priceField('cachedInput', true)}
            {priceField('cacheWrite', true)}
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button onClick={handleSave} disabled={upsert.isPending} className="gap-2">
              <Icon name={upsert.isPending ? 'progress_activity' : 'check'} size={14} className={upsert.isPending ? 'animate-spin' : undefined} />
              {upsert.isPending ? t('settings.aiBuilder.saving') : t('settings.aiBuilder.save')}
            </Button>
            <Button variant="ghost" onClick={() => setForm(null)} disabled={upsert.isPending}>
              {t('settings.aiBuilder.prices.cancel')}
            </Button>
          </div>
        </div>
      )}

      {prices.length === 0 ? (
        <EmptyState
          icon="payments"
          title={t('settings.aiBuilder.prices.emptyTitle')}
          description={t('settings.aiBuilder.prices.emptyDesc')}
          className="py-8"
        />
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('settings.aiBuilder.prices.model')}</TableHead>
                <TableHead className="text-right">{t('settings.aiBuilder.prices.colInput')}</TableHead>
                <TableHead className="text-right">{t('settings.aiBuilder.prices.colCachedInput')}</TableHead>
                <TableHead className="text-right">{t('settings.aiBuilder.prices.colCacheWrite')}</TableHead>
                <TableHead className="text-right">{t('settings.aiBuilder.prices.colOutput')}</TableHead>
                <TableHead>
                  <span className="sr-only">{t('settings.aiBuilder.prices.actions')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {prices.map((price) => {
                const money = (value: string | null) => {
                  const micros = value === null ? null : microsFromDecimal(value)
                  return micros === null ? (
                    <span style={{ color: 'var(--text-tertiary)' }}>{t('settings.aiBuilder.prices.sameAsInput')}</span>
                  ) : (
                    formatMoney(micros, currency)
                  )
                }
                return (
                  <TableRow key={`${price.provider}/${price.model}`}>
                    <TableCell>
                      <div className="break-all font-mono text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
                        {price.model}
                      </div>
                      <div className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                        {providerLabel(price.provider)} · {t('settings.aiBuilder.prices.updatedAt', { date: formatDateTime(price.updated_at) })}
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{money(price.input_per_mtok)}</TableCell>
                    <TableCell className="text-right tabular-nums">{money(price.cached_input_per_mtok)}</TableCell>
                    <TableCell className="text-right tabular-nums">{money(price.cache_write_per_mtok)}</TableCell>
                    <TableCell className="text-right tabular-nums">{money(price.output_per_mtok)}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t('settings.aiBuilder.prices.edit', { model: price.model })}
                          onClick={() => openEdit(price)}
                        >
                          <Icon name="edit" size={16} />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t('settings.aiBuilder.prices.delete', { model: price.model })}
                          onClick={() => setToDelete(price)}
                        >
                          <Icon name="delete" size={16} />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <ConfirmDialog
        open={toDelete !== null}
        destructive
        loading={del.isPending}
        title={t('settings.aiBuilder.prices.deleteTitle')}
        message={t('settings.aiBuilder.prices.deleteMessage', { model: toDelete?.model ?? '' })}
        confirmLabel={t('settings.aiBuilder.prices.deleteConfirm')}
        cancelLabel={t('settings.aiBuilder.prices.cancel')}
        onCancel={() => setToDelete(null)}
        onConfirm={() => {
          if (!toDelete) return
          del.mutate(
            { provider: toDelete.provider, model: toDelete.model },
            {
              onSuccess: () => {
                toast.success(t('settings.aiBuilder.prices.deleted', { model: toDelete.model }))
                setToDelete(null)
              },
            },
          )
        }}
      />
    </SectionCard>
  )
}
