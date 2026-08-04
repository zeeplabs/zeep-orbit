import { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface SettingRowProps {
  label: ReactNode
  description?: ReactNode
  /** Controle à direita (Switch, Select, Input, Button...). */
  control?: ReactNode
  className?: string
  children?: ReactNode
}

/**
 * Linha de configuração: label + descrição à esquerda, controle à direita.
 * Fonte única do padrão de Settings/Database/Security — telas não replicam
 * o layout de linha de config.
 */
export function SettingRow({ label, description, control, className, children }: SettingRowProps) {
  return (
    <div className={cn('flex items-start justify-between gap-4 py-4', className)}>
      <div className="min-w-0">
        <div className="text-sm font-medium text-[var(--text-primary)]">{label}</div>
        {description && (
          <div className="mt-0.5 text-[13px] text-[var(--text-secondary)]">{description}</div>
        )}
        {children}
      </div>
      {control && <div className="shrink-0">{control}</div>}
    </div>
  )
}
