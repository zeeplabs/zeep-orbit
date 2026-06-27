import { AlertTriangle } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

interface Props {
  open: boolean
  appName: string
  loading: boolean
  onConfirm: () => void
  onCancel: () => void
}

export default function DeleteConfirmDialog({ open, appName, loading, onConfirm, onCancel }: Props) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => { if (!isOpen) onCancel() }}>
      <DialogContent className="max-w-[420px] bg-white/[0.04] border-white/[0.08] text-[#F8FAFC] backdrop-blur-md rounded-2xl p-0 gap-0 [&>button]:text-[#94A3B8] [&>button]:hover:text-[#F8FAFC] [&>button]:hover:bg-white/[0.08]">
        {/* inner bezel */}
        <div className="bg-white/[0.03] shadow-[inset_0_1px_1px_rgba(255,255,255,0.08)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
          <DialogHeader className="mb-0">
            {/* icon */}
            <div className="w-11 h-11 rounded-xl bg-red-500/[0.12] border border-red-500/[0.20] flex items-center justify-center mb-[18px]">
              <AlertTriangle size={20} strokeWidth={1.5} className="text-red-500" />
            </div>

            <DialogTitle className="text-base font-bold text-[#F8FAFC] mb-2">
              Deletar app &ldquo;{appName}&rdquo;?
            </DialogTitle>

            <DialogDescription className="text-[13px] text-[#94A3B8] leading-relaxed mb-6">
              Esta ação remove o app do dashboard. As tabelas no banco{' '}
              <strong className="text-[#F8FAFC]">NÃO serão deletadas</strong>.
            </DialogDescription>
          </DialogHeader>

          <DialogFooter className="flex flex-row gap-2.5 sm:flex-row sm:justify-start sm:space-x-0">
            <Button
              variant="outline"
              onClick={onCancel}
              disabled={loading}
              className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.05] text-[#94A3B8] hover:bg-white/[0.08] hover:text-[#F8FAFC] hover:border-white/[0.10] font-medium"
            >
              Cancelar
            </Button>
            <Button
              onClick={onConfirm}
              disabled={loading}
              className="flex-1 rounded-xl bg-red-500 hover:bg-red-600 text-white font-semibold border-0 disabled:bg-red-500/40"
            >
              {loading ? 'Deletando...' : 'Deletar'}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}
