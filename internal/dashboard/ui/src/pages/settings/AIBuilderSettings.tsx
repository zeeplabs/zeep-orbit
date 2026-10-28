import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { EmptyState, ErrorState, LoadingState, ProviderTabs, StatusPill } from '@/components/patterns'
import {
  BUILDER_SETTINGS_LIMITS,
  authFetch,
  useAIProviderStatus,
  useBuilderPrompt,
  useBuilderPromptVersions,
  useBuilderSettings,
  useSetBuilderTemplateEnabled,
  useUpdateBuilderPrompt,
  useUpdateBuilderSettings,
} from '@/lib/api'
import type {
  AIProviderKind,
  BuilderPrompt,
  BuilderPromptVersion,
  BuilderSettings,
  BuilderSettingsUpdate,
  GitHubTemplate,
} from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { roleAllowed } from '@/lib/roles'
import { useCurrentRole } from '@/lib/useCurrentRole'
import { BuilderSkillsSection } from './BuilderSkillsSection'
import { Notice, SectionCard } from './builderSettingsParts'

// Settings -> "AI Builder" (ai-frontend-builder T34, AFB-01/02/25/26). Superadmin only: the
// routes answer 403 to anyone else, so the sections (and with them their queries) are only
// mounted for a superadmin. The provider credentials themselves (key, model, base URL) are
// NOT edited here: they live in the AI tab (PUT /ai-providers/{provider}); this tab only picks
// which provider the builder uses and links to that form, so there is a single write path for
// the secret.

const PROMPT_VERSIONS_PAGE = 10
const PROMPT_VERSIONS_MAX = 100 // server clamps `limit` to 100

type LimitKey =
  | 'context_window_tokens'
  | 'session_token_cap'
  | 'user_monthly_token_cap'
  | 'max_steps_per_turn'
  | 'max_fix_iterations'
  | 'max_turn_seconds'

interface LimitField {
  key: LimitKey
  /** Token budgets: empty means 0 ("not set"), sent explicitly. The others are required. */
  clearable: boolean
}

const LIMIT_FIELDS: LimitField[] = [
  { key: 'context_window_tokens', clearable: false },
  { key: 'session_token_cap', clearable: true },
  { key: 'user_monthly_token_cap', clearable: true },
  { key: 'max_steps_per_turn', clearable: false },
  { key: 'max_fix_iterations', clearable: false },
  { key: 'max_turn_seconds', clearable: false },
]

type LimitErrorKind = 'required' | 'integer' | 'range'

interface SettingsForm {
  enabled: boolean
  provider: AIProviderKind
  limits: Record<LimitKey, string>
}

function parseProvider(value: string): AIProviderKind {
  return value === 'openai_compatible' ? 'openai_compatible' : 'openai'
}

function toForm(settings: BuilderSettings): SettingsForm {
  const limits = {} as Record<LimitKey, string>
  for (const { key } of LIMIT_FIELDS) limits[key] = String(settings[key])
  return { enabled: settings.enabled, provider: parseProvider(settings.provider), limits }
}

/** Validates one limit; returns the number to send or the error kind. Never yields NaN. */
function parseLimit(field: LimitField, raw: string): { value: number } | { error: LimitErrorKind } {
  const text = raw.trim()
  if (text === '') return field.clearable ? { value: 0 } : { error: 'required' }
  if (!/^\d+$/.test(text)) return { error: 'integer' }
  const value = Number(text)
  const { min, max } = BUILDER_SETTINGS_LIMITS[field.key]
  if (!Number.isSafeInteger(value) || value < min || value > max) return { error: 'range' }
  return { value }
}

function buildUpdate(form: SettingsForm): { update: BuilderSettingsUpdate; errors: Partial<Record<LimitKey, LimitErrorKind>> } {
  const update: BuilderSettingsUpdate = { enabled: form.enabled, provider: form.provider }
  const errors: Partial<Record<LimitKey, LimitErrorKind>> = {}
  for (const field of LIMIT_FIELDS) {
    const parsed = parseLimit(field, form.limits[field.key])
    if ('error' in parsed) errors[field.key] = parsed.error
    // A cleared cap is sent as an explicit 0, never omitted (omission = "keep" server side).
    else update[field.key] = parsed.value
  }
  return { update, errors }
}

