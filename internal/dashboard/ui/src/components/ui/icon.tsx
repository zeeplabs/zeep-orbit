import { CSSProperties } from 'react'
import { cn } from '@/lib/utils'

export interface IconProps {
  /** Nome do glyph Material Symbols Rounded (ex: "settings", "delete", "add"). */
  name: string
  /** Tamanho em px (font-size). Handoff usa 15-20px. Default 18. */
  size?: number
  /** Preenchimento do símbolo (0 = outline, 1 = filled). */
  fill?: 0 | 1
  /** Peso do traço (100-700). */
  weight?: number
  className?: string
  style?: CSSProperties
  'aria-label'?: string
}

/**
 * Wrapper único para ícones Material Symbols Rounded (redesign).
 * Cor herda de `currentColor`. Substitui `lucide-react` de forma incremental,
 * uma tela por vez (ver spec dashboard-redesign, DRD-40/41/42).
 */
export function Icon({
  name,
  size = 18,
  fill = 0,
  weight = 400,
  className,
  style,
  'aria-label': ariaLabel,
}: IconProps) {
  return (
    <span
      className={cn('material-symbols-rounded', className)}
      aria-hidden={ariaLabel ? undefined : true}
      aria-label={ariaLabel}
      role={ariaLabel ? 'img' : undefined}
      style={{
        fontSize: size,
        // opsz alinha com o tamanho pra o glyph ficar proporcional
        fontVariationSettings: `'FILL' ${fill}, 'wght' ${weight}, 'GRAD' 0, 'opsz' ${Math.min(48, Math.max(20, size))}`,
        ...style,
      }}
    >
      {name}
    </span>
  )
}

export default Icon
