import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

interface ProviderTabItem {
  key: string
  name: string
  icon: string
  disabled?: boolean
  badge?: string
}

interface ProviderTabsProps {
  items: ProviderTabItem[]
  value: string
  onChange?: (key: string) => void
  className?: string
}

/**
 * Horizontal segmented selector for "which provider is active" (code hosting,
 * deploy). One active/selectable entry plus disabled `badge`-marked entries
 * for providers not yet implemented.
 */
export function ProviderTabs({ items, value, onChange, className }: ProviderTabsProps) {
  return (
    <div className={cn('flex flex-wrap gap-2', className)}>
      {items.map((item) => {
        const active = item.key === value
        return (
          <button
            key={item.key}
            type="button"
            disabled={item.disabled}
            onClick={() => !item.disabled && onChange?.(item.key)}
            className={cn(
              'flex items-center gap-2.5 rounded-[10px] border px-4 py-2.5 text-sm font-medium transition-colors',
              item.disabled ? 'cursor-not-allowed opacity-70' : 'cursor-pointer',
            )}
            style={{
              borderColor: active ? 'var(--primary)' : 'var(--border)',
              background: active ? 'var(--primary-tint)' : 'var(--surface)',
              color: active ? 'var(--primary)' : 'var(--text-secondary)',
            }}
          >
            <Icon name={item.icon} size={16} />
            {item.name}
            {item.badge && (
              <span
                className="rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
                style={{ background: 'var(--hover-surface)', color: 'var(--text-tertiary)' }}
              >
                {item.badge}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
