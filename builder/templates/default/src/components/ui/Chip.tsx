import type { ButtonHTMLAttributes } from 'react'
import { cn } from '../../lib/cn'

export interface ChipProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'aria-pressed'> {
  /** Selected state of a filter chip. Exposed to assistive tech as aria-pressed. */
  selected?: boolean
}

// A toggle for filters and categories ("All", "Fiction", "Open"). Use Badge for
// read-only status and Tabs for switching views; a Chip filters what is already on screen.
export function Chip({ selected = false, className, type = 'button', ...rest }: ChipProps) {
  return (
    <button
      type={type}
      aria-pressed={selected}
      className={cn(
        'inline-flex h-8 items-center rounded-full border px-3 text-sm font-medium transition active:scale-[0.98] disabled:pointer-events-none disabled:opacity-50',
        selected
          ? 'border-primary bg-primary text-primary-foreground'
          : 'border-border bg-surface text-foreground hover:bg-background',
        className,
      )}
      {...rest}
    />
  )
}
