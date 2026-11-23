import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'

// Native <dialog>: focus trap, Escape and the backdrop come from the browser.
export function Dialog({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (open && !el.open) el.showModal()
    if (!open && el.open) el.close()
  }, [open])

  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose()
      }}
      aria-labelledby="dialog-title"
      className="m-auto w-[min(32rem,calc(100vw-2rem))] rounded-lg border border-border bg-surface-raised p-0 text-foreground shadow-card backdrop:bg-black/40"
    >
      <div className="p-5">
        <h2 id="dialog-title" className="mb-3 text-lg font-semibold">
          {title}
        </h2>
        {children}
      </div>
    </dialog>
  )
}
