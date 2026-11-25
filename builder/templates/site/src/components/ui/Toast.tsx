import { createContext, useCallback, useContext, useState } from 'react'
import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'

type Tone = 'success' | 'error'
interface ToastItem {
  id: number
  message: string
  tone: Tone
}

const ToastContext = createContext<(message: string, tone?: Tone) => void>(() => {})

// Wrap the app once, then call useToast()("Changes saved") after an action.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])

  const show = useCallback((message: string, tone: Tone = 'success') => {
    const id = Date.now() + Math.random()
    setItems((prev) => [...prev, { id, message, tone }])
    window.setTimeout(() => setItems((prev) => prev.filter((i) => i.id !== id)), 4000)
  }, [])

  return (
    <ToastContext.Provider value={show}>
      {children}
      <div aria-live="polite" className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {items.map((i) => (
          <div
            key={i.id}
            role="status"
            className={cn(
              'rounded-md border bg-surface-raised px-4 py-2 text-sm shadow-card',
              i.tone === 'error' ? 'border-danger text-danger' : 'border-success text-success',
            )}
          >
            {i.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast() {
  return useContext(ToastContext)
}
