import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { ConfirmDialog } from '@/components/patterns/ConfirmDialog'
import { StatusPill } from '@/components/patterns/StatusPill'
import {
  builderDownloadUrl,
  classifyBuilderError,
  usePublishBuilderSession,
  type BuilderPublishResult,
  type BuilderSessionDetail,
} from '@/lib/api'
import {
  knownCode,
  PUBLISH_REASONS,
  safeHttpsUrl,
  type BuildSummary,
  type PublishEvent,
} from './builderModel'

interface SessionActionsProps {
  sessionId: string
  session: BuilderSessionDetail
  active: boolean
  build: BuildSummary
  publishEvent: PublishEvent | undefined
  /** The builder cannot run actions right now (unavailable). Archived is read from the session. */
  unavailable: boolean
}

/** Publish + Download ZIP, with the reason when Publish is not available and its outcome. */
export function SessionActions({
  sessionId,
  session,
  active,
  build,
  publishEvent,
  unavailable,
}: SessionActionsProps) {
  const { t } = useTranslation()
  const publish = usePublishBuilderSession(sessionId)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [result, setResult] = useState<BuilderPublishResult | null>(null)
  const [conflict, setConflict] = useState<string | null>(null)

  const archived = session.status === 'archived'
  const published = session.frontend_app_id !== null
  const hasProject = session.head_sha !== ''
  const upToDate = published && session.published_sha !== '' && session.published_sha === session.head_sha

  let disabledReason: string | null = null
  if (archived) disabledReason = t('builder.page.publish.disabled.archived')
  else if (unavailable) disabledReason = t('builder.page.publish.disabled.unavailable')
  else if (active || build.state === 'building') disabledReason = t('builder.page.publish.disabled.active')
  else if (session.status === 'awaiting_confirmation') disabledReason = t('builder.page.publish.disabled.awaiting')
  else if (!hasProject) disabledReason = t('builder.page.publish.disabled.nothing')
  else if (build.state === 'failed') disabledReason = t('builder.page.publish.disabled.buildFailed')
  else if (upToDate) disabledReason = t('builder.page.publish.disabled.upToDate')

  const publishing = publish.isPending || (publishEvent?.type === 'publish_started' && !result && !conflict)
  const blocked = disabledReason !== null || publish.isPending
  const reasonId = `builder-publish-reason-${sessionId}`

  const doPublish = () => {
    setConflict(null)
    publish.mutate(undefined, {
      onSuccess: (res) => {
        setResult(res)
        toast.success(t(res.republish ? 'builder.page.publish.republished' : 'builder.page.publish.published'))
        setConfirmOpen(false)
      },
      onError: (error) => {
        setConfirmOpen(false)
        const c = classifyBuilderError(error)
        // The hook already toasted the server text; this is the localized, persistent version.
        setConflict(
          c.kind === 'publish_conflict'
            ? knownCode(c.reason, PUBLISH_REASONS, 'unknown')
            : c.kind === 'turn_active'
              ? 'turn_in_progress'
              : 'failed',
        )
      },
    })
  }

  const frontendApp = result?.frontend_app
  const appUrl =
    safeHttpsUrl(frontendApp?.custom_domain ? `https://${frontendApp.custom_domain}` : frontendApp?.deploy_url)

  const failedEventCode =
    publishEvent?.type === 'publish_failed' && !result && !conflict
      ? knownCode(publishEvent.payload.code, [...PUBLISH_REASONS, 'github_failed'], 'failed')
      : null

  const downloadDisabled = archived || !hasProject

  return (
    <div className="flex flex-col items-end gap-1.5">
      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button
          type="button"
          // aria-disabled (not disabled) keeps the button focusable so the reason is reachable.
          aria-disabled={blocked}
          aria-describedby={disabledReason ? reasonId : undefined}
          onClick={() => {
            if (!blocked) setConfirmOpen(true)
          }}
          className={blocked ? 'cursor-not-allowed gap-2 opacity-50' : 'gap-2'}
        >
          {publishing ? (
            <Icon name="progress_activity" size={16} className="animate-spin" />
          ) : (
            <Icon name="rocket_launch" size={16} />
          )}
          {publishing
            ? t('builder.page.publish.publishing')
            : t(published ? 'builder.page.publish.republish' : 'builder.page.publish.button')}
        </Button>
        {downloadDisabled ? (
          <Button type="button" variant="outline" className="gap-2" disabled>
            <Icon name="download" size={16} />
            {t('builder.page.publish.download')}
          </Button>
        ) : (
          <Button asChild variant="outline" className="gap-2">
            <a href={builderDownloadUrl(sessionId)} download>
              <Icon name="download" size={16} />
              {t('builder.page.publish.download')}
            </a>
          </Button>
        )}
      </div>

      {disabledReason && (
        <p id={reasonId} className="max-w-[360px] text-right text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {disabledReason}
        </p>
      )}

      {conflict && (
        <p role="alert" className="flex max-w-[360px] items-start gap-1.5 text-right text-[12px]" style={{ color: 'var(--danger-text)' }}>
          <Icon name="error" size={14} className="mt-[1px] shrink-0" />
          {t(`builder.page.publish.reason.${conflict}`)}
        </p>
      )}
      {failedEventCode && (
        <p className="flex max-w-[360px] items-start gap-1.5 text-right text-[12px]" style={{ color: 'var(--danger-text)' }}>
          <Icon name="error" size={14} className="mt-[1px] shrink-0" />
          {t(`builder.page.publish.reason.${failedEventCode}`)}
        </p>
      )}

      {published && !conflict && (
        <div className="flex flex-wrap items-center justify-end gap-2 text-[12px]" style={{ color: 'var(--text-secondary)' }}>
          <StatusPill
            label={t(upToDate ? 'builder.page.publish.statusUpToDate' : 'builder.page.publish.statusUnpublished')}
            tone={upToDate ? 'success' : 'warning'}
          />
          {appUrl ? (
            <a
              href={appUrl}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 underline-offset-2 hover:underline"
              style={{ color: 'var(--primary-text)' }}
            >
              {t('builder.page.publish.openApp')}
              <Icon name="open_in_new" size={13} />
            </a>
          ) : (
            <Link to="/apps" className="underline-offset-2 hover:underline" style={{ color: 'var(--primary-text)' }}>
              {t('builder.page.publish.viewInApps')}
            </Link>
          )}
        </div>
      )}

      <ConfirmDialog
        open={confirmOpen}
        title={t(published ? 'builder.page.publish.republishTitle' : 'builder.page.publish.confirmTitle')}
        message={t(published ? 'builder.page.publish.republishMessage' : 'builder.page.publish.confirmMessage')}
        confirmLabel={t(publish.isPending ? 'builder.page.publish.publishing' : 'builder.page.publish.confirm')}
        cancelLabel={t('builder.page.cancel')}
        icon="rocket_launch"
        loading={publish.isPending}
        onConfirm={doPublish}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  )
}
