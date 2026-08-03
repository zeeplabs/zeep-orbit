import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
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
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs'
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

interface GitHubStatus {
  connected: boolean
  configured: boolean
  org_login: string
}

interface GitHubTemplate {
  id: string
  name: string
  description: string
  github_owner: string
  github_repo: string
  framework: string
  active: boolean
  created_by: string
  created_at: string
  render_service_type: string
  build_command: string
  publish_path: string
  start_command: string
}

function PageFooter({ message, type }: { message: string | null; type: 'success' | 'error' }) {
  if (!message) return null
  return (
    <div
      className="mt-4 rounded-[10px] border px-3 py-2 text-[12.5px]"
      style={{
        background: type === 'success' ? 'var(--success-tint)' : 'var(--danger-tint)',
        borderColor: type === 'success' ? 'var(--success)' : 'var(--danger)',
        color: type === 'success' ? 'var(--success)' : 'var(--danger)',
      }}
    >
      {message}
    </div>
  )
}

export default function GitHubIntegrationPage() {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = searchParams.get('tab') || 'config'

  const setTab = (value: string) => {
    setSearchParams({ tab: value }, { replace: true })
  }

  const { data: me, isLoading: meLoading } = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/me', { credentials: 'include' })
      if (!res.ok) return null
      return res.json() as Promise<{ id: string; email: string; name: string; role: string; language: string }>
    },
    retry: false,
  })

  if (meLoading) return null

  if (me?.role !== 'superadmin') {
    return (
      <div
        className="flex flex-col items-center justify-center gap-3 rounded-[14px] border p-12 text-center"
        style={{
          background: 'var(--bg-surface)',
          borderColor: 'var(--border)',
        }}
      >
        <div
          className="flex h-14 w-14 items-center justify-center rounded-[14px]"
          style={{ background: 'var(--hover-surface)' }}
        >
          <Icon name="shield" size={28} style={{ color: 'var(--text-tertiary)' }} />
        </div>
        <h2
          className="text-base font-bold"
          style={{ color: 'var(--text-primary)' }}
        >
          {t('github.forbiddenTitle')}
        </h2>
        <p
          className="max-w-md text-sm"
          style={{ color: 'var(--text-secondary)' }}
        >
          {t('github.forbiddenBody')}
        </p>
      </div>
    )
  }

  return (
    <>
      <PageHeader title={t('github.title')} subtitle={t('github.subtitle')} />

      <Tabs value={tab} onValueChange={setTab} className="w-full">
        <TabsList>
          <TabsTrigger value="config">
            <Icon name="link" size={14} />
            {t('github.tabConfig')}
          </TabsTrigger>
          <TabsTrigger value="templates">
            <Icon name="code" size={14} />
            {t('github.tabTemplates')}
          </TabsTrigger>
          <TabsTrigger value="deploy">
            <Icon name="rocket_launch" size={14} />
            {t('deploy.tabDeploy')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="config">
          <GitHubConfigTab />
        </TabsContent>
        <TabsContent value="templates">
          <GitHubTemplatesTab />
        </TabsContent>
        <TabsContent value="deploy">
          <DeployTab />
        </TabsContent>
      </Tabs>
    </>
  )
}

