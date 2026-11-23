import { Tabs as BaseTabs } from '@base-ui/react/tabs'
import type { ReactNode } from 'react'

export interface TabItem {
  id: string
  label: string
  content: ReactNode
}

// Base UI tabs: arrow-key navigation and tab/tabpanel roles included.
export function Tabs({ items, initial }: { items: TabItem[]; initial?: string }) {
  return (
    <BaseTabs.Root defaultValue={initial ?? items[0]?.id}>
      <BaseTabs.List activateOnFocus className="flex gap-1 border-b border-border">
        {items.map((item) => (
          <BaseTabs.Tab
            key={item.id}
            value={item.id}
            className="-mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium text-muted-foreground transition hover:text-foreground data-[active]:border-primary data-[active]:text-foreground"
          >
            {item.label}
          </BaseTabs.Tab>
        ))}
      </BaseTabs.List>
      {items.map((item) => (
        <BaseTabs.Panel key={item.id} value={item.id} className="pt-4">
          {item.content}
        </BaseTabs.Panel>
      ))}
    </BaseTabs.Root>
  )
}
