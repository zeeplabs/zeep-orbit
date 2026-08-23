import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation, Trans } from "react-i18next";
import { toast } from "sonner";
import { useApps, useDeleteApp, useAIProviderStatus, AppDef } from "../lib/api";
import { BuildWithAIDrawer } from "@/components/patterns/BuildWithAIDrawer";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Icon } from "@/components/ui/icon";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  PageHeader,
  EmptyState,
  ErrorState,
  LoadingState,
  StatusPill,
  ConfirmDialog,
} from "@/components/patterns";
import { cn } from "@/lib/utils";

// --- Types ---

interface FrontendApp {
  id: string;
  name: string;
  slug: string;
  template_name: string;
  github_repo_url: string;
  status: string;
  error_message: string;
  created_by: string;
  created_at: string;
  deploy_status: string;
  deploy_url: string;
  deploy_error_message: string;
  custom_domain: string;
}

interface Template {
  id: string;
  name: string;
  framework: string;
  active: boolean;
}

// --- Metadata chip (icon + label, neutral surface) ---

function Chip({
  icon,
  label,
  tone = "neutral",
}: {
  icon?: string;
  label: string;
  tone?: "neutral" | "primary";
}) {
  const fg = tone === "primary" ? "var(--primary)" : "var(--text-secondary)";
  const bg = tone === "primary" ? "var(--primary-tint)" : "var(--sunken)";
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11.5px] font-semibold"
      style={{ color: fg, background: bg }}
    >
      {icon && <Icon name={icon} size={14} />}
      {label}
    </span>
  );
}

// --- Section header ---

function SectionHeader({ icon, label, count }: { icon: string; label: string; count: number }) {
  return (
    <div className="mb-4 flex items-center gap-2.5">
      <Icon name={icon} size={18} className="text-[var(--text-tertiary)]" />
      <span className="text-sm font-bold text-[var(--text-primary)]">{label}</span>
      <span className="rounded-full bg-[var(--sunken)] px-2 py-0.5 text-xs text-[var(--text-tertiary)]">
        {count}
      </span>
    </div>
  );
}

// --- Backend App Card ---

interface AppCardProps {
  app: AppDef;
  currentUserId?: string;
  onEdit: (app: AppDef) => void;
  onDelete: (app: AppDef) => void;
  onUsers: (app: AppDef) => void;
}

// Footer buttons share the handoff's outlined pill styling.
const cardFooterBtn =
  "flex h-9 items-center justify-center gap-1.5 rounded-[8px] border border-[var(--border-strong)] bg-transparent text-xs font-semibold text-[var(--text-secondary)] transition-colors hover:border-[var(--primary)] hover:text-[var(--text-primary)]";

function AppCard({ app, currentUserId, onEdit, onDelete, onUsers }: AppCardProps) {
  const { t } = useTranslation();
  const initial = app.name.charAt(0).toUpperCase();
  const authOn = app.auth_email_enabled;
  const owner =
    app.owner_id && currentUserId && app.owner_id === currentUserId
      ? t("apps.ownerYou")
      : app.owner_name || app.owner_email || t("apps.ownerYou");

  return (
    <div className="flex h-full flex-col gap-3.5 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5 transition-colors hover:border-[var(--border-strong)]">
      {/* HEADER */}
      <div className="flex items-center gap-3">
        <div
          className="flex size-[38px] shrink-0 items-center justify-center rounded-[10px] text-[15px] font-bold text-white"
          style={{ background: "linear-gradient(135deg, var(--primary), var(--accent))" }}
        >
          {initial}
        </div>
        <div className="min-w-0 flex-1">
          <h3
            className="truncate text-[14.5px] font-bold text-[var(--text-primary)]"
            style={{ fontFamily: "var(--font-display)" }}
          >
            {app.name}
          </h3>
          <span className="truncate text-xs text-[var(--text-tertiary)]">
            {t("apps.ownedBy", { owner })}
          </span>
        </div>
      </div>

      {/* META */}
      <div className="flex flex-wrap items-start gap-2">
        <Chip
          icon="table_chart"
          label={`${app.tables?.length ?? 0} ${t("apps.table", { count: app.tables?.length ?? 0 })}`}
        />
        <span
          className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11.5px] font-semibold"
          style={{
            color: authOn ? "var(--success)" : "var(--text-secondary)",
            background: authOn ? "var(--success-tint)" : "var(--sunken)",
          }}
        >
          <Icon name={authOn ? "mail" : "vpn_key"} size={14} />
          {authOn ? t("apps.authOn") : t("apps.tokenAuth")}
        </span>
      </div>

      {/* FOOTER */}
      <div className="mt-auto flex items-center gap-1.5 border-t border-[var(--border)] pt-3">
        <button type="button" className={cn(cardFooterBtn, "flex-1")} onClick={() => onUsers(app)}>
          <Icon name="group" size={16} />
          {t("apps.users")}
        </button>
        <button
          type="button"
          className={cn(cardFooterBtn, "flex-1")}
          onClick={() => window.open(`/docs/${app.name}`, "_blank")}
        >
          <Icon name="description" size={16} />
          {t("apps.apiDocs")}
        </button>
        <button
          type="button"
          className={cn(cardFooterBtn, "w-9")}
          onClick={() => onEdit(app)}
          title={t("apps.edit")}
        >
          <Icon name="edit" size={16} />
        </button>
        <button
          type="button"
          className="flex h-9 w-9 items-center justify-center rounded-[8px] border border-[var(--border-strong)] bg-transparent text-[var(--danger)] transition-colors hover:border-[var(--danger)] hover:bg-[var(--danger-tint)]"
          onClick={() => onDelete(app)}
          title={t("apps.delete")}
        >
          <Icon name="delete" size={16} />
        </button>
      </div>
    </div>
  );
}