function GitHubConfigTab() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<GitHubStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [messageType, setMessageType] = useState<'success' | 'error'>('success')

  const [appId, setAppId] = useState('')
  const [appSlug, setAppSlug] = useState('')
  const [clientId, setClientId] = useState('')
  const [clientSecret, setClientSecret] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [webhookSecret, setWebhookSecret] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [showPrivateKey, setShowPrivateKey] = useState(false)

  const [disconnecting, setDisconnecting] = useState(false)
  const [showDisconnectDialog, setShowDisconnectDialog] = useState(false)
  const [installing, setInstalling] = useState(false)

  useEffect(() => {
    fetchStatus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const fetchStatus = async () => {
    try {
      const res = await fetch('/dashboard/api/github/status', { credentials: 'include' })
      const data = await res.json()
      setStatus(data)
      if (data.configured) {
        fetchConfig()
      }
    } catch {
      // non-critical
    } finally {
      setLoading(false)
    }
  }

  const fetchConfig = async () => {
    try {
      const res = await fetch('/dashboard/api/github/config', { credentials: 'include' })
      const data = await res.json()
      setAppId(data.app_id || '')
      setAppSlug(data.app_slug || '')
      setClientId(data.client_id || '')
    } catch {
      // non-critical
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setMessage(null)
    try {
      const res = await fetch('/dashboard/api/github/config', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          app_id: appId,
          app_slug: appSlug,
          client_id: clientId,
          client_secret: clientSecret,
          private_key: privateKey,
          webhook_secret: webhookSecret,
        }),
      })
      const data = await res.json()
      if (!res.ok) {
        setMessage(data.error || t('common.errorSaving'))
        setMessageType('error')
        return
      }
      setMessage(t('github.configSaved'))
      setMessageType('success')
      setClientSecret('')
      setPrivateKey('')
      setWebhookSecret('')
      await fetchStatus()
    } catch {
      setMessage(t('common.errorSaving'))
      setMessageType('error')
    } finally {
      setSaving(false)
    }
  }

  const handleInstall = async () => {
    setInstalling(true)
    try {
      const res = await fetch('/dashboard/api/github/install/start', { credentials: 'include' })
      const data = await res.json()
      if (data.install_url) {
        window.location.href = data.install_url
      }
    } catch {
      setInstalling(false)
    }
  }

  const handleDisconnect = async () => {
    setDisconnecting(true)
    try {
      const res = await fetch('/dashboard/api/github/config', {
        method: 'DELETE',
        credentials: 'include',
      })
      if (res.ok) {
        setStatus({ connected: false, configured: false, org_login: '' })
        setMessage(t('github.disconnected'))
        setMessageType('success')
      }
    } catch {
      setMessage(t('common.errorSaving'))
      setMessageType('error')
    } finally {
      setDisconnecting(false)
      setShowDisconnectDialog(false)
    }
  }

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('installed') === 'true') {
      fetchStatus()
      setMessage(t('github.installed'))
      setMessageType('success')
      const url = new URL(window.location.href)
      url.searchParams.delete('installed')
      window.history.replaceState({}, '', url.toString())
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (loading) return <LoadingState rows={4} />

  return (
    <div className="flex flex-col gap-6">
      <div
        className="flex flex-col gap-6 rounded-[14px] border p-6"
        style={{ background: 'var(--bg-surface)', borderColor: 'var(--border)' }}
      >
        <div className="flex items-center justify-between">
          <div>
            <div className="text-[15px] font-bold" style={{ color: 'var(--text-primary)' }}>
              GitHub App
            </div>
            <p className="mt-0.5 text-[12px]" style={{ color: 'var(--text-secondary)' }}>
              {t('github.configDesc')}
            </p>
          </div>
          {status?.configured && (
            <div className="flex items-center gap-3">
              <StatusPill
                label={status.connected ? t('github.connected') : t('github.notConnected')}
                tone={status.connected ? 'success' : 'warning'}
              />
              {status.connected && status.org_login && (
                <span className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                  {status.org_login}
                </span>
              )}
            </div>
          )}
        </div>

        <div className="flex max-w-lg flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="gh-app-id" className="text-[12px] font-semibold">
              {t('github.appId')}
            </Label>
            <Input id="gh-app-id" value={appId} onChange={(e) => setAppId(e.target.value)} placeholder="123456" />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="gh-app-slug" className="text-[12px] font-semibold">
              {t('github.appSlug')}
            </Label>
            <Input id="gh-app-slug" value={appSlug} onChange={(e) => setAppSlug(e.target.value)} placeholder="my-orbit-app" />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="gh-client-id" className="text-[12px] font-semibold">
              {t('github.clientId')}
            </Label>
            <Input id="gh-client-id" value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="Iv23li..." />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="gh-client-secret" className="text-[12px] font-semibold">
              {t('github.clientSecret')}
            </Label>
            <div className="relative">
              <Input
                id="gh-client-secret"
                type={showSecret ? 'text' : 'password'}
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                placeholder={status?.configured ? '•••••••• (empty = keep current)' : t('github.clientSecret')}
                className="pr-10"
              />
              <button
                type="button"
                onClick={() => setShowSecret(!showSecret)}
                aria-label={showSecret ? t('login.hidePassword') : t('login.showPassword')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 flex items-center justify-center"
                style={{ color: 'var(--text-tertiary)' }}
              >
                <Icon name={showSecret ? 'visibility_off' : 'visibility'} size={16} />
              </button>
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="gh-private-key" className="text-[12px] font-semibold">
              {t('github.privateKey')}
            </Label>
            <div className="relative">
              <textarea
                id="gh-private-key"
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
                placeholder={status?.configured ? '•••••••• (empty = keep current)' : t('github.privateKey')}
                rows={4}
                className="w-full resize-none rounded-lg border px-3 py-2 font-mono text-[12px] outline-none"
                style={{
                  background: 'var(--bg-page)',
                  borderColor: 'var(--border-strong)',
                  color: 'var(--text-primary)',
                  fontFamily: 'var(--font-mono)',
                }}
              />
              <button
                type="button"
                onClick={() => setShowPrivateKey(!showPrivateKey)}
                aria-label={showPrivateKey ? t('login.hidePassword') : t('login.showPassword')}
                className="absolute right-2.5 top-2.5 flex items-center justify-center"
                style={{ color: 'var(--text-tertiary)' }}
              >
                <Icon name={showPrivateKey ? 'visibility_off' : 'visibility'} size={16} />
              </button>
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="gh-webhook-secret" className="text-[12px] font-semibold">
              {t('github.webhookSecret')}
            </Label>
            <Input
              id="gh-webhook-secret"
              type="password"
              value={webhookSecret}
              onChange={(e) => setWebhookSecret(e.target.value)}
              placeholder={status?.configured ? '•••••••• (empty = keep current)' : t('github.webhookSecret')}
            />
          </div>
        </div>

        <PageFooter message={message} type={messageType} />

        <div className="flex items-center gap-3">
          <Button onClick={handleSave} disabled={saving} className="gap-2">
            {saving ? (
              <>
                <Icon name="progress_activity" size={14} className="animate-spin" /> {t('brand.saving')}
              </>
            ) : (
              <>
                <Icon name="check" size={14} /> {t('brand.save')}
              </>
            )}
          </Button>

          {status?.configured && (
            <>
              <Button
                onClick={handleInstall}
                disabled={installing || status.connected}
                variant="outline"
                className="gap-2"
              >
                {installing ? (
                  <Icon name="progress_activity" size={14} className="animate-spin" />
                ) : (
                  <Icon name="link" size={14} />
                )}
                {status.connected ? t('github.installed') : t('github.install')}
              </Button>
              <Button
                onClick={() => setShowDisconnectDialog(true)}
                disabled={disconnecting}
                variant="outline"
                className="gap-2"
                style={{ color: 'var(--danger)' }}
              >
                <Icon name="link_off" size={14} />
                {t('github.disconnect')}
              </Button>
            </>
          )}
        </div>
      </div>

      <Dialog open={showDisconnectDialog} onOpenChange={setShowDisconnectDialog}>
        <DialogContent className="max-w-[420px]">
          <DialogHeader>
            <div
              className="mb-[18px] flex h-11 w-11 items-center justify-center rounded-[10px] border"
              style={{ background: 'var(--danger-tint)', borderColor: 'var(--danger)' }}
            >
              <Icon name="link_off" size={18} style={{ color: 'var(--danger)' }} />
            </div>
            <DialogTitle>{t('github.disconnectTitle')}</DialogTitle>
            <DialogDescription>{t('github.disconnectDesc')}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDisconnectDialog(false)}>
              {t('github.disconnectCancel')}
            </Button>
            <Button
              onClick={handleDisconnect}
              disabled={disconnecting}
              style={{ background: 'var(--danger)', color: '#fff' }}
            >
              {disconnecting ? t('github.disconnecting') : t('github.disconnectConfirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function GitHubTemplatesTab() {
  const { t } = useTranslation()
  const [templates, setTemplates] = useState<GitHubTemplate[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showModal, setShowModal] = useState(false)
  const [editingTemplate, setEditingTemplate] = useState<GitHubTemplate | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [githubOwner, setGithubOwner] = useState('')
  const [githubRepo, setGithubRepo] = useState('')
  const [framework, setFramework] = useState('')
  const [renderServiceType, setRenderServiceType] = useState('')
  const [buildCommand, setBuildCommand] = useState('')
  const [publishPath, setPublishPath] = useState('')
  const [startCommand, setStartCommand] = useState('')

  useEffect(() => {
    fetchTemplates()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const fetchTemplates = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/dashboard/api/github/templates', { credentials: 'include' })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || t('common.connectionError'))
      }
      const data = await res.json()
      setTemplates(data)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const openCreateModal = () => {
    setEditingTemplate(null)
    setName('')
    setDescription('')
    setGithubOwner('')
    setGithubRepo('')
    setFramework('')
    setRenderServiceType('')
    setBuildCommand('')
    setPublishPath('')
    setStartCommand('')
    setFormError(null)
    setShowModal(true)
  }

  const openEditModal = (tpl: GitHubTemplate) => {
    setEditingTemplate(tpl)
    setName(tpl.name)
    setDescription(tpl.description)
    setGithubOwner(tpl.github_owner)
    setGithubRepo(tpl.github_repo)
    setFramework(tpl.framework)
    setRenderServiceType(tpl.render_service_type || '')
    setBuildCommand(tpl.build_command || '')
    setPublishPath(tpl.publish_path || '')
    setStartCommand(tpl.start_command || '')
    setFormError(null)
    setShowModal(true)
  }

  const handleSubmit = async () => {
    setSaving(true)
    setFormError(null)
    try {
      const body = {
        name,
        description,
        github_owner: githubOwner,
        github_repo: githubRepo,
        framework,
        render_service_type: renderServiceType,
        build_command: buildCommand,
        publish_path: publishPath,
        start_command: startCommand,
      }
      const url = editingTemplate
        ? `/dashboard/api/github/templates/${editingTemplate.id}`
        : '/dashboard/api/github/templates'
      const method = editingTemplate ? 'PUT' : 'POST'
      const res = await fetch(url, {
        method,
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || t('common.errorSaving'))
      }
      setShowModal(false)
      await fetchTemplates()
    } catch (err) {
      setFormError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleToggleActive = async (tpl: GitHubTemplate) => {
    try {
      await fetch(`/dashboard/api/github/templates/${tpl.id}/active`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ active: !tpl.active }),
      })
      setTemplates((prev) =>
        prev.map((x) => (x.id === tpl.id ? { ...x, active: !x.active } : x)),
      )
    } catch {
      // non-critical
    }
  }

  const handleDelete = async (tpl: GitHubTemplate) => {
    try {
      await fetch(`/dashboard/api/github/templates/${tpl.id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      setTemplates((prev) => prev.filter((x) => x.id !== tpl.id))
    } catch {
      // non-critical
    }
  }

  const columns: Column<GitHubTemplate>[] = [
    {
      key: 'name',
      header: t('github.templateName'),
      render: (tpl) => (
        <div>
          <div className="text-[13px] font-bold">{tpl.name}</div>
          {tpl.description && (
            <div
              className="mt-0.5 line-clamp-1 text-[11.5px]"
              style={{ color: 'var(--text-tertiary)' }}
            >
              {tpl.description}
            </div>
          )}
        </div>
      ),
    },
    {
      key: 'framework',
      header: t('github.templateFramework'),
      render: (tpl) =>
        tpl.framework ? (
          <span
            className="rounded px-2 py-0.5 text-[11.5px]"
            style={{ background: 'var(--bg-sunken)', color: 'var(--text-secondary)' }}
          >
            {tpl.framework}
          </span>
        ) : null,
    },
    {
      key: 'repo',
      header: t('integrations.title') === 'Integrations' ? 'Repository' : t('github.templateRepo'),
      render: (tpl) => (
        <span
          className="font-mono text-[12px]"
          style={{ color: 'var(--text-tertiary)' }}
        >
          {tpl.github_owner}/{tpl.github_repo}
        </span>
      ),
    },
    {
      key: 'active',
      header: t('github.active'),
      render: (tpl) => (
        <div className="flex items-center gap-2">
          <Switch checked={tpl.active} onCheckedChange={() => handleToggleActive(tpl)} />
          <span className="text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
            {tpl.active ? t('github.active') : t('github.inactive')}
          </span>
        </div>
      ),
    },
    {
      key: 'actions',
      header: t('frontendApps.actions'),
      align: 'right',
      render: (tpl) => (
        <div className="flex items-center justify-end gap-1">
          <Button
            variant="outline"
            size="icon"
            onClick={() => openEditModal(tpl)}
            title={t('github.editTemplate')}
            className="size-7 rounded-lg"
            style={{ color: 'var(--text-tertiary)' }}
          >
            <Icon name="edit" size={12} />
          </Button>
          <Button
            variant="outline"
            size="icon"
            onClick={() => handleDelete(tpl)}
            title={t('github.deleteTemplate')}
            className="size-7 rounded-lg"
            style={{ color: 'var(--danger)' }}
          >
            <Icon name="delete" size={12} />
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <p className="text-[12px]" style={{ color: 'var(--text-secondary)' }}>
          {t('github.templatesDesc')}
        </p>
        <Button onClick={openCreateModal} className="gap-2">
          {t('github.addTemplate')}
          <span
            className="flex size-6 items-center justify-center rounded-full"
            style={{ background: 'var(--hover-surface)' }}
          >
            <Icon name="add" size={12} />
          </span>
        </Button>
      </div>

      <DataTable<GitHubTemplate>
        columns={columns}
        rows={templates}
        getRowId={(tpl) => tpl.id}
        loading={loading}
        error={!!error}
        empty={{ title: t('github.noTemplates'), icon: 'code' }}
      />

      <Dialog open={showModal} onOpenChange={setShowModal}>
        <DialogContent className="max-w-[480px]">
          <DialogHeader>
            <div
              className="mb-[18px] flex h-11 w-11 items-center justify-center rounded-[10px] border"
              style={{ background: 'var(--hover-surface)', borderColor: 'var(--border)' }}
            >
              <Icon name="code" size={18} style={{ color: 'var(--text-tertiary)' }} />
            </div>
            <DialogTitle>
              {editingTemplate ? t('github.editTemplate') : t('github.addTemplate')}
            </DialogTitle>
            <DialogDescription>{t('github.templateFormDesc')}</DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label className="text-[12px] font-semibold">{t('github.templateName')}</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Vite React" />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label className="text-[12px] font-semibold">{t('github.templateDescription')}</Label>
              <Input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t('github.templateDescriptionPlaceholder')}
              />
            </div>
            <div className="flex gap-3">
              <div className="flex-1 flex flex-col gap-1.5">
                <Label className="text-[12px] font-semibold">{t('github.templateOwner')}</Label>
                <Input
                  value={githubOwner}
                  onChange={(e) => setGithubOwner(e.target.value)}
                  placeholder="zeeplabs"
                />
              </div>
              <div className="flex-[2] flex flex-col gap-1.5">
                <Label className="text-[12px] font-semibold">{t('github.templateRepo')}</Label>
                <Input
                  value={githubRepo}
                  onChange={(e) => setGithubRepo(e.target.value)}
                  placeholder="orbit-template-vite-react"
                />
              </div>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label className="text-[12px] font-semibold">{t('github.templateFramework')}</Label>
              <Input
                value={framework}
                onChange={(e) => setFramework(e.target.value)}
                placeholder="Vite + React"
              />
            </div>
            <div
              className="mt-2 flex flex-col gap-3 border-t pt-4"
              style={{ borderColor: 'var(--border)' }}
            >
              <p
                className="text-[11px] font-semibold uppercase tracking-wider"
                style={{ color: 'var(--text-tertiary)' }}
              >
                {t('github.deployConfig')}
              </p>
              <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                  <Label className="text-[12px] font-semibold">Service Type</Label>
                  <Select value={renderServiceType || 'none'} onValueChange={(v) => setRenderServiceType(v === 'none' ? '' : v)}>
                    <SelectTrigger>
                      <SelectValue placeholder={t('github.deployNone')} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">{t('github.deployNone')}</SelectItem>
                      <SelectItem value="static_site">Static Site</SelectItem>
                      <SelectItem value="web_service">Web Service</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {renderServiceType && (
                  <>
                    <div className="flex flex-col gap-1.5">
                      <Label className="text-[12px] font-semibold">Build Command</Label>
                      <Input
                        value={buildCommand}
                        onChange={(e) => setBuildCommand(e.target.value)}
                        placeholder="npm run build"
                      />
                    </div>
                    {renderServiceType === 'static_site' && (
                      <div className="flex flex-col gap-1.5">
                        <Label className="text-[12px] font-semibold">Publish Path</Label>
                        <Input
                          value={publishPath}
                          onChange={(e) => setPublishPath(e.target.value)}
                          placeholder="dist"
                        />
                      </div>
                    )}
                    {renderServiceType === 'web_service' && (
                      <div className="flex flex-col gap-1.5">
                        <Label className="text-[12px] font-semibold">Start Command</Label>
                        <Input
                          value={startCommand}
                          onChange={(e) => setStartCommand(e.target.value)}
                          placeholder="npm start"
                        />
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          </div>

          <PageFooter message={formError} type="error" />

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowModal(false)} disabled={saving}>
              {t('github.cancel')}
            </Button>
            <Button onClick={handleSubmit} disabled={saving}>
              {saving ? (
                <Icon name="progress_activity" size={14} className="mr-1.5 animate-spin" />
              ) : editingTemplate ? (
                t('github.updateTemplate')
              ) : (
                t('github.createTemplate')
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function DeployTab() {
  const { t } = useTranslation()
  const [apiKey, setApiKey] = useState('')
  const [renderProjectId, setRenderProjectId] = useState('')
  const [renderEnvironmentId, setRenderEnvironmentId] = useState('')
  const [baseDomain, setBaseDomain] = useState('')
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<{
    connected: boolean
    provider: string
    render_project_id?: string
    render_environment_id?: string
    base_domain?: string
  } | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [messageType, setMessageType] = useState<'success' | 'error'>('success')

  useEffect(() => {
    fetchStatus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const fetchStatus = async () => {
    try {
      const res = await fetch('/dashboard/api/deploy-provider/status', { credentials: 'include' })
      if (res.ok) {
        const data = await res.json()
        setStatus(data)
        if (data.render_project_id) setRenderProjectId(data.render_project_id)
        if (data.render_environment_id) setRenderEnvironmentId(data.render_environment_id)
        if (data.base_domain) setBaseDomain(data.base_domain)
      }
    } catch {
      // non-critical
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setMessage(null)
    try {
      const res = await fetch('/dashboard/api/deploy-provider/config', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          api_key: apiKey,
          render_project_id: renderProjectId,
          render_environment_id: renderEnvironmentId,
          base_domain: baseDomain,
        }),
      })
      const data = await res.json()
      if (!res.ok) {
        setMessage(data.error || t('common.errorSaving'))
        setMessageType('error')
        return
      }
      setMessage(status?.connected ? t('deploy.update') : t('github.configSaved'))
      setMessageType('success')
      setApiKey('')
      await fetchStatus()
    } catch {
      setMessage(t('common.connectionError'))
      setMessageType('error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div
        className="flex flex-col gap-6 rounded-[14px] border p-6"
        style={{ background: 'var(--bg-surface)', borderColor: 'var(--border)' }}
      >
        <div className="mb-0 flex items-center gap-4">
          <div
            className="flex size-10 shrink-0 items-center justify-center rounded-[10px] border"
            style={{
              background: status?.connected ? 'var(--success-tint)' : 'var(--bg-sunken)',
              borderColor: status?.connected ? 'var(--success)' : 'var(--border)',
            }}
          >
            <Icon
              name="rocket_launch"
              size={18}
              style={{ color: status?.connected ? 'var(--success)' : 'var(--text-tertiary)' }}
            />
          </div>
          <div>
            <h3 className="text-sm font-bold" style={{ color: 'var(--text-primary)' }}>
              {t('deploy.title')}
            </h3>
            <p className="text-xs" style={{ color: 'var(--text-tertiary)' }}>
              {status?.connected ? t('deploy.connected') : t('deploy.notConnected')}
            </p>
          </div>
          {status?.connected && (
            <div className="ml-auto">
              <StatusPill label={t('deploy.active')} tone="success" />
            </div>
          )}
        </div>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label className="text-[12px] font-semibold">{t('deploy.apiKey')}</Label>
            <Input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="rnd_..."
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSave()
              }}
            />
            <p className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('deploy.apiKeyHint')}
            </p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-[12px] font-semibold">{t('deploy.projectId')}</Label>
            <Input
              value={renderProjectId}
              onChange={(e) => setRenderProjectId(e.target.value)}
              placeholder="prj-..."
            />
            <p className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('deploy.projectIdHint')}
            </p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-[12px] font-semibold">{t('deploy.environmentId')}</Label>
            <Input
              value={renderEnvironmentId}
              onChange={(e) => setRenderEnvironmentId(e.target.value)}
              placeholder="evm-..."
            />
            <p className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('deploy.environmentIdHint')}
            </p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-[12px] font-semibold">{t('deploy.baseDomain')}</Label>
            <Input
              value={baseDomain}
              onChange={(e) => setBaseDomain(e.target.value)}
              placeholder="meusite.com"
            />
            <p className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('deploy.baseDomainHint')}
            </p>
            <p className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
              {t('deploy.dnsHint')}
            </p>
          </div>

          <PageFooter message={message} type={messageType} />

          <Button
            onClick={handleSave}
            disabled={saving || (!status?.connected && !apiKey.trim())}
            className="gap-2 self-start"
          >
            {saving ? (
              <>
                <Icon name="progress_activity" size={14} className="animate-spin" /> {t('brand.saving')}
              </>
            ) : (
              <>
                <Icon name="check" size={14} />{' '}
                {status?.connected ? t('deploy.save') : t('deploy.connect')}
              </>
            )}
          </Button>
        </div>
      </div>
    </div>
  )
}
