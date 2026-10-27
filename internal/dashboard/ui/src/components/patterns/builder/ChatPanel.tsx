import { ReactNode, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { ChatMarkdown } from '@/components/patterns/ChatMarkdown'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'
import {
  builderKeys,
  classifyBuilderError,
  useSendBuilderMessage,
  type BuilderSessionDetail,
  type BuilderTurn,
} from '@/lib/api'
import { ActivityTimeline } from './ActivityTimeline'
import { ProposalCard } from './ProposalCard'
import { knownCode, TURN_ERROR_CODES, TURN_LIMITS, type TurnView } from './builderModel'

/** builderMaxMessageRunes in internal/dashboard/builder_messages.go. */
const MAX_MESSAGE_CHARS = 8000

function isLiveTurn(turn: BuilderTurn): boolean {
  return turn.status === 'pending' || turn.status === 'queued' || turn.status === 'running'
}

interface ChatPanelProps {
  sessionId: string
  session: BuilderSessionDetail
  turnViews: TurnView[]
  active: boolean
  /** Why the composer and the actions are blocked right now (already translated), or null. */
  blockedReason: string | null
  /** The builder cannot run actions (archived / unavailable): proposals and restores are inert. */
  actionsDisabled: boolean
  onRestore: (turnSeq: number) => void
}

export function ChatPanel({
  sessionId,
  session,
  turnViews,
  active,
  blockedReason,
  actionsDisabled,
  onRestore,
}: ChatPanelProps) {
  const { t } = useTranslation()
  const scrollerRef = useRef<HTMLDivElement>(null)
  const stickRef = useRef(true)

  // Follow the conversation unless the user scrolled up to read.
  const lastEvent = turnViews.reduce((acc, v) => {
    const seg = v.segments[v.segments.length - 1]
    return Math.max(acc, seg ? seg.id : 0, v.turn.seq)
  }, 0)
  useLayoutEffect(() => {
    const el = scrollerRef.current
    if (el && stickRef.current) el.scrollTop = el.scrollHeight
  }, [lastEvent, turnViews.length, session.status])

  const liveMessage = active
    ? t('builder.page.chat.liveWorking')
    : session.status === 'awaiting_confirmation'
      ? t('builder.page.chat.liveAwaiting')
      : ''

  return (
    <section
      aria-label={t('builder.page.chat.title')}
      className="flex h-full min-h-0 flex-col overflow-hidden rounded-[14px] border"
      style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
    >
      <div
        ref={scrollerRef}
        role="log"
        aria-live="polite"
        aria-relevant="additions text"
        aria-label={t('builder.page.chat.log')}
        tabIndex={0}
        onScroll={(e) => {
          const el = e.currentTarget
          stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48
        }}
        className="flex min-h-[320px] flex-1 flex-col gap-4 overflow-y-auto p-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
        {turnViews.length === 0 && (
          <p className="text-[13px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('builder.page.chat.empty')}
          </p>
        )}
        {session.head_turn_seq > 0 && session.status !== 'archived' && (
          <div className="flex justify-center">
            <button
              type="button"
              disabled={active || actionsDisabled}
              onClick={() => onRestore(0)}
              className="inline-flex items-center gap-1.5 rounded-[8px] px-2 py-1 text-[12px] underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:no-underline disabled:opacity-50"
              style={{ color: 'var(--text-tertiary)' }}
            >
              <Icon name="history" size={14} />
              {t('builder.page.restore.template')}
            </button>
          </div>
        )}
        {turnViews.map((view) => (
          <TurnBlock
            key={view.turn.id}
            sessionId={sessionId}
            session={session}
            view={view}
            active={active}
            actionsDisabled={actionsDisabled}
            onRestore={onRestore}
          />
        ))}
      </div>
      <div className="sr-only" role="status">
        {liveMessage}
      </div>
      <Composer sessionId={sessionId} blockedReason={blockedReason} />
    </section>
  )
}

function Bubble({ role, children }: { role: 'user' | 'assistant'; children: ReactNode }) {
  const { t } = useTranslation()
  const isUser = role === 'user'
  return (
    <div className={cn('flex flex-col gap-1', isUser ? 'items-end' : 'items-start')}>
      <span className="px-1 text-[11px] font-medium" style={{ color: 'var(--text-tertiary)' }}>
        {isUser ? t('builder.page.chat.you') : t('builder.page.chat.assistant')}
      </span>
      <div
        className="max-w-[92%] break-words rounded-[12px] px-3.5 py-2.5 text-[13px] leading-relaxed"
        style={{
          background: isUser ? 'var(--primary)' : 'var(--hover-surface)',
          color: isUser ? '#fff' : 'var(--text-primary)',
        }}
      >
        {children}
      </div>
    </div>
  )
}

function Notice({
  tone,
  icon,
  children,
}: {
  tone: 'warning' | 'danger' | 'neutral'
  icon: string
  children: ReactNode
}) {
  const fg = tone === 'danger' ? 'var(--danger-text)' : tone === 'warning' ? 'var(--warning-text)' : 'var(--text-secondary)'
  const bg = tone === 'danger' ? 'var(--danger-tint)' : tone === 'warning' ? 'var(--warning-tint)' : 'var(--hover-surface)'
  return (
    <div className="flex items-start gap-2 rounded-[10px] px-3 py-2 text-[12.5px]" style={{ background: bg, color: fg }}>
      <Icon name={icon} size={16} className="mt-[1px] shrink-0" />
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}

interface TurnBlockProps {
  sessionId: string
  session: BuilderSessionDetail
  view: TurnView
  active: boolean
  actionsDisabled: boolean
  onRestore: (turnSeq: number) => void
}

function TurnBlock({ sessionId, session, view, active, actionsDisabled, onRestore }: TurnBlockProps) {
  const { t } = useTranslation()
  const { turn, segments } = view
  const live = isLiveTurn(turn)
  const canRestore = turn.status === 'completed' && turn.commit_sha !== '' && session.status !== 'archived'
  const isCurrent = turn.seq === session.head_turn_seq

  return (
    <div className="flex flex-col gap-2.5">
      {turn.user_message ? (
        <Bubble role="user">
          <ChatMarkdown content={turn.user_message} />
        </Bubble>
      ) : turn.kind === 'revert' ? (
        <div className="flex justify-center">
          <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('builder.page.turn.revert', { seq: turn.seq })}
          </span>
        </div>
      ) : null}

      {segments.map((segment, i) =>
        segment.kind === 'message' ? (
          <Bubble key={`m-${segment.id}`} role="assistant">
            <ChatMarkdown content={segment.content} />
          </Bubble>
        ) : (
          <ActivityTimeline
            key={`a-${segment.id}`}
            entries={segment.entries}
            live={live && i === segments.length - 1}
          />
        ),
      )}

      {(turn.status === 'pending' || turn.status === 'queued') && (
        <div className="flex items-center gap-2 text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
          <Icon name="progress_activity" size={14} className="animate-spin" />
          {t('builder.page.turn.queued')}
        </div>
      )}
      {turn.status === 'running' && segments.length === 0 && (
        <div className="flex items-center gap-2 text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
          <Icon name="progress_activity" size={14} className="animate-spin" />
          {t('builder.page.turn.running')}
        </div>
      )}

      {turn.status === 'awaiting_confirmation' && turn.pending_proposal && (
        <ProposalCard
          sessionId={sessionId}
          turn={turn}
          proposal={turn.pending_proposal}
          disabled={actionsDisabled}
        />
      )}

      {turn.status === 'limit_reached' && (
        <Notice tone="warning" icon="timer">
          {t(`builder.page.turn.limit.${knownCode(view.limit, TURN_LIMITS, 'unknown')}`)}
        </Notice>
      )}
      {turn.status === 'budget_exceeded' && (
        <Notice tone="warning" icon="savings">
          {t('builder.page.turn.budgetExceeded')}
        </Notice>
      )}
      {turn.status === 'failed' && (
        <Notice tone="danger" icon="error">
          {t(`builder.page.turn.error.${knownCode(view.errorCode, TURN_ERROR_CODES, 'unknown')}`)}
        </Notice>
      )}

      {canRestore && (
        <div className="flex justify-start">
          {isCurrent ? (
            <span className="inline-flex items-center gap-1.5 px-1 text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
              <Icon name="check" size={13} />
              {t('builder.page.restore.current')}
            </span>
          ) : (
            <button
              type="button"
              disabled={active || actionsDisabled}
              onClick={() => onRestore(turn.seq)}
              aria-label={t('builder.page.restore.aria', { seq: turn.seq })}
              className="inline-flex items-center gap-1.5 rounded-[8px] px-1 py-0.5 text-[11.5px] underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:no-underline disabled:opacity-50"
              style={{ color: 'var(--text-tertiary)' }}
            >
              <Icon name="history" size={13} />
              {t('builder.page.restore.button')}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function Composer({ sessionId, blockedReason }: { sessionId: string; blockedReason: string | null }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const send = useSendBuilderMessage(sessionId)
  const [value, setValue] = useState('')
  const [turnActiveNotice, setTurnActiveNotice] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const refocus = useRef(false)
  const wasBlocked = useRef(blockedReason !== null)

  const blocked = blockedReason !== null

  // A disabled textarea drops focus: hand it back once the assistant finishes a turn we sent.
  useEffect(() => {
    if (wasBlocked.current && !blocked) {
      setTurnActiveNotice(false)
      if (refocus.current) {
        textareaRef.current?.focus()
        refocus.current = false
      }
    }
    wasBlocked.current = blocked
  }, [blocked])

  const submit = () => {
    const content = value.trim()
    if (!content || blocked || send.isPending) return
    setTurnActiveNotice(false)
    send.mutate(content, {
      onSuccess: () => {
        setValue('')
        refocus.current = true
      },
      onError: (error) => {
        // 409 "turn already in progress": the screen was stale. Keep the draft and resync.
        if (classifyBuilderError(error).kind === 'turn_active') {
          setTurnActiveNotice(true)
          void qc.invalidateQueries({ queryKey: builderKeys.session(sessionId) })
          void qc.invalidateQueries({ queryKey: builderKeys.events(sessionId) })
        }
      },
    })
  }

  const hintId = `builder-composer-hint-${sessionId}`
  return (
    <div className="flex flex-col gap-2 border-t p-3" style={{ borderColor: 'var(--border)' }}>
      <div className="flex items-end gap-2">
        <Textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              submit()
            }
          }}
          aria-label={t('builder.page.chat.composerLabel')}
          aria-describedby={hintId}
          placeholder={t('builder.page.chat.placeholder')}
          rows={2}
          maxLength={MAX_MESSAGE_CHARS}
          className="flex-1 resize-none"
          disabled={blocked}
          readOnly={send.isPending}
        />
        <Button
          type="button"
          size="icon"
          onClick={submit}
          disabled={blocked || send.isPending || !value.trim()}
          aria-label={t('builder.page.chat.send')}
        >
          {send.isPending ? (
            <Icon name="progress_activity" size={16} className="animate-spin" />
          ) : (
            <Icon name="send" size={16} />
          )}
        </Button>
      </div>
      <p id={hintId} className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
        {blockedReason ?? (turnActiveNotice ? t('builder.page.chat.turnActive') : t('builder.page.chat.hintKeys'))}
      </p>
    </div>
  )
}