function sameForm(a: SettingsForm, b: SettingsForm): boolean {
  return (
    a.enabled === b.enabled &&
    a.provider === b.provider &&
    LIMIT_FIELDS.every(({ key }) => a.limits[key].trim() === b.limits[key].trim())
  )
}

/** Rune count (the server counts characters, not UTF-16 units). */
function runeCount(text: string): number {
  return Array.from(text).length
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

function SaveButton({
  pending,
  disabled,
  onClick,
}: {
  pending: boolean
  disabled?: boolean
  onClick: () => void
}) {
  const { t } = useTranslation()
  return (
    <Button onClick={onClick} disabled={pending || disabled} className="gap-2">
      {pending ? (
        <>
          <Icon name="progress_activity" size={14} className="animate-spin" /> {t('settings.aiBuilder.saving')}
        </>
      ) : (
        <>
          <Icon name="check" size={14} /> {t('settings.aiBuilder.save')}
        </>
      )}
    </Button>
  )
}

export default function AIBuilderSettings() {
  const { t } = useTranslation()
  const role = useCurrentRole()

  // Defense in depth: the page route and the backend already enforce superadmin.
  if (!roleAllowed(role, ['superadmin'])) {
    return (
      <div
        className="flex flex-col items-center justify-center gap-3 rounded-[14px] border p-12 text-center"
        style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
      >
        <Icon name="shield" size={28} style={{ color: 'var(--text-tertiary)' }} />
        <h2 className="text-base font-bold" style={{ color: 'var(--text-primary)' }}>
          {t('settings.aiBuilder.forbiddenTitle')}
        </h2>
        <p className="max-w-md text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('settings.aiBuilder.forbiddenBody')}
        </p>
      </div>
    )
  }

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <GeneralSection />
      <PromptSection />
      <BuilderSkillsSection />
      <TemplatesSection />
    </div>
  )
}

// ---------------------------------------------------------------------------
// General: enable, provider, caps/limits (AFB-01, AFB-02)
// ---------------------------------------------------------------------------

