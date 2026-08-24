import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FormDrawer } from './FormDrawer'
import { ChatMarkdown } from './ChatMarkdown'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'
import {
  useBuildChatSession,
  useSendBuildChatMessage,
  useConfirmBuildChatPlan,
  useRestartBuildChatSession,
  type BuildChatMessage,
  type BuildChatPlan,
} from '@/lib/api'

interface BuildWithAIDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

// BuildWithAIDrawer — ai-build-chat T12. Live chat wiring for the "Build
// with AI" entry point (AppsPage): session resume on open (AIBC-07/08),
// send-message turns rendered as a message bubble or a plan card
// (AIBC-12/13/14), "Confirm & create app" (P4), and "Restart" (AIBC-09). No
// streaming (spec Out of Scope) — a single loading state per turn.
export function BuildWithAIDrawer({ open, onOpenChange }: BuildWithAIDrawerProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [input, setInput] = useState('')
  const [messages, setMessages] = useState<BuildChatMessage[]>([])
  const [pendingPlan, setPendingPlan] = useState<BuildChatPlan | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  const sessionQuery = useBuildChatSession(open)
  const sendMessage = useSendBuildChatMessage()
  const confirmPlan = useConfirmBuildChatPlan()
  const restartSession = useRestartBuildChatSession()

  // Resume: seed local state from the session's persisted history once it
  // loads, including re-deriving the last proposed plan (if any) so the
  // plan card survives a drawer close/reopen (AIBC-07).
  useEffect(() => {
    if (!sessionQuery.data) return
    setMessages(sessionQuery.data.messages)
    const lastPlanMessage = [...sessionQuery.data.messages].reverse().find((m) => m.plan)
    setPendingPlan(lastPlanMessage?.plan ?? null)
  }, [sessionQuery.data])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, sendMessage.isPending])

  const sessionId = sessionQuery.data?.session.id

  const handleSend = async () => {
    const content = input.trim()
    if (!content || sendMessage.isPending) return
    setInput('')
    setMessages((prev) => [
      ...prev,
      { id: `local-user-${Date.now()}`, session_id: sessionId ?? '', role: 'user', content, created_at: new Date().toISOString() },
    ])
    try {
      const result = await sendMessage.mutateAsync(content)
      if (result.type === 'plan' && result.plan) {
        setPendingPlan(result.plan)
        setMessages((prev) => [
          ...prev,
          { id: `local-plan-${Date.now()}`, session_id: sessionId ?? '', role: 'assistant', content: '', plan: result.plan, created_at: new Date().toISOString() },
        ])
      } else {
        setMessages((prev) => [
          ...prev,
          { id: `local-assistant-${Date.now()}`, session_id: sessionId ?? '', role: 'assistant', content: result.content ?? '', created_at: new Date().toISOString() },
        ])
      }
    } catch {
      // useSendBuildChatMessage's onError already toasts; the user's own
      // message above stays visible so nothing appears lost.
    }
  }

  const handleConfirm = async () => {
    if (!sessionId) return
    try {
      const result = await confirmPlan.mutateAsync(sessionId)
      toast.success(t('buildWithAI.appCreated', { name: result.app.name }))
      setPendingPlan(null)
      onOpenChange(false)
      navigate(`/apps/${result.app.id}`)
    } catch {
      // useConfirmBuildChatPlan's onError already toasts a generic message;
      // the session stays in_progress server-side (partial-failure
      // handling, AIBC-22) and the drawer stays open so the user can retry
      // the same confirm without losing anything.
    }
  }

  const handleRestart = async () => {
    try {
      const result = await restartSession.mutateAsync()
      setMessages(result.messages)
      setPendingPlan(null)
      setInput('')
    } catch {
      // handled by the hook's onError
    }
  }

  return (
    <FormDrawer
      open={open}
      onOpenChange={onOpenChange}
      title={t('buildWithAI.title')}
      description={t('buildWithAI.description')}
      width={480}
      footer={
        <div className="flex w-full flex-col gap-2">
          <div className="flex items-end gap-2">
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  handleSend()
                }
              }}
              placeholder={t('buildWithAI.inputPlaceholder')}
              rows={2}
              className="flex-1 resize-none"
              disabled={sendMessage.isPending || sessionQuery.isLoading}
            />
            <Button
              onClick={handleSend}
              disabled={!input.trim() || sendMessage.isPending}
              size="icon"
              aria-label={t('buildWithAI.send')}
            >
              {sendMessage.isPending ? (
                <Icon name="progress_activity" size={16} className="animate-spin" />
              ) : (
                <Icon name="send" size={16} />
              )}
            </Button>
          </div>
          <button
            type="button"
            onClick={handleRestart}
            disabled={restartSession.isPending}
            className="self-start text-[11.5px] font-medium underline-offset-2 hover:underline disabled:opacity-50"
            style={{ color: 'var(--text-tertiary)' }}
          >
            {t('buildWithAI.restart')}
          </button>
        </div>
      }
    >
      <div className="flex flex-col gap-3">
        {sessionQuery.isLoading && (
          <p className="text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('buildWithAI.loadingSession')}
          </p>
        )}
        {!sessionQuery.isLoading && messages.length === 0 && (
          <p className="text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('buildWithAI.greeting')}
          </p>
        )}
        {messages.map((m) => (
          <MessageBubble key={m.id} message={m} />
        ))}
        {sendMessage.isPending && (
          <div className="flex items-center gap-2 text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
            <Icon name="progress_activity" size={14} className="animate-spin" />
            {t('buildWithAI.thinking')}
          </div>
        )}
        {pendingPlan && (
          <PlanCard plan={pendingPlan} onConfirm={handleConfirm} confirming={confirmPlan.isPending} />
        )}
        <div ref={bottomRef} />
      </div>
    </FormDrawer>
  )
}

