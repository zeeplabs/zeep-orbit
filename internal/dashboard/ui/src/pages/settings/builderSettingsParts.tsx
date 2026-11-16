import type { ReactNode } from 'react'
import { Icon } from '@/components/ui/icon'

// Layout pieces shared by the AI Builder settings sections.

export function SectionCard({
  title,
  description,
  action,
  children,
}: {
  title: string
  description?: string
  /** Optional control aligned to the right of the title (for example an upload button). */
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section
      className="flex flex-col gap-5 rounded-[14px] border p-6"
      style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-[15px] font-bold" style={{ color: 'var(--text-primary)' }}>
            {title}
          </h2>
          {description && (
            <p className="mt-0.5 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
              {description}
            </p>
          )}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

export function Notice({ tone, children }: { tone: 'warning' | 'danger'; children: ReactNode }) {
  const color = tone === 'warning' ? 'var(--warning-text)' : 'var(--danger)'
  const bg = tone === 'warning' ? 'var(--warning-tint)' : 'var(--danger-tint)'
  return (
    <div
      role="status"
      className="flex items-start gap-2 rounded-[10px] px-3 py-2 text-[12.5px]"
      style={{ background: bg, color }}
    >
      <Icon name="warning" size={16} className="mt-px shrink-0" />
      <div>{children}</div>
    </div>
  )
}
