import { ReactNode } from 'react'
import { Icon } from '@/components/ui/icon'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

interface StateShellProps {
  icon?: string
  iconTone?: string
  title: string
  description?: ReactNode
  action?: { label: string; onClick: () => void }
  className?: string
}

function StateShell({ icon, iconTone, title, description, action, className }: StateShellProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-3 px-6 py-16 text-center', className)}>
      {icon && (
        <div
          className="flex h-12 w-12 items-center justify-center rounded-2xl"
          style={{ background: 'var(--hover-surface)', color: iconTone ?? 'var(--text-tertiary)' }}
        >
          <Icon name={icon} size={24} />
        </div>
      )}
      <div className="text-base font-semibold text-[var(--text-primary)]">{title}</div>
      {description && <div className="max-w-sm text-sm text-[var(--text-secondary)]">{description}</div>}
      {action && (
        <Button className="mt-1" onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </div>
  )
}

/** Estado vazio (sem dados). */
export function EmptyState(props: Omit<StateShellProps, 'iconTone'>) {
  return <StateShell icon={props.icon ?? 'inbox'} {...props} />
}

/** Estado de erro (fetch falhou). */
export function ErrorState(props: Omit<StateShellProps, 'iconTone'>) {
  return <StateShell icon={props.icon ?? 'error'} iconTone="var(--danger)" {...props} />
}

/** Estado de carregamento — linhas de skeleton. */
export function LoadingState({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-2 p-4', className)}>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}
