import { cn } from "@/lib/utils"

/** Placeholder de carregamento. Usa a superfície de hover como base pulsante. */
function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("animate-pulse rounded-md bg-[var(--hover-surface)]", className)}
      {...props}
    />
  )
}

export { Skeleton }
