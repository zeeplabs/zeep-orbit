import { ReactNode } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

/**
 * Badge "Enterprise" reusável para sinalizar features gated. Diferente do
 * RoleGate (que omite): aqui a feature APARECE com badge, pra conversão.
 * Consumido por License, Observability e futuros. Ver spec enterprise-licensing.
 */
export function EnterpriseBadge({ className, onClick }: { className?: string; onClick?: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
        onClick && 'cursor-pointer hover:opacity-90',
        className
      )}
      style={{ borderColor: 'var(--accent)', color: 'var(--accent)', background: 'var(--accent-tint)' }}
    >
      <Icon name="workspace_premium" size={12} />
      Enterprise
    </button>
  )
}

interface UpgradeModalProps {
  open: boolean
  feature: string
  description?: ReactNode
  confirmLabel: string
  cancelLabel: string
  onUpgrade: () => void
  onClose: () => void
}

/** Modal de upgrade reusável, disparado por features gated. */
export function UpgradeModal({
  open,
  feature,
  description,
  confirmLabel,
  cancelLabel,
  onUpgrade,
  onClose,
}: UpgradeModalProps) {
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader className="text-left">
          <div
            className="mb-4 flex h-11 w-11 items-center justify-center rounded-xl"
            style={{ background: 'var(--accent-tint)', color: 'var(--accent)' }}
          >
            <Icon name="workspace_premium" size={22} />
          </div>
          <DialogTitle className="mb-2">{feature}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <DialogFooter className="flex flex-row gap-2.5 sm:justify-start sm:space-x-0">
          <Button variant="outline" className="flex-1" onClick={onClose}>
            {cancelLabel}
          </Button>
          <Button
            className="flex-1 text-white"
            style={{ background: 'var(--accent)' }}
            onClick={onUpgrade}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
