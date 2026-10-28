import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Drawer,
  DrawerBody,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import { Icon } from '@/components/ui/icon'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog, LoadingState, StatusPill } from '@/components/patterns'
import {
  BUILDER_SKILL_LIMITS,
  builderSkillErrorInfo,
  useBuilderSkill,
  useBuilderSkills,
  useDeleteBuilderSkill,
  useSetBuilderSkillEnabled,
  useUploadBuilderSkill,
} from '@/lib/api'
import type { BuilderSkill, BuilderSkillSaveResult, BuilderSkillsIndex } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { Notice, SectionCard } from './builderSettingsParts'

// Settings -> AI Builder -> Skills (ai-builder-skills S10). Superadmin only (the page mounts it
// for a superadmin and the routes answer 403 to anyone else). The server owns the built-in lock:
// the controls hidden here for built-ins (edit, delete) are also refused with 409 there.

const KIB = 1024

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

export function BuilderSkillsSection() {
  const { t } = useTranslation()
  const query = useBuilderSkills()
  const toggle = useSetBuilderSkillEnabled()
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadFor, setUploadFor] = useState<string | null>(null)
  const [selected, setSelected] = useState<string | null>(null)

  const openUpload = (forName: string | null) => {
    setUploadFor(forName)
    setUploadOpen(true)
  }

  const action = (
    <Button size="sm" className="shrink-0 gap-2" onClick={() => openUpload(null)}>
      <Icon name="upload_file" size={14} />
      {t('settings.aiBuilder.skills.uploadButton')}
    </Button>
  )

  if (query.isLoading) {
    return (
      <SectionCard title={t('settings.aiBuilder.skills.title')}>
        <LoadingState rows={3} className="p-0" />
      </SectionCard>
    )
  }

  if (!query.data) {
    return (
      <SectionCard title={t('settings.aiBuilder.skills.title')}>
        <div className="flex flex-wrap items-center gap-3 text-[12.5px]" style={{ color: 'var(--danger)' }} role="alert">
          {t('settings.aiBuilder.skills.loadError')}
          <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
            {t('settings.aiBuilder.retry')}
          </Button>
        </div>
      </SectionCard>
    )
  }

  const { skills, index } = query.data
  const hidden = skills.filter((s) => s.enabled && !s.in_agent_view)

  return (
    <SectionCard
      title={t('settings.aiBuilder.skills.title')}
      description={t('settings.aiBuilder.skills.desc')}
      action={action}
    >
      <IndexMeter index={index} />
      {hidden.length > 0 && (
        <Notice tone="warning">
          {t('settings.aiBuilder.skills.notVisibleAlert', {
            count: hidden.length,
            names: hidden.map((s) => s.name).join(', '),
          })}
        </Notice>
      )}

      {skills.length === 0 ? (
        <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
          {t('settings.aiBuilder.skills.empty')}
        </p>
      ) : (
        <ul className="flex flex-col divide-y rounded-[10px] border" style={{ borderColor: 'var(--border)' }}>
          {skills.map((skill) => (
            <SkillRow
              key={skill.name}
              skill={skill}
              toggling={toggle.isPending && toggle.variables?.name === skill.name}
              onOpen={() => setSelected(skill.name)}
              onToggle={(enabled) =>
                toggle.mutate(
                  { name: skill.name, enabled },
                  {
                    onSuccess: () =>
                      toast.success(
                        t(enabled ? 'settings.aiBuilder.skills.toggled.enabled' : 'settings.aiBuilder.skills.toggled.disabled', {
                          name: skill.name,
                        }),
                      ),
                  },
                )
              }
            />
          ))}
        </ul>
      )}

      {uploadOpen && (
        <UploadDialog
          forName={uploadFor}
          onClose={() => {
            setUploadOpen(false)
            setUploadFor(null)
          }}
        />
      )}
      <SkillDrawer
        name={selected}
        onClose={() => setSelected(null)}
        onUploadVersion={(name) => openUpload(name)}
      />
    </SectionCard>
  )
}