function MessageBubble({ message }: { message: BuildChatMessage }) {
  // A plan-bearing message renders as the plan card below, not a bubble —
  // avoid double-rendering the same turn.
  if (message.plan) return null
  const isUser = message.role === 'user'
  return (
    <div className={cn('flex', isUser ? 'justify-end' : 'justify-start')}>
      <div
        className="max-w-[85%] rounded-[12px] px-3.5 py-2.5 text-[13px] leading-relaxed"
        style={{
          background: isUser ? 'var(--primary)' : 'var(--hover-surface)',
          color: isUser ? '#fff' : 'var(--text-primary)',
        }}
      >
        <ChatMarkdown content={message.content} />
      </div>
    </div>
  )
}

function PlanCard({
  plan,
  onConfirm,
  confirming,
}: {
  plan: BuildChatPlan
  onConfirm: () => void
  confirming: boolean
}) {
  const { t } = useTranslation()
  return (
    <div
      className="flex flex-col gap-3 rounded-[14px] border p-4"
      style={{ background: 'var(--surface)', borderColor: 'var(--primary)' }}
    >
      <div className="flex items-center gap-2">
        <Icon name="auto_awesome" size={16} style={{ color: 'var(--primary)' }} />
        <span className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
          {t('buildWithAI.proposedSetup')}
        </span>
      </div>
      <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
        <strong>{plan.name}</strong>
      </div>
      <div className="flex flex-col gap-1.5">
        {plan.tables.map((table) => (
          <div
            key={table.name}
            className="rounded-[8px] px-2.5 py-1.5 text-[12px]"
            style={{ background: 'var(--hover-surface)' }}
          >
            <span className="font-semibold" style={{ color: 'var(--text-primary)' }}>
              {table.name}
            </span>
            {table.columns.length > 0 && (
              <span style={{ color: 'var(--text-tertiary)' }}>
                {' — '}
                {table.columns.map((c) => c.name).join(', ')}
              </span>
            )}
          </div>
        ))}
      </div>
      <div className="flex items-center gap-1.5 text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
        <Icon name={plan.auth ? 'lock' : 'lock_open'} size={13} />
        {plan.auth ? t('buildWithAI.authEnabled') : t('buildWithAI.authDisabled')}
      </div>
      <Button onClick={onConfirm} disabled={confirming} className="gap-2 self-start">
        {confirming ? (
          <>
            <Icon name="progress_activity" size={14} className="animate-spin" /> {t('buildWithAI.confirming')}
          </>
        ) : (
          <>
            <Icon name="check" size={14} /> {t('buildWithAI.confirmButton')}
          </>
        )}
      </Button>
    </div>
  )
}