// --- Frontend App Card ---

interface FrontendCardProps {
  app: FrontendApp;
  onSync: (app: FrontendApp) => void;
  onDelete: (app: FrontendApp) => void;
  onDeployRetry: (app: FrontendApp) => void;
  onSetDomain: (app: FrontendApp) => void;
  loading: { deployRetry: string | null; delete: string | null };
}

function FrontendCard({ app, onSync, onDelete, onDeployRetry, onSetDomain, loading }: FrontendCardProps) {
  const { t } = useTranslation();
  const repoReady = app.status === "ready";
  const deployReady = app.deploy_status === "ready";
  const deployFailed = app.deploy_status === "failed";
  const domain =
    app.custom_domain ||
    (app.deploy_url?.startsWith("https://") ? app.deploy_url.replace("https://", "") : app.deploy_url);

  const deployDot = deployReady ? "var(--success)" : deployFailed ? "var(--danger)" : "var(--warning)";
  const deployLabel = deployReady
    ? t("frontendApps.live")
    : deployFailed
    ? t("frontendApps.deployFailed")
    : t("frontendApps.deploying");
  const retrying = loading.deployRetry === app.id;

  return (
    <div className="flex h-full flex-col gap-3 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5 transition-colors hover:border-[var(--border-strong)]">
      {/* HEADER */}
      <div className="min-w-0">
        <h3
          className="truncate text-[14.5px] font-bold text-[var(--text-primary)]"
          style={{ fontFamily: "var(--font-display)" }}
        >
          {app.name}
        </h3>
        <span className="truncate text-xs text-[var(--text-tertiary)]">{app.slug}</span>
      </div>

      {/* STATUS */}
      <div className="flex flex-1 flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="inline-flex items-center gap-1.5 rounded-full bg-[var(--sunken)] px-2.5 py-1 text-[11.5px] font-semibold text-[var(--text-secondary)]">
            <span className="size-1.5 rounded-full" style={{ background: repoReady ? "var(--success)" : "var(--danger)" }} />
            {repoReady ? t("frontendApps.repo") : t("frontendApps.repoFailed")}
          </span>
          <span className="inline-flex items-center gap-1.5 rounded-full bg-[var(--sunken)] px-2.5 py-1 text-[11.5px] font-semibold text-[var(--text-secondary)]">
            <span className="size-1.5 rounded-full" style={{ background: deployDot }} />
            {deployLabel}
          </span>
          <Chip label={app.template_name} tone="primary" />
        </div>

        {deployReady && domain && (
          <div className="flex items-center gap-1.5 text-[12.5px] text-[var(--text-secondary)]">
            <Icon name="link" size={15} className="shrink-0 text-[var(--text-tertiary)]" />
            <a
              href={app.custom_domain ? `https://${app.custom_domain}` : app.deploy_url}
              target="_blank"
              rel="noopener noreferrer"
              className="min-w-0 truncate font-medium text-[var(--primary)] hover:underline"
            >
              {domain}
            </a>
            <button
              onClick={() => onSetDomain(app)}
              title={t("frontendApps.editDomain")}
              className="shrink-0 text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]"
            >
              <Icon name="edit" size={13} />
            </button>
          </div>
        )}

        {deployFailed && app.deploy_error_message && (
          <div className="flex flex-col gap-2 rounded-[10px] border border-[var(--danger)] bg-[var(--danger-tint)] p-2.5">
            <p className="break-all font-mono text-[11px] leading-relaxed text-[var(--danger)]" title={app.deploy_error_message}>
              {app.deploy_error_message}
            </p>
            <button
              type="button"
              className="self-start text-[11.5px] font-bold text-[var(--danger)] disabled:opacity-60"
              onClick={() => onDeployRetry(app)}
              disabled={retrying}
            >
              {t("frontendApps.deployRetry")}
            </button>
          </div>
        )}
      </div>

      {/* FOOTER */}
      <div className="mt-auto flex items-center gap-1.5 border-t border-[var(--border)] pt-3">
        <button
          type="button"
          className={cn(cardFooterBtn, "flex-1", !repoReady && "text-[var(--warning)]")}
          onClick={() => onSync(app)}
        >
          <Icon name={repoReady ? "key" : "refresh"} size={16} />
          {repoReady ? t("frontendApps.sync") : t("frontendApps.retry")}
        </button>
        <button
          type="button"
          className={cn(cardFooterBtn, "w-9")}
          onClick={() => onDeployRetry(app)}
          disabled={retrying}
          title={t("frontendApps.deployRetry")}
        >
          <Icon name={retrying ? "progress_activity" : "rocket_launch"} size={16} className={retrying ? "animate-spin" : undefined} />
        </button>
        <button
          type="button"
          className="flex h-9 w-9 items-center justify-center rounded-[8px] border border-[var(--border-strong)] bg-transparent text-[var(--danger)] transition-colors hover:border-[var(--danger)] hover:bg-[var(--danger-tint)] disabled:opacity-60"
          onClick={() => onDelete(app)}
          disabled={loading.delete === app.id}
          title={t("frontendApps.delete")}
        >
          <Icon name={loading.delete === app.id ? "progress_activity" : "delete"} size={16} className={loading.delete === app.id ? "animate-spin" : undefined} />
        </button>
      </div>
    </div>
  );
}

