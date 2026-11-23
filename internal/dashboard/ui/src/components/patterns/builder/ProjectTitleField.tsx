import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'

/** Matches the server's limit (builderMaxTitleRunes). */
export const PROJECT_TITLE_MAX_LENGTH = 120

interface ProjectTitleFieldProps {
  /** The title as the server knows it. */
  value: string
  /** Called with the trimmed new title when it is non-empty and different from `value`. */
  onSave: (next: string) => void
  /** True while a save is in flight: the field stays read-only. */
  saving?: boolean
  /** `heading` renders like the page title; `inline` is a compact row inside a card. */
  variant?: 'heading' | 'inline'
}

/**
 * The project's name with an edit button. Enter or the check button saves, Escape or the
 * cross cancels; leaving the field saves, like a rename in a file list. An empty or unchanged
 * value never calls onSave.
 */
export function ProjectTitleField({ value, onSave, saving = false, variant = 'heading' }: ProjectTitleFieldProps) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value)
  const inputRef = useRef<HTMLInputElement>(null)
  const cancelled = useRef(false)

  useEffect(() => {
    if (editing) inputRef.current?.select()
  }, [editing])

  function start() {
    cancelled.current = false
    setDraft(value)
    setEditing(true)
  }

  function commit() {
    if (cancelled.current) return
    const next = draft.trim()
    setEditing(false)
    if (next && next !== value) onSave(next)
  }

  function cancel() {
    cancelled.current = true
    setEditing(false)
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      commit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancel()
    }
  }

  const heading = variant === 'heading'

  if (editing) {
    return (
      <div className="flex min-w-0 items-center gap-1.5">
        <Input
          ref={inputRef}
          value={draft}
          maxLength={PROJECT_TITLE_MAX_LENGTH}
          aria-label={t('builder.page.title.label')}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={commit}
          className={heading ? 'h-10 text-[18px] font-bold sm:text-[20px]' : 'h-9 text-[13px]'}
        />
        <button
          type="button"
          aria-label={t('builder.page.title.save')}
          // mousedown keeps the input from blurring first, which would commit twice.
          onMouseDown={(e) => e.preventDefault()}
          onClick={commit}
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-[8px] hover:bg-[var(--hover-surface)]"
          style={{ color: 'var(--text-secondary)' }}
        >
          <Icon name="check" size={16} />
        </button>
        <button
          type="button"
          aria-label={t('builder.page.title.cancel')}
          onMouseDown={(e) => e.preventDefault()}
          onClick={cancel}
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-[8px] hover:bg-[var(--hover-surface)]"
          style={{ color: 'var(--text-secondary)' }}
        >
          <Icon name="close" size={16} />
        </button>
      </div>
    )
  }

  return (
    <div className="group flex min-w-0 items-center gap-1.5">
      {heading ? (
        <h1
          className="truncate text-[22px] font-bold leading-tight text-[var(--text-primary)] sm:text-[26px]"
          style={{ fontFamily: 'var(--font-display)' }}
        >
          {value}
        </h1>
      ) : (
        <span className="truncate text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
          {value}
        </span>
      )}
      <button
        type="button"
        aria-label={t('builder.page.title.edit')}
        disabled={saving}
        onClick={start}
        className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-[8px] opacity-60 hover:bg-[var(--hover-surface)] hover:opacity-100 focus-visible:opacity-100 disabled:opacity-30"
        style={{ color: 'var(--text-secondary)' }}
      >
        <Icon name="edit" size={16} />
      </button>
    </div>
  )
}
