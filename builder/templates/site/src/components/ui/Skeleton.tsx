import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/cn'

// Placeholder shaped like the real content; size it with className (h-4 w-40).
export function Skeleton({ className, ...rest }: HTMLAttributes<HTMLDivElement>) {
  return <div aria-hidden="true" className={cn('animate-pulse rounded-md bg-border', className)} {...rest} />
}
