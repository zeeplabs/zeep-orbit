import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  useBuilderTemplates,
  useCreateBuilderSession,
  type AppDef,
  type BuilderTemplate,
} from '@/lib/api'

// Max session title length accepted by POST /builder/sessions (see CreateBuilderSessionInput).
const MAX_TITLE_LENGTH = 120

// The default template has an empty id and Radix <SelectItem> rejects value="", so the select
// works on a synthetic key and the empty/absent template_id is restored on submit.
const DEFAULT_TEMPLATE_KEY = '__default__'

function templateKey(template: BuilderTemplate): string {
  return template.kind === 'default' || !template.id ? DEFAULT_TEMPLATE_KEY : template.id
}

interface NewBuilderSessionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Backend apps the current user can access (the list AppsPage already loaded). */
  apps: AppDef[]
}

// NewBuilderSessionDialog — ai-frontend-builder T35 (AFB-03/04). "Create with AI" entry point of
// AppsPage: picks the backend app + base template (+ optional title), creates the builder session
// and navigates to /builder/:sessionId. The form is mounted only while the dialog is open, so its
// state resets on every open and the templates request is not issued until it is needed.
export function NewBuilderSessionDialog({ open, onOpenChange, apps }: NewBuilderSessionDialogProps) {
  const { t } = useTranslation()
  const create = useCreateBuilderSession()

  // Block close (overlay click / Esc / X) while the session is being created.
  const handleOpenChange = (next: boolean) => {
    if (!next && create.isPending) return
    onOpenChange(next)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t('builder.newSession.title')}</DialogTitle>
          <DialogDescription>{t('builder.newSession.description')}</DialogDescription>
        </DialogHeader>
        <NewBuilderSessionForm
          apps={apps}
          create={create}
          onCancel={() => handleOpenChange(false)}
          onCreated={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}

interface FormProps {
  apps: AppDef[]
  create: ReturnType<typeof useCreateBuilderSession>
  onCancel: () => void
  onCreated: () => void
}

function NewBuilderSessionForm({ apps, create, onCancel, onCreated }: FormProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const templatesQuery = useBuilderTemplates()
  const templates = templatesQuery.data ?? []

  const [appId, setAppId] = useState<string>(apps.length === 1 ? apps[0].id : '')
  const [templateChoice, setTemplateChoice] = useState<string>('')
  const [title, setTitle] = useState('')
  const [submitted, setSubmitted] = useState(false)

  // First template (the API lists the default one first) is preselected until the user picks another.
  const selectedTemplateKey =
    templateChoice && templates.some((tpl) => templateKey(tpl) === templateChoice)
      ? templateChoice
      : templates[0]
        ? templateKey(templates[0])
        : ''

  const appMissing = !appId
  const templateMissing = !selectedTemplateKey
  const noApps = apps.length === 0
  const pending = create.isPending

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitted(true)
    if (appMissing || templateMissing || pending) return

    const trimmedTitle = title.trim()
    create.mutate(
      {
        backend_app_id: appId,
        // Absent template_id selects the built-in default template.
        ...(selectedTemplateKey !== DEFAULT_TEMPLATE_KEY ? { template_id: selectedTemplateKey } : {}),
        ...(trimmedTitle ? { title: trimmedTitle } : {}),
        locale: i18n.language,
      },
      {
        // useCreateBuilderSession already toasts error.message on failure.
        onSuccess: (session) => {
          onCreated()
          navigate(`/builder/${session.id}`)
        },
      },
    )
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
      {/* Backend app */}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="builder-app">{t('builder.newSession.appLabel')}</Label>
        {noApps ? (
          <p id="builder-app-error" role="alert" className="text-[12px] text-[var(--danger)]">
            {t('builder.newSession.appsEmpty')}
          </p>
        ) : (
          <>
            <Select value={appId} onValueChange={setAppId} disabled={pending}>
              <SelectTrigger
                id="builder-app"
                aria-invalid={submitted && appMissing}
                aria-describedby={submitted && appMissing ? 'builder-app-error' : 'builder-app-hint'}
              >
                <SelectValue placeholder={t('builder.newSession.appPlaceholder')} />
              </SelectTrigger>
              <SelectContent>
                {apps.map((app) => (
                  <SelectItem key={app.id} value={app.id}>
                    {app.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {submitted && appMissing ? (
              <p id="builder-app-error" role="alert" className="text-[12px] text-[var(--danger)]">
                {t('builder.newSession.appRequired')}
              </p>
            ) : (
              <p id="builder-app-hint" className="text-[12px] text-[var(--text-tertiary)]">
                {t('builder.newSession.appHint')}
              </p>
            )}
          </>
        )}
      </div>

      {/* Template */}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="builder-template">{t('builder.newSession.templateLabel')}</Label>
        {templatesQuery.isError ? (
          <div role="alert" className="flex items-center justify-between gap-3 rounded-[10px] border border-[var(--danger)]/30 bg-[var(--danger-tint)] px-3 py-2 text-[12px] text-[var(--danger)]">
            <span>{t('builder.newSession.templatesError')}</span>
            <Button type="button" size="sm" variant="outline" onClick={() => templatesQuery.refetch()}>
              {t('builder.newSession.templatesRetry')}
            </Button>
          </div>
        ) : (
          <>
            <Select
              value={selectedTemplateKey}
              onValueChange={setTemplateChoice}
              disabled={pending || templatesQuery.isLoading || templates.length === 0}
            >
              <SelectTrigger
                id="builder-template"
                aria-busy={templatesQuery.isLoading}
                aria-invalid={submitted && templateMissing}
              >
                <SelectValue
                  placeholder={
                    templatesQuery.isLoading
                      ? t('builder.newSession.templatesLoading')
                      : t('builder.newSession.templatePlaceholder')
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {templates.map((tpl) => (
                  <SelectItem key={templateKey(tpl)} value={templateKey(tpl)}>
                    <span>{tpl.name}</span>
                    {tpl.framework && (
                      <span className="ml-2 text-xs text-[var(--text-tertiary)]">{tpl.framework}</span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {!templatesQuery.isLoading && templates.length === 0 && (
              <p role="alert" className="text-[12px] text-[var(--danger)]">
                {t('builder.newSession.templatesEmpty')}
              </p>
            )}
          </>
        )}
      </div>

      {/* Title (optional) */}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="builder-title">{t('builder.newSession.titleLabel')}</Label>
        <Input
          id="builder-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          maxLength={MAX_TITLE_LENGTH}
          disabled={pending}
          autoComplete="off"
          placeholder={t('builder.newSession.titlePlaceholder')}
          aria-describedby="builder-title-hint"
        />
        <p id="builder-title-hint" className="text-[12px] text-[var(--text-tertiary)]">
          {t('builder.newSession.titleHint', { max: MAX_TITLE_LENGTH })}
        </p>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel} disabled={pending}>
          {t('builder.newSession.cancel')}
        </Button>
        <Button type="submit" disabled={pending || noApps || templatesQuery.isLoading || templatesQuery.isError} className="gap-2">
          {pending && <Icon name="progress_activity" size={16} className="animate-spin" />}
          {pending ? t('builder.newSession.submitting') : t('builder.newSession.submit')}
        </Button>
      </DialogFooter>
    </form>
  )
}
