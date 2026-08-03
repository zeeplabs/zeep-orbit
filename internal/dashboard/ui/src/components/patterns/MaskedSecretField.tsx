import { useState } from 'react'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

interface MaskedSecretFieldProps {
  /** true = já existe um segredo salvo (mostra máscara + "Replace"). */
  hasValue: boolean
  /** Máscara exibida quando há valor salvo (ex: "••••••••1234"). */
  maskedHint?: string
  placeholder?: string
  replaceLabel: string
  cancelLabel: string
  /** Chamado com o novo valor ao confirmar a substituição. */
  onSave: (value: string) => void
  className?: string
}

/**
 * Campo de segredo mascarado (API key). Write-only: valor salvo nunca é
 * re-exibido — só máscara + botão "Replace" que abre input pra novo valor.
 * Consumido por Observability e License. Ver specs correspondentes.
 */
export function MaskedSecretField({
  hasValue,
  maskedHint = '••••••••••••',
  placeholder,
  replaceLabel,
  cancelLabel,
  onSave,
  className,
}: MaskedSecretFieldProps) {
  const [editing, setEditing] = useState(!hasValue)
  const [value, setValue] = useState('')

  if (!editing) {
    return (
      <div className={cn('flex items-center gap-2', className)}>
        <code className="flex-1 rounded-lg border border-[var(--border)] bg-[var(--sunken)] px-3 py-2 font-mono text-sm text-[var(--text-secondary)]">
          {maskedHint}
        </code>
        <Button variant="outline" size="sm" onClick={() => { setValue(''); setEditing(true) }}>
          <Icon name="key" size={16} />
          {replaceLabel}
        </Button>
      </div>
    )
  }

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <Input
        type="password"
        autoComplete="off"
        placeholder={placeholder}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        className="flex-1 font-mono"
      />
      <Button
        size="sm"
        disabled={!value}
        onClick={() => { onSave(value); setValue(''); if (hasValue) setEditing(false) }}
      >
        <Icon name="check" size={16} />
      </Button>
      {hasValue && (
        <Button variant="outline" size="sm" onClick={() => { setValue(''); setEditing(false) }}>
          {cancelLabel}
        </Button>
      )}
    </div>
  )
}