function IndexMeter({ index }: { index: BuilderSkillsIndex }) {
  const { t } = useTranslation()
  const pct = index.max > 0 ? Math.min(100, Math.round((index.chars / index.max) * 100)) : 0
  const full = index.chars > index.max
  return (
    <div className="flex flex-col gap-1.5">
      <div className="text-[12px]" style={{ color: 'var(--text-secondary)' }}>
        {t('settings.aiBuilder.skills.meter', { chars: formatNumber(index.chars), max: formatNumber(index.max) })}
      </div>
      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={index.max}
        aria-valuenow={Math.min(index.chars, index.max)}
        aria-label={t('settings.aiBuilder.skills.meter', { chars: formatNumber(index.chars), max: formatNumber(index.max) })}
        className="h-1.5 w-full overflow-hidden rounded-full"
        style={{ background: 'var(--hover-surface)' }}
      >
        <div
          className="h-full rounded-full"
          style={{ width: `${pct}%`, background: full || pct >= 90 ? 'var(--warning-text)' : 'var(--primary)' }}
        />
      </div>
    </div>
  )
}

function SourceBadge({ skill }: { skill: BuilderSkill }) {
  const { t } = useTranslation()
  return skill.locked ? (
    <span title={t('settings.aiBuilder.skills.locked')} className="inline-flex">
      <StatusPill label={t('settings.aiBuilder.skills.badge.builtin')} tone="primary" dot={false} />
    </span>
  ) : (
    <StatusPill label={t('settings.aiBuilder.skills.badge.custom')} tone="neutral" dot={false} />
  )
}

function SkillRow({
  skill,
  toggling,
  onOpen,
  onToggle,
}: {
  skill: BuilderSkill
  toggling: boolean
  onOpen: () => void
  onToggle: (enabled: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <li className="flex items-start gap-3 px-3 py-3" style={{ borderColor: 'var(--border)' }}>
      <button
        type="button"
        onClick={onOpen}
        className="flex min-w-0 flex-1 flex-col gap-1 rounded-[8px] text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="flex flex-wrap items-center gap-2">
          {skill.locked && <Icon name="lock" size={14} style={{ color: 'var(--text-tertiary)' }} />}
          <span className="font-mono text-[13px] font-medium" style={{ color: 'var(--text-primary)' }}>
            {skill.name}
          </span>
          <SourceBadge skill={skill} />
          <span className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('settings.aiBuilder.skills.version', { version: skill.version })}
          </span>
          {skill.enabled && !skill.in_agent_view && (
            <StatusPill label={t('settings.aiBuilder.skills.notVisible')} tone="warning" />
          )}
        </span>
        <span className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
          {skill.description}
        </span>
        {skill.updated_at && (
          <span className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {skill.updated_by_email
              ? t('settings.aiBuilder.skills.updatedBy', {
                  date: formatDateTime(skill.updated_at),
                  author: skill.updated_by_email,
                })
              : t('settings.aiBuilder.skills.updated', { date: formatDateTime(skill.updated_at) })}
          </span>
        )}
      </button>
      <Switch
        checked={skill.enabled}
        disabled={toggling}
        onCheckedChange={onToggle}
        aria-label={t('settings.aiBuilder.skills.toggleAria', { name: skill.name })}
      />
    </li>
  )
}

// ---------------------------------------------------------------------------
// Upload
// ---------------------------------------------------------------------------

