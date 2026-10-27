import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'
import type { ActivityEntry, ActivityTone } from './builderModel'

const TONE_COLOR: Record<ActivityTone, string> = {
  neutral: 'var(--text-tertiary)',
  success: 'var(--success-text)',
  warning: 'var(--warning-text)',
  danger: 'var(--danger-text)',
}

interface ActivityTimelineProps {
  entries: ActivityEntry[]
  /** The turn is still working and this is its newest group: open by default. */
  live: boolean
}

/**
 * Compact, collapsible list of what the assistant did between two messages (tool calls, file
 * writes, builds, fix attempts). Open while the turn is live, collapses when it finishes.
 */
export function ActivityTimeline({ entries, live }: ActivityTimelineProps) {
  const { t } = useTranslation()
  const regionId = useId()
  const [open, setOpen] = useState(live)

  // Collapse once the turn stops being live; the user can reopen it.
  const wasLive = useRef(live)
  useEffect(() => {
    if (wasLive.current && !live) setOpen(false)
    wasLive.current = live
  }, [live])

  const hasFailure = entries.some((e) => e.tone === 'danger')

  return (
    <div
      className="rounded-[10px] border text-[12.5px]"
      style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls={regionId}
        className="flex w-full items-center gap-2 rounded-[10px] px-3 py-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        style={{ color: 'var(--text-secondary)' }}
      >
        {live ? (
          <Icon name="progress_activity" size={14} className="animate-spin" />
        ) : (
          <Icon
            name={hasFailure ? 'error' : 'bolt'}
            size={14}
            style={{ color: hasFailure ? 'var(--danger-text)' : undefined }}
          />
        )}
        <span className="flex-1 font-medium">
          {t('builder.page.activity.summary', { count: entries.length })}
        </span>
        <Icon name={open ? 'expand_less' : 'expand_more'} size={16} />
      </button>
      {open && (
        <ol id={regionId} className="flex flex-col gap-1.5 border-t px-3 py-2" style={{ borderColor: 'var(--border)' }}>
          {entries.map((entry) => (
            <TimelineRow key={entry.id} entry={entry} />
          ))}
        </ol>
      )}
    </div>
  )
}

function TimelineRow({ entry }: { entry: ActivityEntry }) {
  const { t } = useTranslation()
  const [showDetail, setShowDetail] = useState(false)
  return (
    <li className="flex flex-col gap-1">
      <div className="flex items-start gap-2" style={{ color: 'var(--text-secondary)' }}>
        <Icon name={entry.icon} size={14} className="mt-[2px] shrink-0" style={{ color: TONE_COLOR[entry.tone] }} />
        <span className="min-w-0 break-words">{t(`builder.page.activity.${entry.labelKey}`, entry.params)}</span>
        {entry.detail && (
          <button
            type="button"
            onClick={() => setShowDetail((v) => !v)}
            aria-expanded={showDetail}
            className="ml-auto shrink-0 rounded-[6px] px-1.5 text-[11.5px] underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            style={{ color: 'var(--text-tertiary)' }}
          >
            {t('builder.page.activity.buildOutput')}
          </button>
        )}
      </div>
      {entry.detail && showDetail && (
        <pre
          className={cn('max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-[8px] p-2 font-mono text-[11px]')}
          style={{ background: 'var(--hover-surface)', color: 'var(--text-secondary)' }}
        >
          {entry.detail}
        </pre>
      )}
    </li>
  )
}
