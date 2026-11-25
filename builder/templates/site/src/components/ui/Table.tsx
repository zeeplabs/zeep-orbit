import type { HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from 'react'
import { cn } from '../../lib/cn'

// Wrapper scrolls on narrow screens instead of breaking the page layout.
export function Table({ className, ...rest }: HTMLAttributes<HTMLTableElement>) {
  return (
    <div className="overflow-x-auto rounded-lg border border-border bg-surface">
      <table className={cn('w-full border-collapse text-left text-sm', className)} {...rest} />
    </div>
  )
}

export function Th({ className, numeric, ...rest }: ThHTMLAttributes<HTMLTableCellElement> & { numeric?: boolean }) {
  return (
    <th
      className={cn('border-b border-border px-4 py-2.5 font-medium text-muted-foreground', numeric && 'text-right', className)}
      {...rest}
    />
  )
}

export function Td({ className, numeric, ...rest }: TdHTMLAttributes<HTMLTableCellElement> & { numeric?: boolean }) {
  return (
    <td
      className={cn('border-b border-border px-4 py-2.5 last:border-b-0', numeric && 'text-right tabular-nums', className)}
      {...rest}
    />
  )
}
