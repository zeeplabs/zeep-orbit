import { Select as BaseSelect } from '@base-ui/react/select'
import { Check, ChevronsUpDown } from 'lucide-react'

export interface SelectOption {
  label: string
  value: string
}

// Base UI select: typeahead, arrow keys and screen reader roles included.
// Give it the same `id` as its FormField so the label points at the trigger.
export function Select({
  id,
  name,
  items,
  value,
  defaultValue,
  onValueChange,
  placeholder = 'Select',
  disabled,
}: {
  id?: string
  name?: string
  items: SelectOption[]
  value?: string
  defaultValue?: string
  onValueChange?: (value: string) => void
  placeholder?: string
  disabled?: boolean
}) {
  return (
    <BaseSelect.Root
      items={items}
      name={name}
      value={value}
      defaultValue={defaultValue}
      disabled={disabled}
      onValueChange={(next) => {
        if (typeof next === 'string') onValueChange?.(next)
      }}
    >
      <BaseSelect.Trigger
        id={id}
        className="flex h-10 w-full items-center justify-between gap-2 rounded-md border border-border bg-surface px-3 text-sm text-foreground data-[disabled]:opacity-50"
      >
        <BaseSelect.Value placeholder={placeholder} className="data-[placeholder]:text-muted-foreground" />
        <BaseSelect.Icon>
          <ChevronsUpDown size={14} aria-hidden="true" className="text-muted-foreground" />
        </BaseSelect.Icon>
      </BaseSelect.Trigger>
      <BaseSelect.Portal>
        <BaseSelect.Positioner sideOffset={4} className="z-50 select-none outline-none">
          <BaseSelect.Popup className="min-w-[var(--anchor-width)] origin-[var(--transform-origin)] rounded-md border border-border bg-surface-raised py-1 text-sm text-foreground shadow-card transition duration-100 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 data-[ending-style]:scale-95 data-[ending-style]:opacity-0">
            <BaseSelect.List className="max-h-[var(--available-height)] overflow-y-auto">
              {items.map((item) => (
                <BaseSelect.Item
                  key={item.value}
                  value={item.value}
                  className="grid cursor-default grid-cols-[1rem_1fr] items-center gap-2 px-3 py-2 data-[highlighted]:bg-primary data-[highlighted]:text-primary-foreground"
                >
                  <BaseSelect.ItemIndicator className="col-start-1">
                    <Check size={14} aria-hidden="true" />
                  </BaseSelect.ItemIndicator>
                  <BaseSelect.ItemText className="col-start-2">{item.label}</BaseSelect.ItemText>
                </BaseSelect.Item>
              ))}
            </BaseSelect.List>
          </BaseSelect.Popup>
        </BaseSelect.Positioner>
      </BaseSelect.Portal>
    </BaseSelect.Root>
  )
}
