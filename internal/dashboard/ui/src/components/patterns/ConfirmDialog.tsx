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

interface ConfirmDialogProps {
  open: boolean
  title: string
  message: ReactNode
  confirmLabel: string
  cancelLabel: string
  /** true = ação destrutiva (delete): ícone/botão em tom danger. */
  destructive?: boolean
  loading?: boolean
  icon?: string
  onConfirm: () => void
  onCancel: () => void
}

/**
 * Diálogo de confirmação genérico e reusável — cobre delete E logout (e qualquer
 * confirmação simples). Título/mensagem adaptam ao que está sendo confirmado.
 */
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  cancelLabel,
  destructive,
  loading,
  icon,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const tone = destructive ? 'var(--danger)' : 'var(--primary)'
  const tint = destructive ? 'var(--danger-tint)' : 'var(--primary-tint)'
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onCancel() }}>
      <DialogContent className="max-w-[420px] gap-0 p-6">
        <DialogHeader className="text-left">
          <div
            className="mb-4 flex h-11 w-11 items-center justify-center rounded-[12px]"
            style={{ background: tint, color: tone }}
          >
            <Icon name={icon ?? (destructive ? 'warning' : 'help')} size={20} />
          </div>
          <DialogTitle className="mb-2 text-base font-bold">{title}</DialogTitle>
          <DialogDescription className="mb-6 text-[13px] leading-relaxed">
            {message}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter className="flex flex-row gap-2.5 sm:justify-start sm:space-x-0">
          <Button variant="outline" className="flex-1" onClick={onCancel} disabled={loading}>
            {cancelLabel}
          </Button>
          <Button
            className="flex-1 text-white"
            style={{ background: tone }}
            onClick={onConfirm}
            disabled={loading}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
