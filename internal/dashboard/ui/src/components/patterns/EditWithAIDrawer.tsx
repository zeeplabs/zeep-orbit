import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FormDrawer } from './FormDrawer'
import { ChatMarkdown } from './ChatMarkdown'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'
import {
  useEditChatSession,
  useSendEditChatMessage,
  useConfirmEditChatOperation,
  useRestartEditChatSession,
  type EditChatMessage,
  type EditOperation,
} from '@/lib/api'

interface EditWithAIDrawerProps {
  appId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

// EditWithAIDrawer — ai-edit-chat T11. Chat scoped to exactly one existing
// app (`appId`), mirroring BuildWithAIDrawer's structure but kept as its
// own component (design.md Tech Decisions: isolate the edit lifecycle from
// the already-shipped creation flow). Unlike the "Proposed setup" batch
// card, this drawer shows one EditOperation at a time, confirmed and
// applied immediately (spec's Confirmation model) — the session never
// completes, only stays in_progress or is abandoned via "Recomeçar".
export function EditWithAIDrawer({ appId, open, onOpenChange }: EditWithAIDrawerProps) {
  const { t } = useTranslation()
  const [input, setInput] = useState('')
  const [messages, setMessages] = useState<EditChatMessage[]>([])
  const [pendingOp, setPendingOp] = useState<EditOperation | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  const sessionQuery = useEditChatSession(appId, open)
  const sendMessage = useSendEditChatMessage(appId)
  const confirmOp = useConfirmEditChatOperation(appId)
  const restartSession = useRestartEditChatSession(appId)

  // Resume: seed local state from the session's persisted history once it
  // loads, including re-deriving a pending operation (if any) so the
  // confirmation card survives a drawer close/reopen (AIEC-01). This must
  // mirror EditChatConfirm's own rule exactly (ai_edit_chat_handlers.go:
  // "last := messages[len(messages)-1]; if len(last.Plan) == 0 { ... error
  // ... }") — only the very last message counts as pending. Scanning
  // backward for *any* message with a plan (as this used to do) resurrects
  // an already-applied operation from earlier in the conversation once a
  // later plain-text turn follows it (e.g. the user asks a question after
  // confirming), showing a stale "Confirmar e aplicar" card that the
  // backend then rejects with "no proposed operation to confirm".
  useEffect(() => {
    if (!sessionQuery.data) return
    setMessages(sessionQuery.data.messages)
    const lastMessage = sessionQuery.data.messages[sessionQuery.data.messages.length - 1]
    setPendingOp(lastMessage?.plan ?? null)
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
      if (result.type === 'edit_op' && result.edit_op) {
        setPendingOp(result.edit_op)
        setMessages((prev) => [
          ...prev,
          { id: `local-op-${Date.now()}`, session_id: sessionId ?? '', role: 'assistant', content: '', plan: result.edit_op, created_at: new Date().toISOString() },
        ])
      } else {
        setMessages((prev) => [
          ...prev,
          { id: `local-assistant-${Date.now()}`, session_id: sessionId ?? '', role: 'assistant', content: result.content ?? '', created_at: new Date().toISOString() },
        ])
      }
    } catch {
      // useSendEditChatMessage's onError already toasts; the user's own
      // message above stays visible so nothing appears lost.
    }
  }

  const handleConfirm = async () => {
    if (!sessionId) return
    try {
      const result = await confirmOp.mutateAsync(sessionId)
      toast.success(t('editWithAI.operationApplied'))
      setPendingOp(null)
      setMessages((prev) => [
        ...prev,
        {
          id: `local-applied-${Date.now()}`,
          session_id: sessionId,
          role: 'assistant',
          content: t('editWithAI.operationApplied'),
          created_at: new Date().toISOString(),
        },
      ])
      void result
    } catch {
      // useConfirmEditChatOperation's onError already toasts a specific
      // validation error when the handler rejected the operation (AIEC-04)
      // or a generic one otherwise — the session stays in_progress
      // server-side and the drawer stays open so the user can retry.
    }
  }

  const handleRestart = async () => {
    try {
      const result = await restartSession.mutateAsync()
      setMessages(result.messages)
      setPendingOp(null)
      setInput('')
    } catch {
      // handled by the hook's onError
    }
  }

  return (
    <FormDrawer
      open={open}
      onOpenChange={onOpenChange}
      title={t('editWithAI.title')}
      description={t('editWithAI.description')}
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
              placeholder={t('editWithAI.inputPlaceholder')}
              rows={2}
              className="flex-1 resize-none"
              disabled={sendMessage.isPending || sessionQuery.isLoading}
            />
            <Button
              onClick={handleSend}
              disabled={!input.trim() || sendMessage.isPending}
              size="icon"
              aria-label={t('editWithAI.send')}
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
            {t('editWithAI.restart')}
          </button>
        </div>
      }
    >
      <div className="flex flex-col gap-3">
        {sessionQuery.isLoading && (
          <p className="text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('editWithAI.loadingSession')}
          </p>
        )}
        {!sessionQuery.isLoading && messages.length === 0 && (
          <p className="text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('editWithAI.greeting')}
          </p>
        )}
        {messages.map((m) => (
          <MessageBubble key={m.id} message={m} />
        ))}
        {sendMessage.isPending && (
          <div className="flex items-center gap-2 text-[12.5px]" style={{ color: 'var(--text-tertiary)' }}>
            <Icon name="progress_activity" size={14} className="animate-spin" />
            {t('editWithAI.thinking')}
          </div>
        )}
        {pendingOp && (
          <OperationCard operation={pendingOp} onConfirm={handleConfirm} confirming={confirmOp.isPending} />
        )}
        <div ref={bottomRef} />
      </div>
    </FormDrawer>
  )
}

