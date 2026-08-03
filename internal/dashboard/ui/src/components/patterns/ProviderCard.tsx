import { ReactNode } from 'react'
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from '@/components/ui/accordion'
import { Icon } from '@/components/ui/icon'
import { StatusPill, StatusTone } from './StatusPill'
import { cn } from '@/lib/utils'

interface ProviderCardProps {
  name: string
  /** Material Symbols glyph ou nó custom (ex: logo). */
  icon?: string
  logo?: ReactNode
  description?: string
  /** Pílula de status à direita (ex: Active / Paused). */
  status?: { label: string; tone?: StatusTone }
  /** Badge textual (ex: "SOON") para providers futuros. */
  badge?: string
  /** Desabilita expansão/config (provider futuro). */
  disabled?: boolean
  defaultOpen?: boolean
  children?: ReactNode
  className?: string
}

/**
 * Card de provider em accordion — um por provider (login/storage/observability/
 * deploy), expande pra configurar. Fonte única do padrão accordion-of-cards.
 * Providers futuros: `disabled` + `badge="SOON"`.
 */
export function ProviderCard({
  name,
  icon,
  logo,
  description,
  status,
  badge,
  disabled,
  defaultOpen,
  children,
  className,
}: ProviderCardProps) {
  const header = (
    <div className="flex flex-1 items-center gap-3">
      <div
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg"
        style={{ background: 'var(--hover-surface)', color: 'var(--text-secondary)' }}
      >
        {logo ?? (icon && <Icon name={icon} size={20} />)}
      </div>
      <div className="min-w-0 text-left">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-[var(--text-primary)]">{name}</span>
          {badge && (
            <span className="rounded-full bg-[var(--hover-surface)] px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">
              {badge}
            </span>
          )}
        </div>
        {description && (
          <div className="truncate text-xs text-[var(--text-secondary)]">{description}</div>
        )}
      </div>
      {status && <StatusPill label={status.label} tone={status.tone} className="ml-auto" />}
    </div>
  )

  if (disabled) {
    return (
      <div
        className={cn(
          'flex items-center gap-3 rounded-xl border border-[var(--border)] bg-[var(--surface)] px-4 py-3.5 opacity-60',
          className
        )}
      >
        {header}
      </div>
    )
  }

  return (
    <Accordion
      type="single"
      collapsible
      defaultValue={defaultOpen ? 'item' : undefined}
      className={className}
    >
      <AccordionItem value="item">
        <AccordionTrigger>{header}</AccordionTrigger>
        <AccordionContent>{children}</AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}
