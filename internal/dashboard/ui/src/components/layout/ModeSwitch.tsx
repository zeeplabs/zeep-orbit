import { useId } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Switch } from '@/components/ui/switch'
import { writeMode } from '@/lib/mode'
import { cn } from '@/lib/utils'

/**
 * "Modo avançado" switch (HCH-08). `advanced` says which interface renders it:
 * the advanced shell passes true, the simple shell false, whatever the saved
 * preference is (a deep link renders the advanced shell in simple mode, HCH-11).
 * Toggling saves the preference and goes to `/` (back to simple) or `/apps`.
 */
export function ModeSwitch({
  advanced,
  className,
  labelClassName,
}: {
  advanced: boolean
  className?: string
  /** Lets a narrow rail hide the visible label; the accessible name stays. */
  labelClassName?: string
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const id = useId()
  const label = t('nav.simple.advancedMode')

  function onCheckedChange(next: boolean) {
    writeMode(next ? 'advanced' : 'simple')
    navigate(next ? '/apps' : '/')
  }

  return (
    <div className={cn('flex items-center gap-2.5', className)}>
      <Switch
        id={id}
        checked={advanced}
        onCheckedChange={onCheckedChange}
        aria-label={label}
      />
      <label
        htmlFor={id}
        className={cn('cursor-pointer text-[13px] text-[var(--text-secondary)]', labelClassName)}
      >
        {label}
      </label>
    </div>
  )
}