// EDIT_OP_APPLIED_MARKER mirrors the backend's editChatAppliedMarker
// (ai_edit_chat_handlers.go) verbatim — the no-op sentinel EditChatConfirm
// persists as the assistant's message content right after applying an
// operation, so a repeat confirm on the same session recognizes "already
// applied" instead of re-running the mutation (AIEC-16). On a fresh send
// this raw marker never reaches the UI (handleConfirm appends its own
// locally-translated "operationApplied" message instead), but reloading
// history (session resume, AIEC-01) fetches the persisted row as-is — so
// MessageBubble must recognize the same literal string here or it renders
// as raw text (worse: markdown turns its double underscores into bold).
const EDIT_OP_APPLIED_MARKER = '__edit_op_applied__'

function MessageBubble({ message }: { message: EditChatMessage }) {
  const { t } = useTranslation()
  // An operation-bearing message renders as the operation card below, not
  // a bubble — avoid double-rendering the same turn.
  if (message.plan) return null
  const isUser = message.role === 'user'
  const content = message.content === EDIT_OP_APPLIED_MARKER ? t('editWithAI.operationApplied') : message.content
  return (
    <div className={cn('flex', isUser ? 'justify-end' : 'justify-start')}>
      <div
        className="max-w-[85%] rounded-[12px] px-3.5 py-2.5 text-[13px] leading-relaxed"
        style={{
          background: isUser ? 'var(--primary)' : 'var(--hover-surface)',
          color: isUser ? '#fff' : 'var(--text-primary)',
        }}
      >
        <ChatMarkdown content={content} />
      </div>
    </div>
  )
}

// operationSummary renders a one-line, human-readable description of a
// single EditOperation — exactly one of the operation's optional fields is
// populated, matching `kind` (mirrors the Go EditOperation contract).
function OperationSummary({ operation }: { operation: EditOperation }) {
  const { t } = useTranslation()
  switch (operation.kind) {
    case 'add_table':
      return (
        <>
          <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
            <strong>{operation.add_table?.name}</strong>
          </div>
          <div className="flex flex-col gap-1.5">
            {operation.add_table?.columns.map((c) => (
              <div key={c.name} className="rounded-[8px] px-2.5 py-1.5 text-[12px]" style={{ background: 'var(--hover-surface)' }}>
                <span className="font-semibold" style={{ color: 'var(--text-primary)' }}>{c.name}</span>
                <span style={{ color: 'var(--text-tertiary)' }}> — {c.type}</span>
              </div>
            ))}
          </div>
        </>
      )
    case 'add_column':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          <div>
            {t('editWithAI.opAddColumn', {
              table: operation.add_column?.table,
              column: operation.add_column?.column.name,
              type: operation.add_column?.column.type,
            })}
          </div>
          {operation.add_column?.column.type === 'enum' && (operation.add_column?.column.allowed_values?.length ?? 0) > 0 && (
            <div className="opacity-70">
              {t('editWithAI.opAddColumnEnumValues', {
                values: operation.add_column!.column.allowed_values!.join(', '),
              })}
            </div>
          )}
        </div>
      )
    case 'add_index':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          {t('editWithAI.opAddIndex', {
            table: operation.add_index?.table,
            name: operation.add_index?.name,
            columns: operation.add_index?.columns.join(', '),
          })}
        </div>
      )
    case 'add_reference':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          {t('editWithAI.opAddReference', {
            table: operation.add_reference?.table,
            column: operation.add_reference?.column.name,
            refTable: operation.add_reference?.ref_table,
            refColumn: operation.add_reference?.ref_column,
          })}
        </div>
      )
    case 'add_foreign_key':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          {t('editWithAI.opAddForeignKey', {
            table: operation.add_foreign_key?.table,
            column: operation.add_foreign_key?.column,
            refTable: operation.add_foreign_key?.ref_table,
            refColumn: operation.add_foreign_key?.ref_column,
          })}
        </div>
      )
    case 'remove_foreign_key':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          {t('editWithAI.opRemoveForeignKey', {
            table: operation.remove_foreign_key?.table,
            column: operation.remove_foreign_key?.column,
          })}
        </div>
      )
    case 'set_rls_mode':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          {t('editWithAI.opSetRlsMode', {
            table: operation.set_rls_mode?.table,
            mode: operation.set_rls_mode?.mode,
          })}
        </div>
      )
    case 'toggle_auth':
      return (
        <div className="text-[13px]" style={{ color: 'var(--text-primary)' }}>
          {operation.toggle_auth?.email_enabled ? t('editWithAI.opEnableAuth') : t('editWithAI.opDisableAuth')}
        </div>
      )
    default:
      return null
  }
}

function OperationCard({
  operation,
  onConfirm,
  confirming,
}: {
  operation: EditOperation
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
          {t('editWithAI.proposedChange')}
        </span>
      </div>
      <OperationSummary operation={operation} />
      <Button onClick={onConfirm} disabled={confirming} className="gap-2 self-start">
        {confirming ? (
          <>
            <Icon name="progress_activity" size={14} className="animate-spin" /> {t('editWithAI.confirming')}
          </>
        ) : (
          <>
            <Icon name="check" size={14} /> {t('editWithAI.confirmButton')}
          </>
        )}
      </Button>
    </div>
  )
}