function UploadDialog({ forName, onClose }: { forName: string | null; onClose: () => void }) {
  const { t } = useTranslation()
  const upload = useUploadBuilderSkill()
  const fileInput = useRef<HTMLInputElement>(null)
  const [content, setContent] = useState('')
  const [localError, setLocalError] = useState<string | null>(null)
  const [result, setResult] = useState<BuilderSkillSaveResult | null>(null)

  const limits = BUILDER_SKILL_LIMITS
  const errorInfo = upload.error ? builderSkillErrorInfo(upload.error) : null

  const handleOpenChange = (next: boolean) => {
    if (!next && upload.isPending) return
    if (!next) onClose()
  }

  const readFile = async (file: File | undefined) => {
    setLocalError(null)
    upload.reset()
    if (!file) return
    if (file.size > limits.fileMaxBytes) {
      setLocalError(t('settings.aiBuilder.skills.upload.tooLarge', { max: limits.fileMaxBytes / KIB }))
      return
    }
    try {
      setContent(await file.text())
    } catch {
      setLocalError(t('settings.aiBuilder.skills.upload.readError'))
    }
  }

  const submit = () => {
    setLocalError(null)
    if (content.trim() === '') {
      setLocalError(t('settings.aiBuilder.skills.upload.empty'))
      return
    }
    upload.mutate(content, {
      onSuccess: (data) => {
        const name = data.skill?.name ?? ''
        if (!data.created) {
          toast.info(t('settings.aiBuilder.skills.upload.unchanged'))
        } else {
          toast.success(
            t(
              data.version === 1
                ? 'settings.aiBuilder.skills.upload.created'
                : 'settings.aiBuilder.skills.upload.versioned',
              { name, version: data.version },
            ),
          )
        }
        if (data.warnings.length > 0) setResult(data)
        else onClose()
      },
    })
  }

  return (
    <Dialog open onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-[640px] gap-0 overflow-y-auto p-6">
        <DialogHeader className="text-left">
          <DialogTitle className="text-base font-bold">
            {forName ? `${t('settings.aiBuilder.skills.uploadNewVersion')}: ${forName}` : t('settings.aiBuilder.skills.upload.title')}
          </DialogTitle>
          <DialogDescription className="text-[13px]">{t('settings.aiBuilder.skills.upload.desc')}</DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="mt-4 flex flex-col gap-3">
            <Notice tone="warning">
              <p className="font-medium">{t('settings.aiBuilder.skills.upload.warnings')}</p>
              <ul className="mt-1 list-disc pl-5">
                {result.warnings.map((w, i) => (
                  <li key={`${w.code}-${w.key ?? i}`}>
                    {t(`settings.aiBuilder.skills.warning.${w.code}`, { key: w.key ?? '' })}
                  </li>
                ))}
              </ul>
            </Notice>
            <DialogFooter>
              <Button onClick={onClose}>{t('settings.aiBuilder.skills.detail.close')}</Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="mt-4 flex flex-col gap-4">
            {forName && (
              <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
                {t('settings.aiBuilder.skills.upload.versionHint', { name: forName })}
              </p>
            )}
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="skill-file">{t('settings.aiBuilder.skills.upload.file')}</Label>
              <input
                id="skill-file"
                ref={fileInput}
                type="file"
                accept=".md,.markdown,text/markdown,text/plain"
                onChange={(e) => void readFile(e.target.files?.[0])}
                className="text-[12.5px]"
                style={{ color: 'var(--text-secondary)' }}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="skill-content">{t('settings.aiBuilder.skills.upload.content')}</Label>
              <Textarea
                id="skill-content"
                value={content}
                onChange={(e) => {
                  setContent(e.target.value)
                  setLocalError(null)
                  upload.reset()
                }}
                rows={12}
                spellCheck={false}
                className="font-mono text-[12.5px]"
                placeholder={t('settings.aiBuilder.skills.upload.contentPlaceholder')}
              />
              <p className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
                {t('settings.aiBuilder.skills.upload.format', {
                  name: limits.nameMaxChars,
                  description: limits.descriptionMaxChars,
                  body: limits.bodyMaxBytes / KIB,
                  file: limits.fileMaxBytes / KIB,
                })}
              </p>
            </div>

            <Notice tone="warning">{t('settings.aiBuilder.skills.upload.providerNotice')}</Notice>

            {localError && (
              <p role="alert" className="text-[12.5px]" style={{ color: 'var(--danger)' }}>
                {localError}
              </p>
            )}
            {errorInfo && <UploadError error={errorInfo} />}

            <DialogFooter className="gap-2 sm:space-x-0">
              <Button variant="outline" onClick={onClose} disabled={upload.isPending}>
                {t('settings.aiBuilder.skills.upload.cancel')}
              </Button>
              <Button onClick={submit} disabled={upload.isPending} className="gap-2">
                {upload.isPending ? (
                  <>
                    <Icon name="progress_activity" size={14} className="animate-spin" />
                    {t('settings.aiBuilder.skills.upload.submitting')}
                  </>
                ) : (
                  t('settings.aiBuilder.skills.upload.submit')
                )}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function UploadError({ error }: { error: ReturnType<typeof builderSkillErrorInfo> }) {
  const { t } = useTranslation()
  const limits = BUILDER_SKILL_LIMITS

  const fieldMessage = (code: string): string => {
    const key = `settings.aiBuilder.skills.field.${code}`
    const params = {
      max:
        code === 'name_too_long'
          ? limits.nameMaxChars
          : code === 'description_too_long'
            ? limits.descriptionMaxChars
            : code === 'body_too_large'
              ? limits.bodyMaxBytes / KIB
              : limits.fileMaxBytes / KIB,
    }
    return t(key, { ...params, defaultValue: t('settings.aiBuilder.skills.field.unknown') })
  }

  if (error.reason === 'skill_invalid') {
    return (
      <div role="alert" className="flex flex-col gap-1 text-[12.5px]" style={{ color: 'var(--danger)' }}>
        <p className="font-medium">{t('settings.aiBuilder.skills.error.skill_invalid')}</p>
        <ul className="list-disc pl-5">
          {error.fields.map((f) => (
            <li key={`${f.field}-${f.code}`}>
              <span className="font-mono">{f.field}</span>: {fieldMessage(f.code)}
            </li>
          ))}
        </ul>
      </div>
    )
  }

  let message: string
  switch (error.reason) {
    case 'skill_builtin_readonly':
    case 'skill_name_reserved':
    case 'skill_not_found':
    case 'skill_update_invalid':
      message = t(`settings.aiBuilder.skills.error.${error.reason}`)
      break
    case 'skills_index_budget_exceeded':
      message = error.index
        ? t('settings.aiBuilder.skills.error.skills_index_budget_exceeded', {
            chars: formatNumber(error.index.chars),
            max: formatNumber(error.index.max),
          })
        : t('settings.aiBuilder.skills.error.skills_index_budget_exceeded_generic')
      break
    default:
      message = t('settings.aiBuilder.skills.error.generic')
  }
  return (
    <p role="alert" className="text-[12.5px]" style={{ color: 'var(--danger)' }}>
      {message}
    </p>
  )
}

// ---------------------------------------------------------------------------
// Detail drawer
// ---------------------------------------------------------------------------

function SkillDrawer({
  name,
  onClose,
  onUploadVersion,
}: {
  name: string | null
  onClose: () => void
  onUploadVersion: (name: string) => void
}) {
  const { t } = useTranslation()
  const [version, setVersion] = useState<number | undefined>(undefined)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const del = useDeleteBuilderSkill()
  const query = useBuilderSkill(name, version)
  const skill = query.data

  const close = () => {
    setVersion(undefined)
    onClose()
  }

  return (
    <>
      <Drawer open={name !== null} onOpenChange={(open) => !open && close()}>
        <DrawerContent width={560}>
          <DrawerHeader>
            <DrawerTitle className="font-mono text-base">{name}</DrawerTitle>
            <DrawerDescription>{skill?.description ?? ''}</DrawerDescription>
          </DrawerHeader>
          <DrawerBody className="flex flex-col gap-4">
            {query.isLoading ? (
              <LoadingState rows={4} className="p-0" />
            ) : !skill ? (
              <p role="alert" className="text-[12.5px]" style={{ color: 'var(--danger)' }}>
                {t('settings.aiBuilder.skills.detail.loadError')}
              </p>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  <SourceBadge skill={skill} />
                  <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                    {t('settings.aiBuilder.skills.version', { version: skill.version })}
                  </span>
                  {skill.locked && (
                    <StatusPill label={t('settings.aiBuilder.skills.detail.readOnly')} tone="neutral" dot={false} />
                  )}
                </div>
                {skill.locked && <Notice tone="warning">{t('settings.aiBuilder.skills.locked')}</Notice>}
                {(skill.provenance || skill.license) && (
                  <div className="flex flex-col gap-0.5 text-[12px]" style={{ color: 'var(--text-secondary)' }}>
                    {skill.provenance && (
                      <span>{t('settings.aiBuilder.skills.detail.provenance', { source: skill.provenance })}</span>
                    )}
                    {skill.license && (
                      <span>{t('settings.aiBuilder.skills.detail.license', { license: skill.license })}</span>
                    )}
                  </div>
                )}

                <div className="flex flex-col gap-1.5">
                  <h3 className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
                    {t('settings.aiBuilder.skills.detail.body')}
                  </h3>
                  {/* Plain text only: skill content is never rendered as HTML. */}
                  <pre
                    tabIndex={0}
                    aria-label={t('settings.aiBuilder.skills.detail.body')}
                    className="max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-[10px] border p-3 font-mono text-[12px]"
                    style={{ background: 'var(--hover-surface)', borderColor: 'var(--border)', color: 'var(--text-primary)' }}
                  >
                    {skill.body}
                  </pre>
                </div>

                {skill.versions && skill.versions.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    <h3 className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
                      {t('settings.aiBuilder.skills.detail.history')}
                    </h3>
                    <ul className="flex flex-col divide-y rounded-[10px] border" style={{ borderColor: 'var(--border)' }}>
                      {skill.versions.map((v) => (
                        <li
                          key={v.version}
                          className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-[12px]"
                          style={{ borderColor: 'var(--border)', color: 'var(--text-secondary)' }}
                        >
                          <span>
                            {t('settings.aiBuilder.skills.detail.historyItem', {
                              version: v.version,
                              date: formatDateTime(v.created_at),
                              author: v.created_by_email || '-',
                            })}
                          </span>
                          {v.version === skill.version ? (
                            <span style={{ color: 'var(--text-tertiary)' }}>
                              {t('settings.aiBuilder.skills.detail.viewing', { version: v.version })}
                            </span>
                          ) : (
                            <Button variant="ghost" size="sm" onClick={() => setVersion(v.version)}>
                              {t('settings.aiBuilder.skills.detail.view', { version: v.version })}
                            </Button>
                          )}
                        </li>
                      ))}
                    </ul>
                    {version !== undefined && (
                      <Button variant="ghost" size="sm" className="self-start" onClick={() => setVersion(undefined)}>
                        {t('settings.aiBuilder.skills.detail.viewLatest')}
                      </Button>
                    )}
                  </div>
                )}
              </>
            )}
          </DrawerBody>
          {skill && !skill.locked && (
            <DrawerFooter className="justify-between">
              <Button variant="outline" className="gap-2" onClick={() => onUploadVersion(skill.name)}>
                <Icon name="upload_file" size={14} />
                {t('settings.aiBuilder.skills.uploadNewVersion')}
              </Button>
              <Button
                variant="outline"
                className="gap-2"
                style={{ color: 'var(--danger)' }}
                onClick={() => setConfirmDelete(true)}
              >
                <Icon name="delete" size={14} />
                {t('settings.aiBuilder.skills.delete.button')}
              </Button>
            </DrawerFooter>
          )}
        </DrawerContent>
      </Drawer>

      <ConfirmDialog
        open={confirmDelete}
        destructive
        loading={del.isPending}
        title={t('settings.aiBuilder.skills.delete.title')}
        message={t('settings.aiBuilder.skills.delete.message', { name: name ?? '' })}
        confirmLabel={t('settings.aiBuilder.skills.delete.confirm')}
        cancelLabel={t('settings.aiBuilder.skills.delete.cancel')}
        onCancel={() => setConfirmDelete(false)}
        onConfirm={() => {
          if (!name) return
          del.mutate(name, {
            onSuccess: () => {
              toast.success(t('settings.aiBuilder.skills.delete.done', { name }))
              setConfirmDelete(false)
              close()
            },
            onError: () => setConfirmDelete(false),
          })
        }}
      />
    </>
  )
}