function GeneralSection() {
  const { t } = useTranslation()
  const settingsQuery = useBuilderSettings()
  const update = useUpdateBuilderSettings()
  const [form, setForm] = useState<SettingsForm | null>(null)
  const [touched, setTouched] = useState<Partial<Record<LimitKey, boolean>>>({})
  const [submitted, setSubmitted] = useState(false)

  // Hydrate once. Later refetches must not overwrite what the superadmin is typing; saving
  // and "discard" re-sync from the server value explicitly.
  useEffect(() => {
    if (settingsQuery.data && form === null) setForm(toForm(settingsQuery.data))
  }, [settingsQuery.data, form])

  if (settingsQuery.isLoading || (settingsQuery.data && form === null)) {
    return (
      <SectionCard title={t('settings.aiBuilder.general.title')}>
        <LoadingState rows={4} />
      </SectionCard>
    )
  }
  if (settingsQuery.isError || !settingsQuery.data || form === null) {
    return (
      <SectionCard title={t('settings.aiBuilder.general.title')}>
        <ErrorState
          title={t('settings.aiBuilder.loadError')}
          description={settingsQuery.error?.message}
          action={{ label: t('settings.aiBuilder.retry'), onClick: () => void settingsQuery.refetch() }}
        />
      </SectionCard>
    )
  }

  const settings = settingsQuery.data
  const serverForm = toForm(settings)
  const dirty = !sameForm(form, serverForm)
  const { update: payload, errors } = buildUpdate(form)
  const hasErrors = Object.keys(errors).length > 0
  const sessionCap = parseLimit(LIMIT_FIELDS[1], form.limits.session_token_cap)
  const monthlyCap = parseLimit(LIMIT_FIELDS[2], form.limits.user_monthly_token_cap)
  const capsMissing =
    'error' in sessionCap || 'error' in monthlyCap || sessionCap.value <= 0 || monthlyCap.value <= 0

  const setLimit = (key: LimitKey, value: string) =>
    setForm({ ...form, limits: { ...form.limits, [key]: value } })

  const handleSave = () => {
    setSubmitted(true)
    if (hasErrors) return
    update.mutate(payload, {
      onSuccess: (saved) => {
        setForm(toForm(saved))
        setTouched({})
        setSubmitted(false)
        toast.success(t('settings.aiBuilder.saved'))
      },
    })
  }

  const handleDiscard = () => {
    setForm(serverForm)
    setTouched({})
    setSubmitted(false)
  }

  return (
    <SectionCard title={t('settings.aiBuilder.general.title')} description={t('settings.aiBuilder.general.desc')}>
      {!settings.configured && <Notice tone="warning">{t('settings.aiBuilder.general.notConfigured')}</Notice>}

      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <Label htmlFor="builder-enabled" className="text-sm font-medium">
            {t('settings.aiBuilder.general.enabled')}
          </Label>
          <p className="mt-0.5 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
            {t('settings.aiBuilder.general.enabledDesc')}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Switch
            id="builder-enabled"
            checked={form.enabled}
            onCheckedChange={(enabled) => setForm({ ...form, enabled })}
          />
          <span
            className="text-[12px] font-medium"
            style={{ color: form.enabled ? 'var(--success)' : 'var(--text-tertiary)' }}
          >
            {form.enabled ? t('settings.aiBuilder.general.on') : t('settings.aiBuilder.general.off')}
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <div className="text-[12px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-tertiary)' }}>
          {t('settings.aiBuilder.provider.title')}
        </div>
        <ProviderTabs
          value={form.provider}
          onChange={(key) => setForm({ ...form, provider: parseProvider(key) })}
          items={[
            { key: 'openai', name: 'OpenAI', icon: 'bolt' },
            { key: 'openai_compatible', name: t('settings.aiBuilder.provider.openaiCompatible'), icon: 'hub' },
          ]}
        />
        <ProviderSummary provider={form.provider} />
      </div>

      <fieldset className="flex flex-col gap-4 border-0 p-0">
        <legend className="mb-1 p-0 text-[12px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-tertiary)' }}>
          {t('settings.aiBuilder.limits.title')}
        </legend>
        <p className="-mt-2 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
          {t('settings.aiBuilder.limits.desc')}
        </p>
        {form.enabled && capsMissing && <Notice tone="warning">{t('settings.aiBuilder.limits.capsRequired')}</Notice>}
        <div className="grid gap-4 sm:grid-cols-2">
          {LIMIT_FIELDS.map((field) => {
            const { min, max } = BUILDER_SETTINGS_LIMITS[field.key]
            const errorKind = errors[field.key]
            const showError = Boolean(errorKind) && (submitted || touched[field.key])
            const id = `builder-limit-${field.key}`
            return (
              <div key={field.key} className="flex flex-col gap-1.5">
                <Label htmlFor={id} className="text-[12px] font-semibold">
                  {t(`settings.aiBuilder.limits.${field.key}`)}
                </Label>
                <Input
                  id={id}
                  type="text"
                  inputMode="numeric"
                  autoComplete="off"
                  value={form.limits[field.key]}
                  onChange={(e) => setLimit(field.key, e.target.value)}
                  onBlur={() => setTouched((prev) => ({ ...prev, [field.key]: true }))}
                  aria-invalid={showError ? true : undefined}
                  aria-describedby={`${id}-hint`}
                />
                <p
                  id={`${id}-hint`}
                  role={showError ? 'alert' : undefined}
                  className="text-[12px]"
                  style={{ color: showError ? 'var(--danger)' : 'var(--text-tertiary)' }}
                >
                  {showError && errorKind
                    ? t(`settings.aiBuilder.limits.error.${errorKind}`, {
                        min: formatNumber(min),
                        max: formatNumber(max),
                      })
                    : t(field.clearable ? 'settings.aiBuilder.limits.rangeHintCap' : 'settings.aiBuilder.limits.rangeHint', {
                        min: formatNumber(min),
                        max: formatNumber(max),
                      })}
                </p>
              </div>
            )
          })}
        </div>
      </fieldset>

      <div className="flex flex-wrap items-center gap-3">
        <SaveButton pending={update.isPending} disabled={!dirty} onClick={handleSave} />
        {dirty && !update.isPending && (
          <Button variant="ghost" onClick={handleDiscard}>
            {t('settings.aiBuilder.discard')}
          </Button>
        )}
      </div>
    </SectionCard>
  )
}

