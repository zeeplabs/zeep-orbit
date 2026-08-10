import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { PageHeader, EmptyState, LoadingState, StatusPill, DataTable } from '@/components/patterns'
import type { Column } from '@/components/patterns'
import { cn } from '@/lib/utils'

interface FrontendApp {
  id: string
  name: string
  slug: string
  template_id: string
  template_name: string
  github_repo_url: string
  status: string
  error_message: string
  created_by: string
  created_at: string
  archived_at: string | null
}

interface Template {
  id: string
  name: string
  description: string
  github_owner: string
  github_repo: string
  framework: string
  active: boolean
}

interface SyncInfo {
  sync_status: string
  public_key: string
  error_message: string
}

function CopyButton({ value, label }: { value: string; label: string }) {
  return (
    <Button
      type="button"
      size="icon"
      variant="ghost"
      onClick={() => {
        navigator.clipboard.writeText(value)
        toast.success(label)
      }}
      title={label}
      aria-label={label}
      className="size-7 shrink-0 rounded-md"
      style={{ color: 'var(--text-tertiary)' }}
    >
      <Icon name="content_copy" size={15} />
    </Button>
  )
}

export default function FrontendAppsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [apps, setApps] = useState<FrontendApp[]>([])
  const [templates, setTemplates] = useState<Template[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [showCreate, setShowCreate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [newName, setNewName] = useState('')
  const [newTemplateId, setNewTemplateId] = useState('')

  const [deleting, setDeleting] = useState<string | null>(null)
  const [retrying, setRetrying] = useState<string | null>(null)

  const [syncModalApp, setSyncModalApp] = useState<FrontendApp | null>(null)
  const [syncInfo, setSyncInfo] = useState<SyncInfo | null>(null)
  const [syncLoading, setSyncLoading] = useState(false)
  const [revealedKey, setRevealedKey] = useState<string | null>(null)
  const [revealing, setRevealing] = useState(false)

  const fetchApps = async () => {
    try {
      const res = await fetch('/dashboard/api/frontend-apps', { credentials: 'include' })
      if (!res.ok) throw new Error('failed')
      const data = await res.json()
      setApps(data)
      setError(null)
    } catch {
      setError(t('frontendApps.repo'))
    }
  }

  const fetchTemplates = async () => {
    try {
      const res = await fetch('/dashboard/api/github/templates', { credentials: 'include' })
      if (!res.ok) throw new Error('failed')
      const data = await res.json()
      setTemplates((data || []).filter((tmpl: Template) => tmpl.active))
    } catch {
      // non-critical
    }
  }

  useEffect(() => {
    Promise.all([fetchApps(), fetchTemplates()]).finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleCreate = async () => {
    setCreating(true)
    setCreateError(null)
    try {
      const res = await fetch('/dashboard/api/frontend-apps', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName, template_id: newTemplateId }),
      })
      const data = await res.json()
      if (!res.ok) {
        setCreateError(data.error || t('frontendApps.repoFailed'))
        return
      }
      setShowCreate(false)
      setNewName('')
      setNewTemplateId('')
      qc.invalidateQueries({ queryKey: ['frontend-apps'] })
      await fetchApps()
      if (data.status === 'failed') {
        setCreateError(
          `${t('frontendApps.deployFailed')}: ${data.error_message}`,
        )
      }
    } catch {
      setCreateError(t('common.connectionError'))
    } finally {
      setCreating(false)
    }
  }

  const handleRetry = async (id: string) => {
    setRetrying(id)
    try {
      await fetch(`/dashboard/api/frontend-apps/${id}/retry`, {
        method: 'POST',
        credentials: 'include',
      })
      await fetchApps()
    } finally {
      setRetrying(null)
    }
  }

  const handleDelete = async (id: string) => {
    setDeleting(id)
    try {
      await fetch(`/dashboard/api/frontend-apps/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      setApps((prev) => prev.filter((a) => a.id !== id))
    } finally {
      setDeleting(null)
    }
  }

  const openSync = async (app: FrontendApp) => {
    setSyncModalApp(app)
    setSyncInfo(null)
    setRevealedKey(null)
    setSyncLoading(true)
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${app.id}/sync`, {
        credentials: 'include',
      })
      if (res.ok) {
        const data = await res.json()
        setSyncInfo(data)
      }
    } finally {
      setSyncLoading(false)
    }
  }

  const handleReveal = async () => {
    if (!syncModalApp) return
    setRevealing(true)
    try {
      const res = await fetch(
        `/dashboard/api/frontend-apps/${syncModalApp.id}/reveal-key`,
        { method: 'POST', credentials: 'include' },
      )
      if (res.ok) {
        const data = await res.json()
        setRevealedKey(data.private_key)
      }
    } catch {
      toast.error(t('frontendApps.revealErr'))
    } finally {
      setRevealing(false)
    }
  }

  const handleSyncRetry = async () => {
    if (!syncModalApp) return
    setSyncLoading(true)
    try {
      const res = await fetch(
        `/dashboard/api/frontend-apps/${syncModalApp.id}/sync/retry`,
        { method: 'POST', credentials: 'include' },
      )
      if (res.ok) {
        const data = await res.json()
        setSyncInfo({
          sync_status: data.sync_status,
          public_key: data.public_key || '',
          error_message: data.error_message || '',
        })
        toast.success(t('frontendApps.syncRetryOk'))
      }
    } finally {
      setSyncLoading(false)
    }
  }

  const handleSyncRegenerate = async () => {
    if (!syncModalApp) return
    setSyncLoading(true)
    setRevealedKey(null)
    try {
      const res = await fetch(
        `/dashboard/api/frontend-apps/${syncModalApp.id}/sync/regenerate`,
        { method: 'POST', credentials: 'include' },
      )
      if (res.ok) {
        const data = await res.json()
        setSyncInfo({
          sync_status: data.sync_status,
          public_key: data.public_key || '',
          error_message: data.error_message || '',
        })
        toast.success(t('frontendApps.syncRegenOk'))
      }
    } finally {
      setSyncLoading(false)
    }
  }

  const cloneCommand = syncModalApp?.github_repo_url
    ? `git clone ${syncModalApp.github_repo_url.replace('https://github.com/', 'git@github.com:')}.git`
    : ''

  const agentPrompt = syncModalApp
    ? `You are configuring SSH-based Git access for a frontend app managed by Zeep Orbit.
The app "${syncModalApp.name}" has a GitHub repository that already exists. Your goal is to clone it using a one-time deploy key and start working on the code.

---
BEFORE YOU START
---
Ask the user:
- Where should the project be cloned? (e.g. ~/projects, ~/Desktop, or a specific path)
- What do they want to build or change in this app? (e.g. add a landing page, change the theme, fix a bug)
- Any other preferences? (framework version, libraries to use, design system)

Wait for the user's answers before proceeding. Do NOT clone to a default or temporary directory unless explicitly asked.

---
CONTEXT
---
- A deploy key (SSH key pair) was generated specifically for this repository.
- The private key below grants push access. Keep it secure — only write it to disk when needed.
- The SSH key has no passphrase. Strict file permissions (600) are required.
- After cloning, you should build and develop the app as if it were any other local project.

---
STEPS
---
(Execute these steps after the user tells you where to clone.)

Step 1 — Write the private key to disk:
  cat > ~/.ssh/id_ed25519_${syncModalApp.slug} << 'KEYEOF'
${revealedKey || "[click 'Reveal Key' to display]"}
KEYEOF

Step 2 — Set correct permissions:
  chmod 600 ~/.ssh/id_ed25519_${syncModalApp.slug}

Step 3 — Add an SSH host entry so Git uses this key for the target repo only:
  cat >> ~/.ssh/config << 'SSHEOF'

Host github.com-${syncModalApp.slug}
    HostName github.com
    IdentityFile ~/.ssh/id_ed25519_${syncModalApp.slug}
    IdentitiesOnly yes
SSHEOF

Step 4 — Clone the repository into the directory the user chose:
  GIT_SSH_COMMAND="ssh -i ~/.ssh/id_ed25519_${syncModalApp.slug} -o StrictHostKeyChecking=accept-new" ${cloneCommand} <user-chosen-path>

---
AFTER CLONE
---
- cd into the cloned directory and start working on what the user asked for.
- Use 'git push' normally — the SSH config will route authentication through the deploy key automatically.
- Commit and push your changes so the user can deploy.`
    : ''

  const columns: Column<FrontendApp>[] = [
    {
      key: 'name',
      header: t('frontendApps.name'),
      render: (app) => (
        <div>
          <div className="text-[13px] font-bold">{app.name}</div>
          <div className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {app.created_by}
          </div>
        </div>
      ),
    },
    {
      key: 'slug',
      header: t('frontendApps.slug'),
      render: (app) => (
        <code
          className="rounded px-1.5 py-0.5 text-[11.5px] font-mono"
          style={{
            background: 'var(--bg-sunken)',
            color: 'var(--text-secondary)',
          }}
        >
          {app.slug}
        </code>
      ),
    },
    {
      key: 'template',
      header: t('frontendApps.template'),
      render: (app) => (
        <span className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
          {app.template_name}
        </span>
      ),
    },
    {
      key: 'status',
      header: t('frontendApps.status'),
      render: (app) => {
        if (app.status === 'ready') {
          return (
            <StatusPill
              label={t('frontendApps.statusReady')}
              tone="success"
              dot
            />
          )
        }
        return (
          <StatusPill
            label={t('frontendApps.statusFailed')}
            tone="danger"
            dot
          />
        )
      },
    },
    {
      key: 'actions',
      header: t('frontendApps.actions'),
      align: 'right',
      render: (app) => {
        const isDeleting = deleting === app.id
        const isRetrying = retrying === app.id
        return (
          <div className="flex items-center justify-end gap-1">
            {app.status === 'ready' && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => openSync(app)}
                className="h-8 rounded-md text-[12px] font-semibold"
                style={{ color: 'var(--primary)' }}
              >
                <Icon name="vpn_key" size={14} />
                <span className="ml-1">{t('frontendApps.sync')}</span>
              </Button>
            )}
            {app.status === 'failed' && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => handleRetry(app.id)}
                disabled={isRetrying}
                className="h-8 rounded-md text-[12px] font-semibold"
                style={{ color: 'var(--warning)' }}
              >
                {isRetrying ? (
                  <Icon name="progress_activity" size={14} className="animate-spin" />
                ) : (
                  <Icon name="refresh" size={14} />
                )}
                <span className="ml-1">{t('frontendApps.retry')}</span>
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              onClick={() => handleDelete(app.id)}
              disabled={isDeleting}
              className="h-8 rounded-md text-[12px] font-semibold"
              style={{ color: 'var(--danger)' }}
            >
              {isDeleting ? (
                <Icon name="progress_activity" size={14} className="animate-spin" />
              ) : (
                <Icon name="delete" size={14} />
              )}
            </Button>
          </div>
        )
      },
    },
  ]

  return (
    <>
      <PageHeader
        title={t('frontendApps.title')}
        subtitle={t('frontendApps.subtitle')}
      />

      <div className="mb-5 flex items-center justify-end">
        <Button
          onClick={() => {
            setShowCreate(true)
            setCreateError(null)
            setNewName('')
            setNewTemplateId('')
          }}
          disabled={templates.length === 0}
          className="rounded-[10px]"
        >
          <Icon name="add" size={16} />
          <span className="ml-1.5">{t('frontendApps.create')}</span>
        </Button>
      </div>

      <DataTable<FrontendApp>
        columns={columns}
        rows={apps}
        getRowId={(app) => app.id}
        loading={loading}
        error={!!error}
        empty={{
          title: t('frontendApps.noApps'),
          icon: 'globe',
        }}
      />

      <Dialog open={showCreate} onOpenChange={setShowCreate}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t('frontendApps.createTitle')}</DialogTitle>
            <DialogDescription>{t('frontendApps.createDesc')}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="fa-name" className="text-[12px] font-semibold">
                {t('frontendApps.formName')}
              </Label>
              <Input
                id="fa-name"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder={t('frontendApps.formNamePlaceholder')}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="fa-template" className="text-[12px] font-semibold">
                {t('frontendApps.formTemplate')}
              </Label>
              <Select value={newTemplateId} onValueChange={setNewTemplateId}>
                <SelectTrigger id="fa-template">
                  <SelectValue placeholder={t('frontendApps.selectTemplate')} />
                </SelectTrigger>
                <SelectContent>
                  {templates.map((tmpl) => (
                    <SelectItem key={tmpl.id} value={tmpl.id}>
                      <span>{tmpl.name}</span>
                      <span
                        className="ml-2 text-[11.5px]"
                        style={{ color: 'var(--text-tertiary)' }}
                      >
                        {tmpl.framework}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {createError && (
              <div
                className="rounded-[10px] border px-3 py-2 text-[12.5px]"
                style={{
                  background: 'var(--danger-tint)',
                  borderColor: 'var(--danger)',
                  color: 'var(--danger)',
                }}
              >
                {createError}
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCreate(false)}>
              {t('frontendApps.cancel')}
            </Button>
            <Button
              onClick={handleCreate}
              disabled={!newName.trim() || !newTemplateId || creating}
            >
              {creating && (
                <Icon
                  name="progress_activity"
                  size={14}
                  className="mr-1.5 animate-spin"
                />
              )}
              {t('frontendApps.creating')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!syncModalApp}
        onOpenChange={() => {
          setSyncModalApp(null)
          setRevealedKey(null)
        }}
      >
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>
              {t('frontendApps.syncTitle')}
              {syncModalApp ? ` — ${syncModalApp.name}` : ''}
            </DialogTitle>
            <DialogDescription>{t('frontendApps.syncDesc')}</DialogDescription>
          </DialogHeader>

          {syncLoading ? (
            <LoadingState rows={4} />
          ) : syncInfo ? (
            <div className="flex min-w-0 flex-col gap-4">
              <div
                className="flex items-center justify-between rounded-[10px] border p-3"
                style={{
                  background: 'var(--bg-sunken)',
                  borderColor: 'var(--border)',
                }}
              >
                <span className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
                  {t('frontendApps.syncStatus')}
                </span>
                {syncInfo.sync_status === 'ready' && (
                  <StatusPill
                    label={t('frontendApps.syncReady')}
                    tone="success"
                    dot
                  />
                )}
                {syncInfo.sync_status === 'pending' && (
                  <StatusPill
                    label={t('frontendApps.syncPending')}
                    tone="warning"
                    dot
                  />
                )}
                {syncInfo.sync_status === 'failed' && (
                  <StatusPill
                    label={t('frontendApps.syncFailed')}
                    tone="danger"
                    dot
                  />
                )}
              </div>

              {syncInfo.error_message && (
                <div
                  className="flex items-start gap-2 rounded-[10px] border p-3 text-[12.5px]"
                  style={{
                    background: 'var(--danger-tint)',
                    borderColor: 'var(--danger)',
                    color: 'var(--danger)',
                  }}
                >
                  <Icon name="warning" size={15} className="mt-0.5 shrink-0" />
                  <span>{syncInfo.error_message}</span>
                </div>
              )}

              <div className="flex flex-col gap-2">
                <Label className="text-[12px] font-semibold">
                  {t('frontendApps.cloneCommand')}
                </Label>
                <div className="flex items-center gap-2">
                  <code
                    className="flex-1 rounded border px-3 py-2 font-mono text-[11.5px] break-all"
                    style={{
                      background: 'var(--bg-page)',
                      borderColor: 'var(--border)',
                      color: 'var(--text-secondary)',
                    }}
                  >
                    {cloneCommand}
                  </code>
                  <CopyButton value={cloneCommand} label={t('frontendApps.copied')} />
                </div>
              </div>

              {syncInfo.sync_status === 'ready' && !revealedKey && (
                <Button onClick={handleReveal} disabled={revealing} className="w-full">
                  {revealing && (
                    <Icon
                      name="progress_activity"
                      size={14}
                      className="mr-1.5 animate-spin"
                    />
                  )}
                  <Icon name="vpn_key" size={14} className="mr-1.5" />
                  {t('frontendApps.revealKey')}
                </Button>
              )}

              {revealedKey && (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <Label className="text-[12px] font-semibold">
                      {t('frontendApps.privateKey')}
                    </Label>
                    <div className="relative overflow-hidden rounded border">
                      <code
                        className="block max-h-32 overflow-auto whitespace-pre-wrap break-all p-3 pr-10 font-mono text-[11.5px]"
                        style={{
                          background: 'var(--bg-page)',
                          color: 'var(--text-primary)',
                        }}
                      >
                        {revealedKey}
                      </code>
                      <div className="absolute right-1 top-1">
                        <CopyButton
                          value={revealedKey}
                          label={t('frontendApps.promptCopied')}
                        />
                      </div>
                    </div>
                  </div>

                  <div className="flex flex-col gap-1.5">
                    <Label className="text-[12px] font-semibold">
                      {t('frontendApps.agentPrompt')}
                    </Label>
                    <div className="relative overflow-hidden rounded border">
                      <code
                        className="block max-h-48 overflow-auto whitespace-pre-wrap break-all p-3 pr-10 font-mono text-[11.5px]"
                        style={{
                          background: 'var(--bg-page)',
                          color: 'var(--text-secondary)',
                        }}
                      >
                        {agentPrompt}
                      </code>
                      <div className="absolute right-1 top-1">
                        <CopyButton
                          value={agentPrompt}
                          label={t('frontendApps.promptCopied')}
                        />
                      </div>
                    </div>
                  </div>
                </div>
              )}

              <div className="flex gap-2">
                {(syncInfo.sync_status === 'pending' || syncInfo.sync_status === 'failed') && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleSyncRetry}
                    className="rounded-md"
                  >
                    <Icon name="refresh" size={14} className="mr-1" />
                    {t('frontendApps.syncRetry')}
                  </Button>
                )}
                {syncInfo.sync_status === 'ready' && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleSyncRegenerate}
                    className="rounded-md"
                  >
                    <Icon name="refresh" size={14} className="mr-1" />
                    {t('frontendApps.syncRegenerate')}
                  </Button>
                )}
              </div>
            </div>
          ) : (
            <div className="py-10 text-center text-sm" style={{ color: 'var(--text-tertiary)' }}>
              {t('frontendApps.noSyncInfo')}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
