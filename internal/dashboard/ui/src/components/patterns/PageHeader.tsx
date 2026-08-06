import { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface Crumb {
  label: string
  href?: string
}

interface PageHeaderProps {
  title: string
  subtitle?: ReactNode
  actions?: ReactNode
  breadcrumb?: Crumb[]
  className?: string
}

/**
 * Cabeçalho de página padrão: título display + subtítulo + ações + breadcrumb.
 * Fonte única do bloco de topo — telas não replicam o markup de header.
 */
export function PageHeader({ title, subtitle, actions, breadcrumb, className }: PageHeaderProps) {
  return (
    <div className={cn('mb-6 flex flex-wrap items-start justify-between gap-4', className)}>
      <div className="min-w-0">
        {breadcrumb && breadcrumb.length > 0 && (
          <nav className="mb-1.5 flex items-center gap-1.5 text-xs text-[var(--text-tertiary)]">
            {breadcrumb.map((c, i) => (
              <span key={i} className="flex items-center gap-1.5">
                {i > 0 && <span>/</span>}
                {c.href ? (
                  <a href={c.href} className="hover:text-[var(--text-secondary)]">
                    {c.label}
                  </a>
                ) : (
                  <span>{c.label}</span>
                )}
              </span>
            ))}
          </nav>
        )}
        <h1
          className="truncate text-[22px] font-bold leading-tight text-[var(--text-primary)] sm:text-[26px]"
          style={{ fontFamily: 'var(--font-display)' }}
        >
          {title}
        </h1>
        {subtitle && <p className="mt-1 text-sm text-[var(--text-secondary)]">{subtitle}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}