// --- Main Component ---

export default function AppsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const { data: apps, isLoading, error } = useApps();
  const deleteApp = useDeleteApp();

  const { data: me } = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/me', { credentials: 'include' })
      if (!res.ok) return null
      return res.json() as Promise<{ id: string; email: string; name: string; role: string; language: string }>
    },
    retry: false,
  });

  // Frontend apps state
  const [frontendApps, setFrontendApps] = useState<FrontendApp[]>([]);
  const [feLoading, setFeLoading] = useState(true);
  const [feError, setFeError] = useState<string | null>(null);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [baseDomain, setBaseDomain] = useState("");
  const [deployRetrying, setDeployRetrying] = useState<string | null>(null);
  const [deletingFe, setDeletingFe] = useState<string | null>(null);
  const [settingDomain, setSettingDomain] = useState(false);

  // Domain modal
  const [domainApp, setDomainApp] = useState<FrontendApp | null>(null);
  const [domainSub, setDomainSub] = useState("");

  // Type selection modal
  const [showTypeModal, setShowTypeModal] = useState(false);

  // Build with AI drawer (ai-build-chat T12) — the entry point is disabled
  // whenever the provider isn't configured/enabled (AIBC-18), instead of
  // opening a chat that can't call a model.
  const [showBuildWithAI, setShowBuildWithAI] = useState(false);
  const { data: aiProviderStatus } = useAIProviderStatus();
  const aiProviderReady = Boolean(aiProviderStatus?.enabled && aiProviderStatus?.has_key);

  // Frontend create modal
  const [showFeCreate, setShowFeCreate] = useState(false);
  const [newFeName, setNewFeName] = useState("");
  const [newFeTemplateId, setNewFeTemplateId] = useState("");
  const [newFeSubdomain, setNewFeSubdomain] = useState("");
  const [feCreating, setFeCreating] = useState(false);
  const [feCreateError, setFeCreateError] = useState<string | null>(null);

  // Sync modal
  const [syncApp, setSyncApp] = useState<FrontendApp | null>(null);
  const [syncInfo, setSyncInfo] = useState<{ sync_status: string; public_key: string; error_message: string } | null>(null);
  const [syncLoading, setSyncLoading] = useState(false);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [revealing, setRevealing] = useState(false);

  // Delete
  const [deleteTarget, setDeleteTarget] = useState<AppDef | null>(null);
  const [feDeleteTarget, setFeDeleteTarget] = useState<FrontendApp | null>(null);

  const fetchFrontendApps = async () => {
    try {
      const res = await fetch("/dashboard/api/frontend-apps", { credentials: "include" });
      if (res.ok) setFrontendApps(await res.json());
      setFeError(null);
    } catch { setFeError("Failed to load"); }
  };

  const fetchTemplates = async () => {
    try {
      const res = await fetch("/dashboard/api/github/templates", { credentials: "include" });
      if (res.ok) setTemplates((await res.json()).filter((t: Template) => t.active));
    } catch { /* non-critical */ }
  };

  const fetchBaseDomain = async () => {
    try {
      const res = await fetch("/dashboard/api/deploy-provider/status", { credentials: "include" });
      if (res.ok) {
        const data = await res.json();
        setBaseDomain(data.base_domain || "");
      }
    } catch {}
  };

  useEffect(() => {
    Promise.all([fetchFrontendApps(), fetchTemplates(), fetchBaseDomain()]).finally(() => setFeLoading(false));
  }, []);

  // Handlers
  function handleEdit(app: AppDef) { navigate(`/apps/${app.id}`); }
  function handleUsers(app: AppDef) { navigate(`/apps/${app.id}?tab=users`); }

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    try { await deleteApp.mutateAsync(deleteTarget.id); setDeleteTarget(null); } catch { }
  }

  const handleFeCreate = async () => {
    setFeCreating(true); setFeCreateError(null);
    try {
      const res = await fetch("/dashboard/api/frontend-apps", {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: newFeName, template_id: newFeTemplateId, subdomain: newFeSubdomain }),
      });
      const data = await res.json();
      if (!res.ok) { setFeCreateError(data.error || "Failed"); return; }
      setShowFeCreate(false); setNewFeName(""); setNewFeTemplateId(""); setNewFeSubdomain("");
      await fetchFrontendApps();
      qc.invalidateQueries({ queryKey: ["frontend-apps"] });
      if (data.status === "failed") setFeCreateError(`Repo creation failed: ${data.error_message}`);
    } finally { setFeCreating(false); }
  };

  const handleFeDelete = async (id: string) => {
    setDeletingFe(id);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${id}`, { method: "DELETE", credentials: "include" });
      if (res.ok) { setFrontendApps(prev => prev.filter(a => a.id !== id)); toast.success(t("frontendApps.deleted")); }
      else { const d = await res.json(); toast.error(d.error || t("frontendApps.deleteErr")); }
    } catch { toast.error(t("frontendApps.deleteErr")); }
    finally { setDeletingFe(null); setFeDeleteTarget(null); }
  };

  const handleDeployRetry = async (app: FrontendApp) => {
    setDeployRetrying(app.id);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${app.id}/deploy/retry`, { method: "POST", credentials: "include" });
      if (res.ok) toast.success(t("frontendApps.deployRetryOk"));
      else toast.error(t("frontendApps.deployRetryErr"));
      fetchFrontendApps();
    } catch { toast.error(t("frontendApps.deployRetryErr")); }
    finally { setDeployRetrying(null); }
  };

  const openDomainModal = (app: FrontendApp) => { setDomainApp(app); setDomainSub(app.custom_domain?.split(".")[0] || ""); };

  const handleSetDomain = async () => {
    if (!domainApp || !domainSub.trim()) return;
    setSettingDomain(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${domainApp.id}/custom-domain`, { method: "PUT", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ subdomain: domainSub }) });
      if (res.ok) { toast.success(t("frontendApps.domainSaved")); setDomainApp(null); setDomainSub(""); fetchFrontendApps(); }
      else { const d = await res.json(); toast.error(d.error || t("frontendApps.domainErr")); }
    } catch { toast.error(t("frontendApps.domainErr")); }
    finally { setSettingDomain(false); }
  };

  // Sync
  const openSync = async (app: FrontendApp) => {
    setSyncApp(app); setSyncInfo(null); setRevealedKey(null); setSyncLoading(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${app.id}/sync`, { credentials: "include" });
      if (res.ok) setSyncInfo(await res.json());
    } catch { toast.error(t("frontendApps.syncErr")); }
    finally { setSyncLoading(false); }
  };

  const handleReveal = async () => {
    if (!syncApp) return;
    setRevealing(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${syncApp.id}/reveal-key`, { method: "POST", credentials: "include" });
      if (res.ok) setRevealedKey((await res.json()).private_key);
      else toast.error(t("frontendApps.revealErr"));
    } catch { toast.error(t("frontendApps.revealErr")); }
    finally { setRevealing(false); }
  };

  const handleSyncRetry = async () => {
    if (!syncApp) return;
    setSyncLoading(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${syncApp.id}/sync/retry`, { method: "POST", credentials: "include" });
      if (res.ok) { const d = await res.json(); setSyncInfo({ sync_status: d.sync_status, public_key: d.public_key || "", error_message: d.error_message || "" }); toast.success(t("frontendApps.syncRetryOk")); }
      else toast.error(t("frontendApps.syncRetryErr"));
    } catch { toast.error(t("frontendApps.syncRetryErr")); }
    finally { setSyncLoading(false); }
  };

  const handleSyncRegenerate = async () => {
    if (!syncApp) return;
    setSyncLoading(true); setRevealedKey(null);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${syncApp.id}/sync/regenerate`, { method: "POST", credentials: "include" });
      if (res.ok) { const d = await res.json(); setSyncInfo({ sync_status: d.sync_status, public_key: d.public_key || "", error_message: d.error_message || "" }); toast.success(t("frontendApps.syncRegenOk")); }
      else toast.error(t("frontendApps.syncRegenErr"));
    } catch { toast.error(t("frontendApps.syncRegenErr")); }
    finally { setSyncLoading(false); }
  };

  const cloneCommand = syncApp?.github_repo_url
    ? `git clone ${syncApp.github_repo_url.replace("https://github.com/", "git@github.com:")}.git`
    : "";

  const agentPrompt = syncApp && revealedKey
    ? `You are configuring SSH-based Git access for a frontend app managed by Orbit.
The app "${syncApp.name}" has a GitHub repository. Your goal is to clone it and start working.

---
BEFORE YOU START
---
Ask the user where to clone, what to build, and any preferences. Do NOT clone to a default directory unless asked.

---
STEPS
---
Step 1 — Write the private key:
  cat > ~/.ssh/id_ed25519_${syncApp.slug} << 'KEYEOF'
${revealedKey}
KEYEOF

Step 2 — Set permissions:
  chmod 600 ~/.ssh/id_ed25519_${syncApp.slug}

Step 3 — Add SSH config:
  cat >> ~/.ssh/config << 'SSHEOF'

Host github.com-${syncApp.slug}
    HostName github.com
    IdentityFile ~/.ssh/id_ed25519_${syncApp.slug}
    IdentitiesOnly yes
SSHEOF

Step 4 — Clone into the chosen directory:
  GIT_SSH_COMMAND="ssh -i ~/.ssh/id_ed25519_${syncApp.slug} -o StrictHostKeyChecking=accept-new" ${cloneCommand} <user-path>

---
AFTER CLONE
---
- cd into the directory and start working.
- 'git push' works normally via the SSH config.`
    : "";

  const showEmpty = !isLoading && !error && (apps?.length ?? 0) === 0 && frontendApps.length === 0 && !feLoading;
  const showGrid = !isLoading && !error && ((apps?.length ?? 0) > 0 || frontendApps.length > 0);

  return (
    <>
      <PageHeader
        title={t("apps.title")}
        subtitle={t("apps.subtitle")}
        actions={
          <div className="flex items-center gap-2.5">
            <button
              type="button"
              disabled={!aiProviderReady}
              title={aiProviderReady ? undefined : t("apps.createWithAiNotConfigured")}
              onClick={() => setShowBuildWithAI(true)}
              className={cn(
                "flex items-center gap-2 rounded-[10px] border border-[var(--accent)]/40 bg-[var(--accent-tint)] px-4 py-2.5 text-[13.5px] font-bold text-[var(--accent)]",
                aiProviderReady ? "cursor-pointer" : "cursor-not-allowed opacity-70",
              )}
            >
              <Icon name="auto_awesome" size={18} />
              {t("apps.createWithAi")}
              {!aiProviderReady && (
                <span className="ml-0.5 rounded-full bg-[var(--accent)]/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide">
                  {t("apps.soon")}
                </span>
              )}
            </button>
            <Button className="gap-2" onClick={() => setShowTypeModal(true)}>
              <Icon name="add" size={16} />
              {t("apps.create")}
            </Button>
          </div>
        }
      />

      {isLoading && <LoadingState rows={4} />}

      {!isLoading && error && (
        <ErrorState title={t("apps.error")} description={(error as Error).message} />
      )}

      {showEmpty && (
        <EmptyState
          icon="grid_view"
          title={t("apps.emptyTitle")}
          description={t("apps.emptyDesc")}
          action={{ label: t("apps.create"), onClick: () => setShowTypeModal(true) }}
        />
      )}

      {showGrid && (
        <>
          {(apps?.length ?? 0) > 0 && (
            <>
              <SectionHeader icon="grid_view" label={t("apps.backendSection")} count={apps?.length ?? 0} />
              <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,340px))] gap-4">
                {(apps || []).map((app) => (
                  <AppCard key={app.id} app={app} currentUserId={me?.id}
                    onEdit={handleEdit} onDelete={setDeleteTarget} onUsers={handleUsers} />
                ))}
              </div>
            </>
          )}

          {frontendApps.length > 0 && (
            <div className={cn((apps?.length ?? 0) > 0 && "mt-10")}>
              <SectionHeader icon="language" label={t("apps.frontendSection")} count={frontendApps.length} />
              <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,340px))] gap-4">
                {frontendApps.map((app) => (
                  <FrontendCard key={app.id} app={app}
                    onSync={openSync}
                    onDelete={(a) => setFeDeleteTarget(a)}
                    onDeployRetry={handleDeployRetry}
                    onSetDomain={openDomainModal}
                    loading={{ deployRetry: deployRetrying, delete: deletingFe }} />
                ))}
              </div>
            </div>
          )}
        </>
      )}

      {feError && !showGrid && !isLoading && (
        <ErrorState title={t("apps.error")} description={feError} />
      )}

      {/* Type selection modal */}
      <Dialog open={showTypeModal} onOpenChange={setShowTypeModal}>
        <DialogContent className="max-w-[620px] p-8">
          <DialogHeader className="mb-4">
            <DialogTitle className="text-[21px] font-bold" style={{ fontFamily: "var(--font-display)" }}>
              {t("apps.typeModalTitle")}
            </DialogTitle>
            <DialogDescription className="text-[13.5px]">
              <Trans
                i18nKey="apps.typeModalDesc"
                components={{
                  1: (
                    <a
                      href="#"
                      onClick={(e) => e.preventDefault()}
                      className="text-[var(--primary)] hover:underline"
                    />
                  ),
                }}
              />
            </DialogDescription>
          </DialogHeader>
          <div className="grid grid-cols-2 gap-3.5">
            <button
              onClick={() => { setShowTypeModal(false); navigate("/apps/new"); }}
              className="rounded-[12px] border border-[var(--border-strong)] p-[18px] text-left transition-colors hover:border-[var(--primary)]"
            >
              <div className="mb-3 flex size-9 items-center justify-center rounded-[9px]"
                style={{ background: "var(--primary-tint)", color: "var(--primary)" }}>
                <Icon name="grid_view" size={20} />
              </div>
              <div className="mb-1.5 text-sm font-bold text-[var(--text-primary)]">{t("apps.typeBackend")}</div>
              <p className="text-[12.5px] leading-relaxed text-[var(--text-secondary)]">{t("apps.typeBackendDesc")}</p>
            </button>

            <button
              onClick={() => { setShowTypeModal(false); setShowFeCreate(true); setFeCreateError(null); setNewFeName(""); setNewFeTemplateId(""); setNewFeSubdomain(""); fetchTemplates(); }}
              className="rounded-[12px] border border-[var(--border-strong)] p-[18px] text-left transition-colors hover:border-[var(--primary)]"
            >
              <div className="mb-3 flex size-9 items-center justify-center rounded-[9px]"
                style={{ background: "var(--primary-tint)", color: "var(--primary)" }}>
                <Icon name="language" size={20} />
              </div>
              <div className="mb-1.5 text-sm font-bold text-[var(--text-primary)]">{t("apps.typeFrontend")}</div>
              <p className="text-[12.5px] leading-relaxed text-[var(--text-secondary)]">{t("apps.typeFrontendDesc")}</p>
            </button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Frontend create modal */}
      <Dialog open={showFeCreate} onOpenChange={setShowFeCreate}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.createTitle")}</DialogTitle>
            <DialogDescription>{t("frontendApps.createDesc")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="fe-name">{t("frontendApps.formName")}</Label>
              <Input id="fe-name" value={newFeName} onChange={(e) => setNewFeName(e.target.value)}
                placeholder={t("frontendApps.formNamePlaceholder")} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="fe-template">{t("frontendApps.formTemplate")}</Label>
              <Select value={newFeTemplateId} onValueChange={setNewFeTemplateId}>
                <SelectTrigger id="fe-template">
                  <SelectValue placeholder={t("frontendApps.selectTemplate")} />
                </SelectTrigger>
                <SelectContent>
                  {templates.map((tmpl) => (
                    <SelectItem key={tmpl.id} value={tmpl.id}>
                      <span>{tmpl.name}</span><span className="ml-2 text-xs text-[var(--text-tertiary)]">{tmpl.framework}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="fe-subdomain">{t("frontendApps.formSubdomain")}</Label>
              <Input id="fe-subdomain" value={newFeSubdomain} onChange={(e) => setNewFeSubdomain(e.target.value)}
                placeholder="meuapp" />
            </div>
            {feCreateError && <p className="text-[12px] text-[var(--danger)]">{feCreateError}</p>}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowFeCreate(false)}>
              {t("frontendApps.cancel")}
            </Button>
            <Button onClick={handleFeCreate} disabled={!newFeName.trim() || !newFeTemplateId || feCreating} className="gap-2">
              {feCreating && <Icon name="progress_activity" size={16} className="animate-spin" />}
              {t("frontendApps.creating")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Sync modal */}
      <Dialog open={!!syncApp} onOpenChange={() => { setSyncApp(null); setRevealedKey(null); }}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.syncTitle")}{syncApp ? ` — ${syncApp.name}` : ""}</DialogTitle>
            <DialogDescription>{t("frontendApps.syncDesc")}</DialogDescription>
          </DialogHeader>

          {syncLoading ? (
            <div className="flex items-center justify-center py-10">
              <Icon name="progress_activity" size={24} className="animate-spin text-[var(--text-tertiary)]" />
            </div>
          ) : syncInfo ? (
            <div className="min-w-0 space-y-4">
              <div className="flex items-center justify-between rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3">
                <span className="text-sm text-[var(--text-secondary)]">{t("frontendApps.syncStatus")}</span>
                {syncInfo.sync_status === "ready" ? (
                  <StatusPill label={t("frontendApps.syncReady")} tone="success" />
                ) : syncInfo.sync_status === "pending" ? (
                  <StatusPill label={t("frontendApps.syncPending")} tone="warning" />
                ) : (
                  <StatusPill label={t("frontendApps.syncFailed")} tone="danger" />
                )}
              </div>

              {syncInfo.error_message && (
                <div className="rounded-[10px] border border-[var(--danger)]/20 bg-[var(--danger-tint)] p-3 text-xs text-[var(--danger)]">
                  {syncInfo.error_message}
                </div>
              )}

              <div className="space-y-2">
                <Label>{t("frontendApps.cloneCommand")}</Label>
                <div className="flex items-center gap-2">
                  <code className="flex-1 break-all rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-3 py-2 font-mono text-xs text-[var(--text-secondary)]">{cloneCommand}</code>
                  <Button size="icon" variant="outline" className="size-9 shrink-0"
                    onClick={() => { navigator.clipboard.writeText(cloneCommand); toast.success(t("frontendApps.copied")); }}
                    title={t("frontendApps.copy")}><Icon name="content_copy" size={16} /></Button>
                </div>
              </div>

              {syncInfo.sync_status === "ready" && !revealedKey && (
                <>
                  <p className="text-center text-xs text-[var(--text-secondary)]">{t("frontendApps.syncReadyDesc")}</p>
                  <Button onClick={handleReveal} disabled={revealing} className="w-full gap-2">
                    {revealing ? <Icon name="progress_activity" size={16} className="animate-spin" /> : <Icon name="key" size={16} />}
                    {t("frontendApps.viewInstructions")}
                  </Button>
                </>
              )}

              {revealedKey && (
                <div className="space-y-3">
                  <div className="space-y-1.5">
                    <Label>{t("frontendApps.privateKey")}</Label>
                    <code className="block max-h-32 overflow-auto whitespace-pre-wrap break-all rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3 font-mono text-xs text-[var(--text-primary)]">{revealedKey}</code>
                  </div>
                  <div className="space-y-1.5">
                    <Label>{t("frontendApps.agentPrompt")}</Label>
                    <div className="relative">
                      <code className="block max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3 pr-10 font-mono text-xs text-[var(--text-secondary)]">{agentPrompt}</code>
                      <Button size="icon" variant="outline" className="absolute right-1.5 top-1.5 size-8"
                        onClick={() => { navigator.clipboard.writeText(agentPrompt); toast.success(t("frontendApps.promptCopied")); }}
                        title={t("frontendApps.copy")}><Icon name="content_copy" size={15} /></Button>
                    </div>
                  </div>
                </div>
              )}

              <div className="flex gap-2">
                {(syncInfo.sync_status === "pending" || syncInfo.sync_status === "failed") && (
                  <Button size="sm" variant="outline" className="gap-1.5 text-[var(--warning)]" onClick={handleSyncRetry}>
                    <Icon name="refresh" size={15} />{t("frontendApps.syncRetry")}
                  </Button>
                )}
                {syncInfo.sync_status === "ready" && (
                  <Button size="sm" variant="outline" className="gap-1.5 text-[var(--warning)]" onClick={handleSyncRegenerate}>
                    <Icon name="refresh" size={15} />{t("frontendApps.syncRegenerate")}
                  </Button>
                )}
              </div>
            </div>
          ) : (
            <div className="py-10 text-center text-sm text-[var(--text-tertiary)]">{t("frontendApps.noSyncInfo")}</div>
          )}
        </DialogContent>
      </Dialog>

      {/* Backend delete dialog */}
      <ConfirmDialog
        open={Boolean(deleteTarget)}
        title={t("apps.deleteTitle")}
        message={<Trans i18nKey="apps.deleteDesc" values={{ name: deleteTarget?.name ?? "" }} />}
        confirmLabel={deleteApp.isPending ? t("apps.deleting") : t("apps.deleteConfirm")}
        cancelLabel={t("apps.deleteCancel")}
        destructive
        icon="delete"
        loading={deleteApp.isPending}
        onConfirm={handleConfirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      {/* Frontend delete dialog */}
      <ConfirmDialog
        open={!!feDeleteTarget}
        title={t("frontendApps.deleteConfirmTitle")}
        message={t("frontendApps.deleteConfirmDesc")}
        confirmLabel={deletingFe ? t("apps.deleting") : t("frontendApps.delete")}
        cancelLabel={t("apps.deleteCancel")}
        destructive
        icon="delete"
        loading={!!deletingFe}
        onConfirm={() => feDeleteTarget && handleFeDelete(feDeleteTarget.id)}
        onCancel={() => setFeDeleteTarget(null)}
      />

      {/* Domain modal */}
      <Dialog open={!!domainApp} onOpenChange={() => { setDomainApp(null); setDomainSub(""); }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.domainTitle")}</DialogTitle>
            <DialogDescription>{t("frontendApps.domainDesc")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            {!baseDomain ? (
              <p className="text-xs text-[var(--danger)]">{t("frontendApps.noBaseDomain")}</p>
            ) : (
              <>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="subdomain-input">{t("frontendApps.subdomain")}</Label>
                  <Input
                    id="subdomain-input"
                    value={domainSub}
                    onChange={(e) => {
                      const val = e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "");
                      setDomainSub(val);
                    }}
                    placeholder="meuapp"
                    className="font-mono"
                  />
                </div>
                <div className="rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3 text-center">
                  <span className="text-xs text-[var(--text-tertiary)]">{t("frontendApps.domainPreview")}</span>
                  <div className="mt-0.5 break-all font-mono text-sm font-bold text-[var(--success)]">
                    {domainSub || "..."}.{baseDomain}
                  </div>
                </div>
                <p className="mt-2 text-center text-[10px] text-[var(--text-tertiary)]">
                  {t("frontendApps.dnsHint")}<br/>
                  <code className="text-[var(--text-secondary)]">{domainSub || "..."}.{baseDomain} → {domainApp?.deploy_url?.replace("https://", "") || "..."}</code>
                </p>
              </>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setDomainApp(null); setDomainSub(""); }}>
              {t("frontendApps.cancel")}
            </Button>
            <Button onClick={handleSetDomain} disabled={!domainSub.trim() || !baseDomain || settingDomain} className="gap-2">
              {settingDomain && <Icon name="progress_activity" size={16} className="animate-spin" />}
              {t("frontendApps.saveDomain")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <BuildWithAIDrawer open={showBuildWithAI} onOpenChange={setShowBuildWithAI} />
    </>
  );
}
