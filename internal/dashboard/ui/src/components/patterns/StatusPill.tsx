import { cn } from '@/lib/utils'

export type StatusTone = 'success' | 'warning' | 'danger' | 'neutral' | 'primary'

const TONE: Record<StatusTone, { fg: string; bg: string }> = {
  success: { fg: 'var(--success)', bg: 'var(--success-tint)' },
  warning: { fg: 'var(--warning)', bg: 'var(--warning-tint)' },
  danger: { fg: 'var(--danger)', bg: 'var(--danger-tint)' },
  primary: { fg: 'var(--primary)', bg: 'var(--primary-tint)' },
  neutral: { fg: 'var(--text-secondary)', bg: 'var(--hover-surface)' },
}

interface StatusPillProps {
  label: string
  tone?: StatusTone
  /** Mostra o ponto colorido antes do label. */
  dot?: boolean
  className?: string
}

/**
 * Pílula de status reusável (Active/Trial/Revoked/Paused/Inactive...).
 * Fonte única de verdade dos badges de status — consumido por License,
 * 2FA, Observability, Users, etc. Semântica de cor vem do tom, não da tela.
 */
export function StatusPill({ label, tone = 'neutral', dot = true, className }: StatusPillProps) {
  const { fg, bg } = TONE[tone]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
        className
      )}
      style={{ color: fg, background: bg }}
    >
      {dot && <span className="h-1.5 w-1.5 rounded-full" style={{ background: fg }} />}
      {label}
    </span>
  )
}