/** Read-only view of the selected provider row, with a link to the form that edits it. */
function ProviderSummary({ provider }: { provider: AIProviderKind }) {
  const { t } = useTranslation()
  const [, setSearchParams] = useSearchParams()
  const status = useAIProviderStatus(provider)

  const goConfigure = () => setSearchParams({ tab: 'ai', provider }, { replace: true })
  const data = status.data
  const isCompatible = provider === 'openai_compatible'
  const ready = Boolean(data?.has_key && data?.enabled && data?.model && (!isCompatible || data?.base_url))

  return (
    <div
      className="flex flex-col gap-3 rounded-[10px] border p-4"
      style={{ borderColor: 'var(--border)', background: 'var(--hover-surface)' }}
    >
      {status.isLoading ? (
        <LoadingState rows={2} className="p-0" />
      ) : status.isError ? (
        <div className="flex flex-wrap items-center gap-3 text-[12.5px]" style={{ color: 'var(--danger)' }} role="alert">
          {t('settings.aiBuilder.provider.loadError')}
          <Button variant="outline" size="sm" onClick={() => void status.refetch()}>
            {t('settings.aiBuilder.retry')}
          </Button>
        </div>
      ) : (
        <>
          <dl className="grid gap-x-6 gap-y-2 text-[13px] sm:grid-cols-2">
            <div>
              <dt style={{ color: 'var(--text-tertiary)' }}>{t('settings.aiBuilder.provider.model')}</dt>
              <dd style={{ color: 'var(--text-primary)' }}>{data?.model || t('settings.aiBuilder.provider.notSet')}</dd>
            </div>
            {isCompatible && (
              <div className="min-w-0">
                <dt style={{ color: 'var(--text-tertiary)' }}>{t('settings.aiBuilder.provider.baseUrl')}</dt>
                <dd className="break-all" style={{ color: 'var(--text-primary)' }}>
                  {data?.base_url || t('settings.aiBuilder.provider.notSet')}
                </dd>
              </div>
            )}
            <div>
              <dt style={{ color: 'var(--text-tertiary)' }}>{t('settings.aiBuilder.provider.apiKey')}</dt>
              <dd>
                <StatusPill
                  label={data?.has_key ? t('settings.aiBuilder.provider.keyConfigured') : t('settings.aiBuilder.provider.keyMissing')}
                  tone={data?.has_key ? 'success' : 'warning'}
                />
              </dd>
            </div>
            <div>
              <dt style={{ color: 'var(--text-tertiary)' }}>{t('settings.aiBuilder.provider.status')}</dt>
              <dd>
                <StatusPill
                  label={data?.enabled ? t('settings.aiBuilder.general.on') : t('settings.aiBuilder.general.off')}
                  tone={data?.enabled ? 'success' : 'neutral'}
                />
              </dd>
            </div>
          </dl>
          {!ready && <Notice tone="warning">{t('settings.aiBuilder.provider.notReady')}</Notice>}
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="outline" size="sm" onClick={goConfigure} className="gap-2">
              <Icon name="settings" size={14} />
              {t('settings.aiBuilder.provider.configure')}
            </Button>
            <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('settings.aiBuilder.provider.configureHint')}
            </span>
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Persona + rules with version history (AFB-25)
// ---------------------------------------------------------------------------

function PromptSection() {
  const { t } = useTranslation()
  const promptQuery = useBuilderPrompt()
  const update = useUpdateBuilderPrompt()
  const [persona, setPersona] = useState('')
  const [rules, setRules] = useState('')
  const [hydrated, setHydrated] = useState(false)

  useEffect(() => {
    if (promptQuery.data && !hydrated) {
      setPersona(promptQuery.data.persona)
      setRules(promptQuery.data.rules)
      setHydrated(true)
    }
  }, [promptQuery.data, hydrated])

  if (promptQuery.isLoading || (promptQuery.data && !hydrated)) {
    return (
      <SectionCard title={t('settings.aiBuilder.prompt.title')}>
        <LoadingState rows={4} />
      </SectionCard>
    )
  }
  if (promptQuery.isError || !promptQuery.data) {
    return (
      <SectionCard title={t('settings.aiBuilder.prompt.title')}>
        <ErrorState
          title={t('settings.aiBuilder.loadError')}
          description={promptQuery.error?.message}
          action={{ label: t('settings.aiBuilder.retry'), onClick: () => void promptQuery.refetch() }}
        />
      </SectionCard>
    )
  }

  const active: BuilderPrompt = promptQuery.data
  const dirty = persona !== active.persona || rules !== active.rules
  // The server counts the assembled company layer (trimmed persona + a "Rules:" header +
  // trimmed rules), so this live figure is an approximation; the saved version reports the
  // exact `chars`, and the server stays the authority (its 400 is shown as a toast).
  const approxChars = runeCount(persona.trim()) + runeCount(rules.trim())
  const overLimit = approxChars > active.max_chars

  const handleSave = () => {
    // Both keys are always sent, "" included: omitting a key means "keep" server side, so a
    // cleared editor would silently not clear.
    update.mutate(
      { persona, rules },
      {
        onSuccess: (saved) => {
          setPersona(saved.persona)
          setRules(saved.rules)
          if (saved.changed === false) toast.info(t('settings.aiBuilder.prompt.unchanged'))
          else toast.success(t('settings.aiBuilder.prompt.saved'))
        },
      },
    )
  }

  const handleRestore = (version: BuilderPromptVersion) => {
    setPersona(version.persona)
    setRules(version.rules)
    toast.info(t('settings.aiBuilder.versions.restored'))
  }

  return (
    <SectionCard title={t('settings.aiBuilder.prompt.title')} description={t('settings.aiBuilder.prompt.desc')}>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="builder-persona" className="text-[12px] font-semibold">
          {t('settings.aiBuilder.prompt.persona')}
        </Label>
        <Textarea
          id="builder-persona"
          rows={6}
          value={persona}
          onChange={(e) => setPersona(e.target.value)}
          placeholder={t('settings.aiBuilder.prompt.personaPlaceholder')}
          aria-describedby="builder-persona-hint builder-prompt-counter"
        />
        <p id="builder-persona-hint" className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {t('settings.aiBuilder.prompt.personaHint')}
        </p>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="builder-rules" className="text-[12px] font-semibold">
          {t('settings.aiBuilder.prompt.rules')}
        </Label>
        <Textarea
          id="builder-rules"
          rows={8}
          value={rules}
          onChange={(e) => setRules(e.target.value)}
          placeholder={t('settings.aiBuilder.prompt.rulesPlaceholder')}
          aria-describedby="builder-rules-hint builder-prompt-counter"
        />
        <p id="builder-rules-hint" className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {t('settings.aiBuilder.prompt.rulesHint')}
        </p>
      </div>

      <p
        id="builder-prompt-counter"
        role={overLimit ? 'alert' : undefined}
        className="text-[12px] font-medium"
        style={{ color: overLimit ? 'var(--danger)' : 'var(--text-secondary)' }}
      >
        {t('settings.aiBuilder.prompt.counter', {
          used: formatNumber(approxChars),
          max: formatNumber(active.max_chars),
        })}
        {overLimit && ` ${t('settings.aiBuilder.prompt.overLimit')}`}
      </p>

      <div className="flex flex-wrap items-center gap-3">
        <SaveButton pending={update.isPending} disabled={!dirty || overLimit} onClick={handleSave} />
        {dirty && !update.isPending && (
          <Button
            variant="ghost"
            onClick={() => {
              setPersona(active.persona)
              setRules(active.rules)
            }}
          >
            {t('settings.aiBuilder.discard')}
          </Button>
        )}
      </div>

      <PromptVersions activeId={active.id} onRestore={handleRestore} />
    </SectionCard>
  )
}

function PromptVersions({
  activeId,
  onRestore,
}: {
  activeId: string
  onRestore: (version: BuilderPromptVersion) => void
}) {
  const { t } = useTranslation()
  const [limit, setLimit] = useState(PROMPT_VERSIONS_PAGE)
  const query = useBuilderPromptVersions({ limit })
  // Keep the previous page on screen while a larger one loads (the query key changes with
  // `limit`, so `data` is briefly undefined).
  const lastPage = useRef(query.data)
  if (query.data) lastPage.current = query.data
  const page = query.data ?? lastPage.current

  return (
    <div className="flex flex-col gap-3 border-t pt-5" style={{ borderColor: 'var(--border)' }}>
      <h3 className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
        {t('settings.aiBuilder.versions.title')}
      </h3>
      {!page && query.isLoading ? (
        <LoadingState rows={3} className="p-0" />
      ) : !page ? (
        <div className="flex flex-wrap items-center gap-3 text-[12.5px]" style={{ color: 'var(--danger)' }} role="alert">
          {t('settings.aiBuilder.versions.loadError')}
          <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
            {t('settings.aiBuilder.retry')}
          </Button>
        </div>
      ) : page.versions.length === 0 ? (
        <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
          {t('settings.aiBuilder.versions.empty')}
        </p>
      ) : (
        <>
          <ul className="flex flex-col divide-y rounded-[10px] border" style={{ borderColor: 'var(--border)' }}>
            {page.versions.map((version) => {
              const isActive = version.id === activeId
              const when = formatDateTime(version.created_at)
              return (
                <li
                  key={version.id}
                  className="flex flex-wrap items-center justify-between gap-3 px-3 py-2.5"
                  style={{ borderColor: 'var(--border)' }}
                >
                  <div className="min-w-0 text-[12.5px]">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium" style={{ color: 'var(--text-primary)' }}>
                        {when}
                      </span>
                      {isActive && <StatusPill label={t('settings.aiBuilder.versions.active')} tone="success" />}
                    </div>
                    <div style={{ color: 'var(--text-secondary)' }}>
                      {version.created_by_email
                        ? t('settings.aiBuilder.versions.by', { author: version.created_by_email })
                        : t('settings.aiBuilder.versions.unknownAuthor')}
                      {' · '}
                      {t('settings.aiBuilder.versions.size', {
                        persona: formatNumber(runeCount(version.persona)),
                        rules: formatNumber(runeCount(version.rules)),
                      })}
                    </div>
                  </div>
                  {!isActive && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => onRestore(version)}
                      aria-label={t('settings.aiBuilder.versions.restoreAria', { date: when })}
                    >
                      {t('settings.aiBuilder.versions.restore')}
                    </Button>
                  )}
                </li>
              )
            })}
          </ul>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('settings.aiBuilder.versions.showing', { shown: page.versions.length, total: page.total })}
            </span>
            {page.versions.length < page.total && limit < PROMPT_VERSIONS_MAX && (
              <Button
                variant="ghost"
                size="sm"
                disabled={query.isFetching}
                onClick={() => setLimit((current) => Math.min(current + PROMPT_VERSIONS_PAGE, PROMPT_VERSIONS_MAX))}
              >
                {t('settings.aiBuilder.versions.showMore')}
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Builder base templates (AFB-26)
// ---------------------------------------------------------------------------

const GITHUB_TEMPLATES_KEY = ['github-templates', 'all'] as const

function TemplatesSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [, setSearchParams] = useSearchParams()
  const setEnabled = useSetBuilderTemplateEnabled()

  // All registered templates (active or not): useBuilderTemplates only lists the ones already
  // enabled for the builder, which is not what a superadmin needs to toggle.
  const templates = useQuery({
    queryKey: GITHUB_TEMPLATES_KEY,
    queryFn: async () => {
      const res = await authFetch('/dashboard/api/github/templates', { credentials: 'include' })
      if (!res.ok) {
        let message = `HTTP ${res.status}`
        try {
          const body = (await res.json()) as { error?: string }
          if (body.error) message = body.error
        } catch {
          // keep the status text
        }
        throw new Error(message)
      }
      return (await res.json()) as GitHubTemplate[]
    },
  })

  const toggle = (template: GitHubTemplate, builderEnabled: boolean) => {
    setEnabled.mutate(
      { id: template.id, builder_enabled: builderEnabled },
      { onSuccess: () => void queryClient.invalidateQueries({ queryKey: GITHUB_TEMPLATES_KEY }) },
    )
  }

  const pendingId = setEnabled.isPending ? setEnabled.variables?.id : undefined
  const list = templates.data ?? []

  return (
    <SectionCard title={t('settings.aiBuilder.templates.title')} description={t('settings.aiBuilder.templates.desc')}>
      <p className="flex items-start gap-2 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
        <Icon name="check_circle" size={16} className="mt-px shrink-0" style={{ color: 'var(--success)' }} />
        {t('settings.aiBuilder.templates.defaultNote')}
      </p>
      {templates.isLoading ? (
        <LoadingState rows={3} className="p-0" />
      ) : templates.isError ? (
        <ErrorState
          title={t('settings.aiBuilder.loadError')}
          description={templates.error.message}
          action={{ label: t('settings.aiBuilder.retry'), onClick: () => void templates.refetch() }}
        />
      ) : list.length === 0 ? (
        <EmptyState
          icon="code"
          title={t('settings.aiBuilder.templates.emptyTitle')}
          description={t('settings.aiBuilder.templates.emptyDesc')}
          action={{
            label: t('settings.aiBuilder.templates.emptyAction'),
            onClick: () => setSearchParams({ tab: 'templates' }, { replace: true }),
          }}
        />
      ) : (
        <ul className="flex flex-col divide-y rounded-[10px] border" style={{ borderColor: 'var(--border)' }}>
          {list.map((template) => {
            const id = `builder-template-${template.id}`
            return (
              <li
                key={template.id}
                className="flex items-start justify-between gap-4 px-3 py-3"
                style={{ borderColor: 'var(--border)' }}
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Label htmlFor={id} className="text-sm font-medium">
                      {template.name}
                    </Label>
                    {!template.active && (
                      <StatusPill label={t('settings.aiBuilder.templates.inactive')} tone="neutral" />
                    )}
                  </div>
                  <div className="mt-0.5 break-all text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                    {template.github_owner}/{template.github_repo}
                    {template.framework ? ` · ${template.framework}` : ''}
                  </div>
                  {template.description && (
                    <div className="mt-1 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
                      {template.description}
                    </div>
                  )}
                  {!template.active && template.builder_enabled && (
                    <div className="mt-1 text-[12px]" style={{ color: 'var(--warning-text)' }}>
                      {t('settings.aiBuilder.templates.inactiveHidden')}
                    </div>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <span
                    className="text-[12px] font-medium"
                    style={{ color: template.builder_enabled ? 'var(--success)' : 'var(--text-tertiary)' }}
                  >
                    {template.builder_enabled
                      ? t('settings.aiBuilder.templates.enabled')
                      : t('settings.aiBuilder.templates.disabled')}
                  </span>
                  <Switch
                    id={id}
                    checked={template.builder_enabled}
                    disabled={pendingId === template.id}
                    onCheckedChange={(checked) => toggle(template, checked)}
                  />
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </SectionCard>
  )
}
