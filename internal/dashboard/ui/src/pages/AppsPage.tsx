import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import {
  Plus,
  Pencil,
  Trash2,
  Table2,
  Mail,
  MailX,
  LayoutGrid,
  Users,
  User,
  BookOpen,
  Globe,
  Key,
  CheckCircle2,
  XCircle,
  RotateCcw,
  Loader2,
  Rocket,
} from "lucide-react";
import { useApps, useDeleteApp, AppDef } from "../lib/api";
import DeleteConfirmDialog from "../components/DeleteConfirmDialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
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
import { cn } from "@/lib/utils";
import { toast } from "sonner";

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

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

// --- Skeleton ---

function SkeletonCard() {
  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.03] p-5">
      <div className="relative overflow-hidden rounded-xl bg-white/[0.03]">
        <div className="absolute inset-0 animate-[shimmer_1.6s_infinite] bg-gradient-to-r from-transparent via-white/[0.04] to-transparent" />
        <div className="mb-3 h-4 w-3/5 rounded bg-white/[0.07]" />
        <div className="mb-2 h-3 w-2/5 rounded bg-white/[0.05]" />
        <div className="h-3 w-1/3 rounded bg-white/[0.05]" />
      </div>
    </div>
  );
}

// --- Backend App Card ---

interface AppCardProps {
  app: AppDef;
  index: number;
  isSuperadmin?: boolean;
  onEdit: (app: AppDef) => void;
  onDelete: (app: AppDef) => void;
  onUsers: (app: AppDef) => void;
}

function AppCard({ app, index, isSuperadmin, onEdit, onDelete, onUsers }: AppCardProps) {
  const { t } = useTranslation();
  const createdAt = new Date(app.created_at).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
  const initial = app.name.charAt(0).toUpperCase();

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease, delay: index * 0.07 }}
      className="group relative rounded-2xl border border-white/[0.06] bg-white/[0.03] p-5 transition-all duration-200 hover:bg-white/[0.06] brand-border-hover"
    >
      <div className="absolute left-5 right-5 top-0 h-[2px] rounded-full brand-accent-bar opacity-0 transition-opacity duration-300 group-hover:opacity-100" />

      <div className="flex items-center gap-3.5">
        <div
          className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-white/[0.06] text-[15px] font-extrabold"
          style={{
            background: "linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.15), rgba(var(--brand-secondary-rgb), 0.15))",
            color: "var(--brand-light)",
          }}
        >
          {initial}
        </div>
        <div className="min-w-0 flex-1 flex items-center justify-between gap-2">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-bold text-[#F8FAFC]">{app.name}</h3>
            <div className="flex flex-wrap gap-1.5 mt-1">
              <Badge className="gap-1 text-[10px]" variant="outline"
                style={{ borderColor: "rgba(var(--brand-primary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-primary-rgb), 0.1)", color: "var(--brand-light)" }}>
                <Table2 size={10} strokeWidth={1.5} />
                {app.tables?.length ?? 0} {t("apps.table", { count: app.tables?.length ?? 0 })}
              </Badge>
              <Badge className={cn("gap-1 text-[10px]", app.auth_email_enabled ? "text-purple-300 hover:bg-white/[0.08]" : "border-white/[0.10] bg-white/[0.05] text-[#94A3B8] hover:bg-white/[0.08]")}
                variant="outline"
                style={app.auth_email_enabled ? { borderColor: "rgba(var(--brand-secondary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-secondary-rgb), 0.1)" } : undefined}>
                {app.auth_email_enabled ? <Mail size={10} strokeWidth={1.5} /> : <MailX size={10} strokeWidth={1.5} />}
              </Badge>
            </div>
          </div>
          <span className="text-[10px] text-[#64748B] tracking-wide shrink-0 whitespace-nowrap">{createdAt}</span>
        </div>
      </div>

      <div className="mt-3 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          {isSuperadmin && (app.owner_name || app.owner_email) && (
            <span className="flex items-center gap-1 text-[10px] text-[#64748B] truncate">
              <User size={10} strokeWidth={1.5} className="shrink-0" />
              <span className="truncate">{app.owner_name || app.owner_email}</span>
            </span>
          )}
        </div>
        <div className="flex gap-1">
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => onUsers(app)} title="Usuários"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-white">
              <Users size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => window.open(`/docs/${app.name}`, '_blank')} title="API Docs"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-white">
              <BookOpen size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => onEdit(app)} title="Editar"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-white">
              <Pencil size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => onDelete(app)} title="Deletar"
              className="size-7 rounded-lg border-red-500/20 bg-red-500/[0.06] text-red-400 hover:bg-red-500/10 hover:text-red-400">
              <Trash2 size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
        </div>
      </div>
    </motion.div>
  );
}

