import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '@/components/patterns/PageHeader'
import { ConfirmDialog } from '@/components/patterns/ConfirmDialog'
import { EmptyState, ErrorState } from '@/components/patterns/states'
import { Skeleton } from '@/components/ui/skeleton'
import { Icon } from '@/components/ui/icon'
import { ChatPanel } from '@/components/patterns/builder/ChatPanel'
import { PreviewPanel } from '@/components/patterns/builder/PreviewPanel'
import { SessionActions } from '@/components/patterns/builder/SessionActions'
import {
  buildTurnViews,
  knownCode,
  latestPublishEvent,
  summarizeBuild,
  UNAVAILABLE_REASONS,
} from '@/components/patterns/builder/builderModel'
import {
  builderKeys,
  classifyBuilderError,
  isBuilderSessionActive,
  useBuilderEvents,
  useBuilderSession,
  useBuilderStatus,
  useRevertBuilderSession,
  type BuilderEvent,
} from '@/lib/api'

const NO_EVENTS: BuilderEvent[] = []

// BuilderPage — ai-frontend-builder T36 (chat, activity timeline, proposal card) and T37
// (sandboxed preview, restore, publish, download). All server state comes from the T33 hooks;
// this page owns no fetching of its own.
export default function BuilderPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { sessionId = '' } = useParams()

  const sessionQuery = useBuilderSession(sessionId)
  const eventsQuery = useBuilderEvents(sessionId)
  const statusQuery = useBuilderStatus()
  const revert = useRevertBuilderSession(sessionId)
  const [restoreSeq, setRestoreSeq] = useState<number | null>(null)

  const session = sessionQuery.data
  const events = eventsQuery.data?.events ?? NO_EVENTS

  const turnViews = useMemo(() => (session ? buildTurnViews(session.turns, events) : []), [session, events])
  const build = useMemo(() => summarizeBuild(events), [events])
  const publishEvent = useMemo(() => latestPublishEvent(events), [events])

  if (!sessionId) {
    return <NotFound onBack={() => navigate('/apps')} />
  }

  if (sessionQuery.isLoading) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true" aria-label={t('builder.page.loading')}>
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 lg:grid-cols-[minmax(340px,440px)_minmax(0,1fr)]">
          <Skeleton className="h-[60vh] w-full" />
          <Skeleton className="h-[60vh] w-full" />
        </div>
      </div>
    )
  }

  if (sessionQuery.isError || !session) {
    const kind = classifyBuilderError(sessionQuery.error).kind
    if (kind === 'not_found' || kind === 'forbidden') {
      return <NotFound onBack={() => navigate('/apps')} />
    }
    if (kind === 'not_configured' || kind === 'unavailable') {
      return (
        <ErrorState
          icon="smart_toy"
          title={t('builder.page.unavailableTitle')}
          description={t(kind === 'not_configured' ? 'builder.page.unavailable.not_configured' : 'builder.page.unavailable.unknown')}
          action={{ label: t('builder.page.back'), onClick: () => navigate('/apps') }}
        />
      )
    }
    return (
      <ErrorState
        title={t('builder.page.loadErrorTitle')}
        description={t('builder.page.loadErrorDesc')}
        action={{ label: t('builder.page.retry'), onClick: () => void sessionQuery.refetch() }}
      />
    )
  }

  const active = isBuilderSessionActive(session)
  const archived = session.status === 'archived'
  const unavailableReason =
    statusQuery.data && !statusQuery.data.available ? statusQuery.data.reason : null
  const unavailable = unavailableReason !== null
  const unavailableText = unavailable
    ? t(`builder.page.unavailable.${knownCode(unavailableReason, UNAVAILABLE_REASONS, 'unknown')}`)
    : null

  let blockedReason: string | null = null
  if (archived) blockedReason = t('builder.page.chat.disabledArchived')
  else if (unavailableText) blockedReason = unavailableText
  else if (active) blockedReason = t('builder.page.chat.disabledRunning')
  else if (session.status === 'awaiting_confirmation') blockedReason = t('builder.page.chat.disabledAwaiting')

  const actionsDisabled = archived || unavailable

  const closeRestore = () => setRestoreSeq(null)
  const confirmRestore = () => {
    if (restoreSeq === null) return
    revert.mutate(restoreSeq, {
      onSuccess: closeRestore,
      onError: (error) => {
        closeRestore()
        // 409: a turn started elsewhere (another tab). Resync instead of showing a stale screen.
        if (classifyBuilderError(error).kind === 'turn_active') {
          void qc.invalidateQueries({ queryKey: builderKeys.session(sessionId) })
          void qc.invalidateQueries({ queryKey: builderKeys.events(sessionId) })
        }
      },
    })
  }

  return (
    <div className="flex flex-col lg:h-[calc(100vh-5rem)]">
      <PageHeader
        className="mb-4"
        title={session.title || t('builder.page.untitled')}
        subtitle={
          <span className="flex flex-wrap items-center gap-x-4 gap-y-1">
            <Link
              to="/apps"
              className="inline-flex items-center gap-1 underline-offset-2 hover:underline"
              style={{ color: 'var(--text-secondary)' }}
            >
              <Icon name="arrow_back" size={14} />
              {t('builder.page.back')}
            </Link>
            <Link
              to={`/apps/${encodeURIComponent(session.backend_app_id)}`}
              className="inline-flex items-center gap-1 underline-offset-2 hover:underline"
              style={{ color: 'var(--text-secondary)' }}
            >
              <Icon name="database" size={14} />
              {t('builder.page.backendApp')}
            </Link>
            {session.backend_origin === 'builder' && (
              <span className="inline-flex items-center gap-1" style={{ color: 'var(--text-tertiary)' }}>
                <Icon name="auto_awesome" size={14} />
                {t('builder.page.backendCreated')}
              </span>
            )}
          </span>
        }
        actions={
          <SessionActions
            sessionId={sessionId}
            session={session}
            active={active}
            build={build}
            publishEvent={publishEvent}
            unavailable={unavailable}
          />
        }
      />

      {(archived || unavailableText) && (
        <div
          role="status"
          className="mb-4 flex items-start gap-2 rounded-[10px] px-3 py-2 text-[13px]"
          style={{ background: 'var(--warning-tint)', color: 'var(--warning-text)' }}
        >
          <Icon name={archived ? 'inventory_2' : 'cloud_off'} size={16} className="mt-[1px] shrink-0" />
          <span>{archived ? t('builder.page.archivedNotice') : unavailableText}</span>
        </div>
      )}

      {eventsQuery.isError && (
        <div
          role="alert"
          className="mb-4 flex flex-wrap items-center gap-2 rounded-[10px] px-3 py-2 text-[13px]"
          style={{ background: 'var(--danger-tint)', color: 'var(--danger-text)' }}
        >
          <Icon name="error" size={16} />
          <span>{t('builder.page.eventsError')}</span>
          <button
            type="button"
            onClick={() => void eventsQuery.refetch()}
            className="underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t('builder.page.retry')}
          </button>
        </div>
      )}

      <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[minmax(340px,440px)_minmax(0,1fr)]">
        <div className="h-[70vh] min-h-[420px] lg:h-auto lg:min-h-0">
          <ChatPanel
            sessionId={sessionId}
            session={session}
            turnViews={turnViews}
            active={active}
            blockedReason={blockedReason}
            actionsDisabled={actionsDisabled}
            onRestore={setRestoreSeq}
          />
        </div>
        <div className="min-h-0 lg:h-auto">
          <PreviewPanel
            sessionId={sessionId}
            archived={archived}
            build={build}
            eventsLoading={eventsQuery.isLoading}
          />
        </div>
      </div>

      <ConfirmDialog
        open={restoreSeq !== null}
        title={t('builder.page.restore.title')}
        message={t(restoreSeq === 0 ? 'builder.page.restore.templateMessage' : 'builder.page.restore.message', {
          seq: restoreSeq ?? 0,
        })}
        confirmLabel={t(revert.isPending ? 'builder.page.restore.restoring' : 'builder.page.restore.confirm')}
        cancelLabel={t('builder.page.cancel')}
        icon="history"
        loading={revert.isPending}
        onConfirm={confirmRestore}
        onCancel={closeRestore}
      />
    </div>
  )
}

function NotFound({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation()
  return (
    <EmptyState
      icon="search_off"
      title={t('builder.page.notFoundTitle')}
      description={t('builder.page.notFoundDesc')}
      action={{ label: t('builder.page.back'), onClick: onBack }}
    />
  )
}
