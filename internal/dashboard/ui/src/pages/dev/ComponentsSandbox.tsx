import { useState } from 'react'
import { useTheme } from '@/lib/theme'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import { Icon } from '@/components/ui/icon'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import {
  PageHeader,
  DataTable,
  Column,
  StatusPill,
  EmptyState,
  ConfirmDialog,
  ProviderCard,
  SettingRow,
  EnterpriseBadge,
  UpgradeModal,
  MaskedSecretField,
  FormDrawer,
} from '@/components/patterns'

interface DemoRow { id: string; name: string; status: string }
const DEMO_ROWS: DemoRow[] = [
  { id: '1', name: 'acme-prod', status: 'active' },
  { id: '2', name: 'acme-staging', status: 'inactive' },
]

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-10">
      <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">{title}</h2>
      <div className="flex flex-wrap items-center gap-3 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
        {children}
      </div>
    </section>
  )
}

/**
 * Sandbox de componentes (T05.11) — renderiza primitivos + padrões isolados nos
 * dois temas, pra revisar a biblioteca antes de aplicar em tela real.
 * Rota gated em DEV (ver App.tsx). Não vai a produção.
 */
export default function ComponentsSandbox() {
  const { mode, toggle } = useTheme()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [upgradeOpen, setUpgradeOpen] = useState(false)
  const [sw, setSw] = useState(true)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const cols: Column<DemoRow>[] = [
    { key: 'name', header: 'Name', sortable: true, render: (r) => <code className="font-mono">{r.name}</code> },
    {
      key: 'status',
      header: 'Status',
      render: (r) => (
        <StatusPill label={r.status} tone={r.status === 'active' ? 'success' : 'neutral'} />
      ),
    },
  ]

  return (
    <div className="min-h-screen bg-[var(--bg-page)] p-8 text-[var(--text-primary)]">
      <div className="mx-auto max-w-4xl">
        <PageHeader
          title="Components Sandbox"
          subtitle="Nível 1 (primitivos) + Nível 2 (padrões) — redesign"
          actions={
            <Button variant="outline" onClick={toggle}>
              <Icon name={mode === 'dark' ? 'light_mode' : 'dark_mode'} size={18} />
              {mode}
            </Button>
          }
        />

        <Section title="Buttons">
          <Button>Default</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="destructive">Destructive</Button>
          <Button size="icon" variant="outline"><Icon name="settings" size={18} /></Button>
        </Section>

        <Section title="Inputs / Switch / Badge">
          <Input placeholder="Type here…" className="max-w-xs" />
          <Switch checked={sw} onCheckedChange={setSw} />
          <Badge>default</Badge>
          <Badge variant="secondary">secondary</Badge>
          <Badge variant="outline">outline</Badge>
        </Section>

        <Section title="StatusPill">
          <StatusPill label="Active" tone="success" />
          <StatusPill label="Trial" tone="warning" />
          <StatusPill label="Revoked" tone="danger" />
          <StatusPill label="Paused" tone="neutral" />
          <StatusPill label="Primary" tone="primary" />
        </Section>

        <Section title="Enterprise / Secret">
          <EnterpriseBadge onClick={() => setUpgradeOpen(true)} />
          <div className="w-full max-w-md">
            <MaskedSecretField
              hasValue
              maskedHint="••••••••1234"
              replaceLabel="Replace"
              cancelLabel="Cancel"
              onSave={() => {}}
            />
          </div>
        </Section>

        <Section title="Tooltip / Skeleton / Icon">
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild><Button variant="outline">Hover me</Button></TooltipTrigger>
              <TooltipContent>Tooltip content</TooltipContent>
            </Tooltip>
          </TooltipProvider>
          <Skeleton className="h-8 w-32" />
          <Icon name="rocket_launch" size={24} />
        </Section>

        <Section title="ProviderCard">
          <div className="flex w-full flex-col gap-2">
            <ProviderCard name="GitHub" icon="code" description="Code provider" status={{ label: 'Active', tone: 'success' }} defaultOpen>
              <p className="text-sm text-[var(--text-secondary)]">Config fields go here.</p>
            </ProviderCard>
            <ProviderCard name="Azure Blob" icon="cloud" description="Future provider" badge="SOON" disabled />
          </div>
        </Section>

        <Section title="SettingRow">
          <div className="w-full divide-y divide-[var(--border)]">
            <SettingRow label="Soft delete" description="Keep deleted rows recoverable" control={<Switch checked={sw} onCheckedChange={setSw} />} />
            <SettingRow label="Require 2FA" description="For all admins" control={<Switch />} />
          </div>
        </Section>

        <Section title="Overlays">
          <Button onClick={() => setConfirmOpen(true)}>Open ConfirmDialog</Button>
          <Button variant="outline" onClick={() => setDrawerOpen(true)}>Open FormDrawer</Button>
        </Section>

        <div className="mb-10">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">DataTable</h2>
          <DataTable
            columns={cols}
            rows={DEMO_ROWS}
            getRowId={(r) => r.id}
            sort={{ key: 'name', dir: 'asc' }}
            onSort={() => {}}
            rowActions={() => (
              <Button size="icon" variant="ghost"><Icon name="more_vert" size={18} /></Button>
            )}
          />
        </div>

        <div className="mb-10">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">DataTable — expandable rows</h2>
          <DataTable
            columns={cols}
            rows={DEMO_ROWS}
            getRowId={(r) => r.id}
            expandedRowId={expandedId}
            onToggleExpand={(id) => setExpandedId((cur) => (cur === id ? null : id))}
            renderExpanded={(r) => (
              <div className="text-sm text-[var(--text-secondary)]">
                Detalhe expandido de <code className="font-mono">{r.name}</code> — status {r.status}.
              </div>
            )}
          />
        </div>

        <div className="mb-10">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">EmptyState</h2>
          <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)]">
            <EmptyState icon="inbox" title="No apps yet" description="Create your first app to get started." action={{ label: 'Create app', onClick: () => {} }} />
          </div>
        </div>

        <ConfirmDialog
          open={confirmOpen}
          title="Delete app?"
          message="This permanently deletes acme-prod and all its data."
          confirmLabel="Delete"
          cancelLabel="Cancel"
          destructive
          onConfirm={() => setConfirmOpen(false)}
          onCancel={() => setConfirmOpen(false)}
        />
        <FormDrawer
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          title="Create with AI"
          description="Describe the app you want."
          footer={<Button onClick={() => setDrawerOpen(false)}>Done</Button>}
        >
          <p className="text-sm text-[var(--text-secondary)]">Drawer body content.</p>
        </FormDrawer>
        <UpgradeModal
          open={upgradeOpen}
          feature="Datadog export"
          description="This feature requires an Enterprise license."
          confirmLabel="Upgrade"
          cancelLabel="Not now"
          onUpgrade={() => setUpgradeOpen(false)}
          onClose={() => setUpgradeOpen(false)}
        />
      </div>
    </div>
  )
}