// --- Frontend App Card ---

interface FrontendCardProps {
  app: FrontendApp;
  index: number;
  onSync: (app: FrontendApp) => void;
  onDelete: (app: FrontendApp) => void;
  onDeployRetry: (app: FrontendApp) => void;
  onSetDomain: (app: FrontendApp) => void;
}

function FrontendCard({ app, index, onSync, onDelete, onDeployRetry, onSetDomain }: FrontendCardProps) {
  const { t } = useTranslation();
  const createdAt = new Date(app.created_at).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
  const initial = app.name.charAt(0).toUpperCase();

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease, delay: index * 0.07 }}
      className="group relative rounded-2xl border border-white/[0.06] bg-white/[0.03] p-5 transition-all duration-200 hover:bg-white/[0.06] brand-border-hover"
    >
      <div className="absolute left-5 right-5 top-0 h-[2px] rounded-full brand-accent-bar opacity-0 transition-opacity duration-300 group-hover:opacity-100" />

      <div className="flex items-center gap-3.5">
        <div
          className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-white/[0.06] text-[15px] font-extrabold"
          style={{
            background: "linear-gradient(to bottom right, rgba(34, 197, 94, 0.15), rgba(16, 185, 129, 0.15))",
            color: "#22C55E",
          }}
        >
          <Globe size={18} strokeWidth={2} />
        </div>
        <div className="min-w-0 flex-1 flex items-center justify-between gap-2">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-bold text-[#F8FAFC]">{app.name}</h3>
            <div className="flex flex-wrap gap-1.5 mt-1">
              <code className="text-[10px] text-[#64748B] bg-white/[0.04] px-1.5 py-0.5 rounded">{app.slug}</code>
              <Badge className="gap-1 text-[10px]" variant="outline"
                style={{ borderColor: "rgba(var(--brand-primary-rgb), 0.15)", backgroundColor: "rgba(var(--brand-primary-rgb), 0.08)", color: "var(--brand-light)" }}>
                {app.template_name}
              </Badge>
              {app.status === "ready" ? (
                <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-[#22C55E]">
                  <CheckCircle2 size={10} />{t("frontendApps.statusReady", "Ready")}
                </span>
              ) : (
                <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-[#EF4444]" title={app.error_message}>
                  <XCircle size={10} />{t("frontendApps.statusFailed", "Failed")}
                </span>
              )}
              {app.status === "ready" && app.deploy_status && app.deploy_status !== "pending" && (
                app.deploy_status === "ready" ? (
                  <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-[#3B82F6]" title={app.deploy_url}>
                    <CheckCircle2 size={10} />Deploy
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-[#EF4444]" title={app.deploy_error_message}>
                    <XCircle size={10} />Deploy failed
                  </span>
                )
              )}
              {app.deploy_status === "ready" && app.deploy_url && (
                <div className="flex items-center gap-1 mt-1">
                  <a href={app.deploy_url} target="_blank" rel="noopener noreferrer"
                    className="text-[10px] text-[#3B82F6] hover:underline">
                    {app.deploy_url.replace("https://", "")}
                  </a>
                  <button
                    onClick={() => onSetDomain(app)}
                    title="Set custom domain"
                    className="text-[#64748B] hover:text-[#94A3B8]"
                  >
                    <Pencil size={10} />
                  </button>
                </div>
              )}
            </div>
          </div>
          <span className="text-[10px] text-[#64748B] tracking-wide shrink-0 whitespace-nowrap">{createdAt}</span>
        </div>
      </div>

      <div className="mt-3 flex items-center justify-end gap-1">
        {app.status === "ready" && (
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => onSync(app)} title="Sync"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#3B82F6] hover:bg-[#3B82F6]/10 hover:text-[#60A5FA]">
              <Key size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
        )}
        {app.status === "failed" && (
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => onSync(app)} title="Retry"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#F59E0B] hover:bg-[#F59E0B]/10">
              <RotateCcw size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
        )}
        {app.status === "ready" && app.deploy_status === "failed" && (
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button variant="outline" size="icon" onClick={() => onDeployRetry(app)} title="Retry deploy"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#F59E0B] hover:bg-[#F59E0B]/10">
              <Rocket size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
        )}
        <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
          <Button variant="outline" size="icon" onClick={() => onDelete(app)} title="Deletar"
            className="size-7 rounded-lg border-red-500/20 bg-red-500/[0.06] text-red-400 hover:bg-red-500/10 hover:text-red-400">
            <Trash2 size={12} strokeWidth={1.5} />
          </Button>
        </motion.div>
      </div>
    </motion.div>
  );
}

// --- Empty State ---

function EmptyState({ onCreateClick }: { onCreateClick: () => void }) {
  const { t } = useTranslation();
  return (
    <motion.div {...fadeUp} className="flex min-h-[360px] items-center justify-center">
      <div className="relative w-full max-w-[380px] overflow-hidden rounded-3xl border border-white/[0.10] bg-white/[0.05] p-1.5 text-center">
        <div className="relative z-[1] rounded-[20px] bg-white/[0.03] px-8 py-10 shadow-[inset_0_1px_1px_rgba(255,255,255,0.08)]">
          <motion.div
            animate={{ y: [0, -6, 0] }}
            transition={{ duration: 2.5, repeat: Infinity, ease: "easeInOut" }}
            className="mx-auto mb-5 flex size-16 items-center justify-center rounded-[18px]"
            style={{ borderColor: "rgba(var(--brand-primary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)" }}
          >
            <LayoutGrid size={28} strokeWidth={1.5} style={{ color: "var(--brand-light)" }} />
          </motion.div>
          <h3 className="mb-2 text-base font-bold">{t("apps.emptyTitle")}</h3>
          <p className="mb-6 text-[13px] leading-relaxed text-[#94A3B8]">{t("apps.emptyDesc")}</p>
          <motion.div whileHover={{ scale: 1.02 }} whileTap={{ scale: 0.98 }} className="inline-flex">
            <Button onClick={onCreateClick}
              className="gap-2 rounded-3xl px-[22px] py-2.5 text-sm font-semibold text-white border-0 hover:opacity-90"
              style={{ background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))" }}>
              {t("apps.create")}
              <span className="flex size-[22px] items-center justify-center rounded-full bg-white/[0.15]"><Plus size={12} strokeWidth={2} /></span>
            </Button>
          </motion.div>
        </div>
      </div>
    </motion.div>
  );
}

function ErrorState({ message }: { message: string }) {
  const { t } = useTranslation();
  return <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">{t("apps.error")}: {message}</div>;
}

// --- Section Header ---

function SectionHeader({ icon: Icon, label }: { icon: typeof Globe; label: string }) {
  return (
    <div className="flex items-center gap-2 mt-2 mb-3">
      <Icon size={14} className="text-[#64748B]" />
      <span className="text-xs font-semibold uppercase tracking-wider text-[#64748B]">{label}</span>
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
  const isSuperadmin = me?.role === 'superadmin';

  // Frontend apps state
  const [frontendApps, setFrontendApps] = useState<FrontendApp[]>([]);
  const [feLoading, setFeLoading] = useState(true);
  const [feError, setFeError] = useState<string | null>(null);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [baseDomain, setBaseDomain] = useState("");

  // Domain modal
  const [domainApp, setDomainApp] = useState<FrontendApp | null>(null);
  const [domainSub, setDomainSub] = useState("");

  // Type selection modal
  const [showTypeModal, setShowTypeModal] = useState(false);

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
  function handleUsers(app: AppDef) { navigate(`/apps/${app.id}/users`); }

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
    await fetch(`/dashboard/api/frontend-apps/${id}`, { method: "DELETE", credentials: "include" });
    setFrontendApps(prev => prev.filter(a => a.id !== id));
    setFeDeleteTarget(null);
  };

  const handleDeployRetry = async (app: FrontendApp) => {
    await fetch(`/dashboard/api/frontend-apps/${app.id}/deploy/retry`, { method: "POST", credentials: "include" });
    fetchFrontendApps();
  };

  const openDomainModal = (app: FrontendApp) => {
    setDomainApp(app);
    setDomainSub(app.custom_domain?.split(".")[0] || "");
  };

  const handleSetDomain = async () => {
    if (!domainApp || !domainSub.trim()) return;
    await fetch(`/dashboard/api/frontend-apps/${domainApp.id}/custom-domain`, {
      method: "PUT", credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ subdomain: domainSub }),
    });
    setDomainApp(null);
    setDomainSub("");
    fetchFrontendApps();
  };

  // Sync
  const openSync = async (app: FrontendApp) => {
    setSyncApp(app); setSyncInfo(null); setRevealedKey(null); setSyncLoading(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${app.id}/sync`, { credentials: "include" });
      if (res.ok) setSyncInfo(await res.json());
    } finally { setSyncLoading(false); }
  };

  const handleReveal = async () => {
    if (!syncApp) return;
    setRevealing(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${syncApp.id}/reveal-key`, { method: "POST", credentials: "include" });
      if (res.ok) setRevealedKey((await res.json()).private_key);
    } finally { setRevealing(false); }
  };

  const handleSyncRetry = async () => {
    if (!syncApp) return;
    setSyncLoading(true);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${syncApp.id}/sync/retry`, { method: "POST", credentials: "include" });
      if (res.ok) { const d = await res.json(); setSyncInfo({ sync_status: d.sync_status, public_key: d.public_key || "", error_message: d.error_message || "" }); }
    } finally { setSyncLoading(false); }
  };

  const handleSyncRegenerate = async () => {
    if (!syncApp) return;
    setSyncLoading(true); setRevealedKey(null);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${syncApp.id}/sync/regenerate`, { method: "POST", credentials: "include" });
      if (res.ok) { const d = await res.json(); setSyncInfo({ sync_status: d.sync_status, public_key: d.public_key || "", error_message: d.error_message || "" }); }
    } finally { setSyncLoading(false); }
  };

  const cloneCommand = syncApp?.github_repo_url
    ? `git clone ${syncApp.github_repo_url.replace("https://github.com/", "git@github.com:")}.git`
    : "";

  const agentPrompt = syncApp && revealedKey
    ? `You are configuring SSH-based Git access for a frontend app managed by Zeep Orbit.
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

  const hasBoth = (apps?.length ?? 0) > 0 && frontendApps.length > 0;

  return (
    <>
      <div className="relative z-10">
        <motion.div {...fadeUp} className="mb-9">
          <span className="mb-3 inline-block rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
            style={{ borderColor: "rgba(var(--brand-primary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)", color: "var(--brand-light)" }}>
            Apps
          </span>

          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 className="mb-1.5 text-[28px] font-extrabold leading-tight">{t("apps.title")}</h2>
              <p className="text-sm text-[#94A3B8]">{t("apps.subtitle")}</p>
            </div>
            <motion.div whileHover={{ scale: 1.02 }} whileTap={{ scale: 0.98 }} className="shrink-0">
              <Button onClick={() => setShowTypeModal(true)}
                className="gap-2 rounded-3xl px-5 py-2.5 text-sm font-semibold text-white border-0 hover:opacity-90"
                style={{ background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))" }}>
                {t("apps.create")}
                <span className="flex size-6 items-center justify-center rounded-full bg-white/[0.12]"><Plus size={12} strokeWidth={2} /></span>
              </Button>
            </motion.div>
          </div>
        </motion.div>

        <AnimatePresence mode="wait">
          {isLoading && (
            <motion.div key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}
              className="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-4">
              <style>{`@keyframes shimmer { 0%{transform:translateX(-100%)} 100%{transform:translateX(100%)} }`}</style>
              <SkeletonCard /><SkeletonCard /><SkeletonCard /><SkeletonCard />
            </motion.div>
          )}

          {!isLoading && error && (
            <motion.div key="error" initial={{ opacity: 0 }} animate={{ opacity: 1 }}><ErrorState message={(error as Error).message} /></motion.div>
          )}

          {!isLoading && !error && apps && apps.length === 0 && frontendApps.length === 0 && (
            <motion.div key="empty" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
              <EmptyState onCreateClick={() => setShowTypeModal(true)} />
            </motion.div>
          )}

          {!isLoading && !error && ((apps?.length ?? 0) > 0 || frontendApps.length > 0) && (
            <motion.div key="grid" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
              {hasBoth && <SectionHeader icon={LayoutGrid} label={t("apps.backendSection", "Backend Apps")} />}
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                {(apps || []).map((app, i) => (
                  <AppCard key={app.id} app={app} index={i} isSuperadmin={isSuperadmin}
                    onEdit={handleEdit} onDelete={setDeleteTarget} onUsers={handleUsers} />
                ))}
              </div>

              {frontendApps.length > 0 && (
                <>
                  {hasBoth && <SectionHeader icon={Globe} label={t("apps.frontendSection", "Frontend Apps")} />}
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-4 mt-2">
                    {frontendApps.map((app, i) => (
                      <FrontendCard key={app.id} app={app} index={i}
                        onSync={openSync}
                        onDelete={(a) => setFeDeleteTarget(a)}
                        onDeployRetry={handleDeployRetry}
                        onSetDomain={openDomainModal} />
                    ))}
                  </div>
                </>
              )}
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* Type selection modal */}
      <Dialog open={showTypeModal} onOpenChange={setShowTypeModal}>
        <DialogContent className="bg-[#0F0F17] border-white/[0.08] text-[#F8FAFC] max-w-md">
          <DialogHeader>
            <DialogTitle>{t("apps.typeModalTitle", "What type of app do you want to create?")}</DialogTitle>
            <DialogDescription className="text-[#94A3B8]">
              {t("apps.typeModalDesc", "Choose the type that fits what you need.")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <button
              onClick={() => { setShowTypeModal(false); navigate("/apps/new"); }}
              className="w-full text-left p-4 rounded-xl border border-white/[0.08] bg-white/[0.03] hover:bg-white/[0.06] hover:border-[rgba(var(--brand-primary-rgb),0.25)] transition-all group"
            >
              <div className="flex items-center gap-3 mb-1.5">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-lg"
                  style={{ background: "rgba(var(--brand-primary-rgb), 0.15)" }}>
                  <LayoutGrid size={16} style={{ color: "var(--brand-light)" }} />
                </div>
                <span className="font-semibold text-sm">{t("apps.typeBackend", "Backend App")}</span>
              </div>
              <p className="text-xs text-[#64748B] ml-12">
                {t("apps.typeBackendDesc", "Database, API, user authentication, and data management. Ideal for apps that need to store and manage data.")}
              </p>
            </button>

            <button
              onClick={() => { setShowTypeModal(false); setShowFeCreate(true); setFeCreateError(null); setNewFeName(""); setNewFeTemplateId(""); setNewFeSubdomain(""); fetchTemplates(); }}
              className="w-full text-left p-4 rounded-xl border border-white/[0.08] bg-white/[0.03] hover:bg-white/[0.06] hover:border-[rgba(34,197,94,0.25)] transition-all group"
            >
              <div className="flex items-center gap-3 mb-1.5">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-lg"
                  style={{ background: "rgba(34,197,94,0.15)" }}>
                  <Globe size={16} className="text-[#22C55E]" />
                </div>
                <span className="font-semibold text-sm">{t("apps.typeFrontend", "Frontend App")}</span>
              </div>
              <p className="text-xs text-[#64748B] ml-12">
                {t("apps.typeFrontendDesc", "Website or web app with automatic GitHub repository and deployment. Ideal for landing pages, dashboards, and static sites.")}
              </p>
            </button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Frontend create modal */}
      <Dialog open={showFeCreate} onOpenChange={setShowFeCreate}>
        <DialogContent className="bg-[#0F0F17] border-white/[0.08] text-[#F8FAFC] max-w-md">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.createTitle", "New Frontend App")}</DialogTitle>
            <DialogDescription className="text-[#94A3B8]">
              {t("frontendApps.createDesc", "Choose a name and a GitHub template to generate a new repository.")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <Label htmlFor="fe-name" className="text-[#94A3B8] text-xs">{t("frontendApps.formName", "App Name")}</Label>
              <Input id="fe-name" value={newFeName} onChange={(e) => setNewFeName(e.target.value)}
                placeholder={t("frontendApps.formNamePlaceholder", "My Awesome App")}
                className="mt-1 bg-white/[0.04] border-white/[0.08] text-[#F8FAFC]" />
            </div>
            <div>
              <Label htmlFor="fe-template" className="text-[#94A3B8] text-xs">{t("frontendApps.formTemplate", "Template")}</Label>
              <Select value={newFeTemplateId} onValueChange={setNewFeTemplateId}>
                <SelectTrigger className="mt-1 bg-white/[0.04] border-white/[0.08] text-[#F8FAFC]">
                  <SelectValue placeholder={t("frontendApps.selectTemplate", "Select a template...")} />
                </SelectTrigger>
                <SelectContent className="bg-[#0F0F17] border-white/[0.08]">
                  {templates.map((tmpl) => (
                    <SelectItem key={tmpl.id} value={tmpl.id} className="text-[#F8FAFC]">
                      <span>{tmpl.name}</span><span className="ml-2 text-xs text-[#64748B]">{tmpl.framework}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label htmlFor="fe-subdomain" className="text-[#94A3B8] text-xs">{t("frontendApps.formSubdomain", "Subdomain (optional)")}</Label>
              <Input id="fe-subdomain" value={newFeSubdomain} onChange={(e) => setNewFeSubdomain(e.target.value)}
                placeholder="meuapp" className="mt-1 bg-white/[0.04] border-white/[0.08] text-[#F8FAFC]" />
            </div>
            {feCreateError && <p className="text-xs text-[#EF4444]">{feCreateError}</p>}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setShowFeCreate(false)} className="text-[#94A3B8] hover:text-[#F8FAFC]">
              {t("frontendApps.cancel", "Cancel")}
            </Button>
            <Button onClick={handleFeCreate} disabled={!newFeName.trim() || !newFeTemplateId || feCreating}
              className="bg-[#3B82F6] hover:bg-[#2563EB] text-white">
              {feCreating && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("frontendApps.creating", "Create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Sync modal */}
      <Dialog open={!!syncApp} onOpenChange={() => { setSyncApp(null); setRevealedKey(null); }}>
        <DialogContent className="bg-[#0F0F17] border-white/[0.08] text-[#F8FAFC] max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.syncTitle", "Sync Setup")}{syncApp ? ` — ${syncApp.name}` : ""}</DialogTitle>
            <DialogDescription className="text-[#94A3B8]">
              {t("frontendApps.syncDesc", "Configure SSH access so your AI agent can push code to this repository.")}
            </DialogDescription>
          </DialogHeader>

          {syncLoading ? (
            <div className="flex items-center justify-center py-10"><Loader2 className="h-6 w-6 animate-spin text-[#64748B]" /></div>
          ) : syncInfo ? (
            <div className="space-y-4 min-w-0">
              <div className="flex items-center justify-between p-3 rounded-lg bg-white/[0.04] border border-white/[0.06]">
                <span className="text-sm text-[#94A3B8]">{t("frontendApps.syncStatus", "Sync Status")}</span>
                {syncInfo.sync_status === "ready" ? (
                  <span className="inline-flex items-center gap-1 text-xs font-medium text-[#22C55E]"><CheckCircle2 className="h-3.5 w-3.5" />{t("frontendApps.syncReady", "Ready")}</span>
                ) : syncInfo.sync_status === "pending" ? (
                  <span className="inline-flex items-center gap-1 text-xs font-medium text-[#F59E0B]"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t("frontendApps.syncPending", "Pending")}</span>
                ) : (
                  <span className="inline-flex items-center gap-1 text-xs font-medium text-[#EF4444]"><XCircle className="h-3.5 w-3.5" />{t("frontendApps.syncFailed", "Failed")}</span>
                )}
              </div>

              {syncInfo.error_message && (
                <div className="flex items-start gap-2 p-3 rounded-lg bg-[#EF4444]/10 border border-[#EF4444]/20 text-xs text-[#EF4444]">
                  <span>{syncInfo.error_message}</span>
                </div>
              )}

              <div className="space-y-2">
                <Label className="text-[#94A3B8] text-xs">{t("frontendApps.cloneCommand", "Clone command")}</Label>
                <div className="flex items-center gap-2">
                  <code className="flex-1 text-xs text-[#94A3B8] bg-[#0F0F17] border border-white/[0.08] rounded px-3 py-2 font-mono break-all">{cloneCommand}</code>
                  <Button size="sm" variant="ghost" onClick={() => { navigator.clipboard.writeText(cloneCommand); toast.success("Copied!"); }}
                    title={t("frontendApps.copy", "Copy")}
                    className="h-8 text-xs text-[#64748B] hover:text-[#94A3B8] shrink-0"><Globe className="h-3.5 w-3.5" /></Button>
                </div>
              </div>

              {syncInfo.sync_status === "ready" && !revealedKey && (
                <Button onClick={handleReveal} disabled={revealing} className="w-full bg-[#3B82F6] hover:bg-[#2563EB] text-white">
                  {revealing && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  <Key className="h-4 w-4 mr-2" />{t("frontendApps.revealKey", "Reveal Private Key")}
                </Button>
              )}

              {revealedKey && (
                <div className="space-y-3">
                  <div>
                    <Label className="text-[#94A3B8] text-xs">{t("frontendApps.privateKey", "Private Key")}</Label>
                    <div className="relative mt-1 overflow-hidden rounded border border-white/[0.08]">
                      <code className="block text-xs text-[#F8FAFC] bg-[#0F0F17] rounded p-3 pr-10 font-mono whitespace-pre-wrap break-all max-h-32 overflow-auto">{revealedKey}</code>
                    </div>
                  </div>
                  <div>
                    <Label className="text-[#94A3B8] text-xs">{t("frontendApps.agentPrompt", "Copy this prompt for your AI agent")}</Label>
                    <div className="relative mt-1 overflow-hidden rounded border border-white/[0.08]">
                      <code className="block text-xs text-[#94A3B8] bg-[#0F0F17] rounded p-3 pr-10 font-mono whitespace-pre-wrap break-all max-h-48 overflow-auto">{agentPrompt}</code>
                      <Button size="sm" variant="ghost"
                        onClick={() => { navigator.clipboard.writeText(agentPrompt); toast.success("Prompt copied!"); }}
                        title={t("frontendApps.copy", "Copy")}
                        className="absolute top-1 right-1 h-7 text-xs text-[#64748B] hover:text-[#94A3B8]"><Globe className="h-3.5 w-3.5" /></Button>
                    </div>
                  </div>
                </div>
              )}

              <div className="flex gap-2">
                {(syncInfo.sync_status === "pending" || syncInfo.sync_status === "failed") && (
                  <Button size="sm" variant="outline" onClick={handleSyncRetry}
                    className="text-xs border-white/[0.08] text-[#F59E0B] hover:bg-[#F59E0B]/10">
                    <RotateCcw className="h-3.5 w-3.5 mr-1" />{t("frontendApps.syncRetry", "Retry")}
                  </Button>
                )}
                {syncInfo.sync_status === "ready" && (
                  <Button size="sm" variant="outline" onClick={handleSyncRegenerate}
                    className="text-xs border-white/[0.08] text-[#F59E0B] hover:bg-[#F59E0B]/10">
                    <RotateCcw className="h-3.5 w-3.5 mr-1" />{t("frontendApps.syncRegenerate", "Regenerate")}
                  </Button>
                )}
              </div>
            </div>
          ) : (
            <div className="text-center py-10 text-sm text-[#64748B]">{t("frontendApps.noSyncInfo", "No sync information available")}</div>
          )}
        </DialogContent>
      </Dialog>

      {/* Backend delete dialog */}
      <DeleteConfirmDialog
        open={Boolean(deleteTarget)}
        appName={deleteTarget?.name ?? ""}
        loading={deleteApp.isPending}
        onConfirm={handleConfirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      {/* Frontend delete dialog (simple) */}
      <Dialog open={!!feDeleteTarget} onOpenChange={() => setFeDeleteTarget(null)}>
        <DialogContent className="bg-[#0F0F17] border-white/[0.08] text-[#F8FAFC] max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.deleteConfirm", "Delete frontend app?")}</DialogTitle>
            <DialogDescription className="text-[#94A3B8]">
              {t("frontendApps.deleteConfirmDesc", "This will archive the GitHub repository and revoke its deploy key.")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setFeDeleteTarget(null)} className="text-[#94A3B8]">{t("frontendApps.cancel", "Cancel")}</Button>
            <Button onClick={() => feDeleteTarget && handleFeDelete(feDeleteTarget.id)} className="bg-[#EF4444] hover:bg-[#DC2626] text-white">
              {t("frontendApps.delete", "Delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {/* Domain modal */}
      <Dialog open={!!domainApp} onOpenChange={() => { setDomainApp(null); setDomainSub(""); }}>
        <DialogContent className="bg-[#0F0F17] border-white/[0.08] text-[#F8FAFC] max-w-sm">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.domainTitle", "Custom Domain")}</DialogTitle>
            <DialogDescription className="text-[#94A3B8]">
              {t("frontendApps.domainDesc", "Set a subdomain for this app.")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            {!baseDomain ? (
              <p className="text-xs text-[#EF4444]">
                {t("frontendApps.noBaseDomain", "Base domain not configured. Ask the superadmin to set it in Integrations → Deploy.")}
              </p>
            ) : (
              <>
                <div>
                  <Label htmlFor="subdomain-input" className="text-[#94A3B8] text-xs">{t("frontendApps.subdomain", "Subdomain")}</Label>
                  <Input
                    id="subdomain-input"
                    value={domainSub}
                    onChange={(e) => {
                      const val = e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "");
                      setDomainSub(val);
                    }}
                    placeholder="meuapp"
                    className="mt-1 bg-white/[0.04] border-white/[0.08] text-[#F8FAFC] font-mono text-sm"
                  />
                </div>
                <div className="p-3 rounded-lg bg-white/[0.04] border border-white/[0.06] text-center">
                  <span className="text-xs text-[#64748B]">{t("frontendApps.domainPreview", "Full domain")}</span>
                  <div className="text-sm font-mono font-bold text-[#22C55E] mt-0.5 break-all">
                    {domainSub || "..."}.{baseDomain}
                  </div>
                </div>
              </>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => { setDomainApp(null); setDomainSub(""); }}
              className="text-[#94A3B8] hover:text-[#F8FAFC]">
              {t("frontendApps.cancel", "Cancel")}
            </Button>
            <Button onClick={handleSetDomain} disabled={!domainSub.trim() || !baseDomain}
              className="bg-[#3B82F6] hover:bg-[#2563EB] text-white">
              {t("frontendApps.saveDomain", "Save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
