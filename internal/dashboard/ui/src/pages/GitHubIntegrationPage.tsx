import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import { Save, Eye, EyeOff, Loader2, Link2, Trash2, Plus, Pencil, Rocket } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

const EASE = [0.32, 0.72, 0, 1] as const;

interface GitHubStatus {
  connected: boolean;
  configured: boolean;
  org_login: string;
}

interface GitHubTemplate {
  id: string;
  name: string;
  description: string;
  github_owner: string;
  github_repo: string;
  framework: string;
  active: boolean;
  created_by: string;
  created_at: string;
  render_service_type: string;
  build_command: string;
  publish_path: string;
  start_command: string;
}

export default function GitHubIntegrationPage() {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") || "config";

  const setTab = (value: string) => {
    setSearchParams({ tab: value }, { replace: true });
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: EASE }}
    >
      <div className="mb-8">
        <span
          className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          <Link2 size={12} strokeWidth={1.5} />
          {t("integrations.title")}
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">{t("github.title")}</h2>
        <p className="mt-1 text-sm text-[#94A3B8]">{t("github.subtitle")}</p>
      </div>

      <Tabs value={tab} onValueChange={setTab} className="w-full">
        <TabsList className="w-full justify-start gap-1 rounded-2xl border border-white/[0.08] bg-white/[0.03] p-1.5 mb-6 h-auto overflow-x-auto">
          <TabsTrigger value="config" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <Link2 size={14} /> {t("github.tabConfig")}
          </TabsTrigger>
          <TabsTrigger value="templates" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <Save size={14} /> {t("github.tabTemplates")}
          </TabsTrigger>
          <TabsTrigger value="deploy" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <Rocket size={14} /> {t("deploy.tabDeploy", "Deploy")}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="config" className="mt-0">
          <GitHubConfigTab />
        </TabsContent>

        <TabsContent value="templates" className="mt-0">
          <GitHubTemplatesTab />
        </TabsContent>

        <TabsContent value="deploy" className="mt-0">
          <DeployTab />
        </TabsContent>
      </Tabs>
    </motion.div>
  );
}

function GitHubConfigTab() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<GitHubStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [messageType, setMessageType] = useState<"success" | "error">("success");

  const [appId, setAppId] = useState("");
  const [appSlug, setAppSlug] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [showSecret, setShowSecret] = useState(false);
  const [showPrivateKey, setShowPrivateKey] = useState(false);

  const [disconnecting, setDisconnecting] = useState(false);
  const [showDisconnectDialog, setShowDisconnectDialog] = useState(false);

  const [installing, setInstalling] = useState(false);

  useEffect(() => {
    fetchStatus();
  }, []);

  const fetchStatus = async () => {
    try {
      const res = await fetch("/dashboard/api/github/status", { credentials: "include" });
      const data = await res.json();
      setStatus(data);
      if (data.configured) {
        fetchConfig();
      }
    } catch {
    } finally {
      setLoading(false);
    }
  };

  const fetchConfig = async () => {
    try {
      const res = await fetch("/dashboard/api/github/config", { credentials: "include" });
      const data = await res.json();
      setAppId(data.app_id || "");
      setAppSlug(data.app_slug || "");
      setClientId(data.client_id || "");
    } catch {
    }
  };

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const res = await fetch("/dashboard/api/github/config", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          app_id: appId,
          app_slug: appSlug,
          client_id: clientId,
          client_secret: clientSecret,
          private_key: privateKey,
          webhook_secret: webhookSecret,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        setMessage(data.error || t("common.errorSaving"));
        setMessageType("error");
        return;
      }
      setMessage(t("github.configSaved"));
      setMessageType("success");
      setClientSecret("");
      setPrivateKey("");
      setWebhookSecret("");
      await fetchStatus();
    } catch {
      setMessage(t("common.errorSaving"));
      setMessageType("error");
    } finally {
      setSaving(false);
    }
  };

  const handleInstall = async () => {
    setInstalling(true);
    try {
      const res = await fetch("/dashboard/api/github/install/start", { credentials: "include" });
      const data = await res.json();
      if (data.install_url) {
        window.location.href = data.install_url;
      }
    } catch {
      setInstalling(false);
    }
  };

  const handleDisconnect = async () => {
    setDisconnecting(true);
    try {
      const res = await fetch("/dashboard/api/github/config", {
        method: "DELETE",
        credentials: "include",
      });
      if (res.ok) {
        setStatus({ connected: false, configured: false, org_login: "" });
        setMessage(t("github.disconnected"));
        setMessageType("success");
      }
    } catch {
      setMessage(t("common.errorSaving"));
      setMessageType("error");
    } finally {
      setDisconnecting(false);
      setShowDisconnectDialog(false);
    }
  };

  useEffect(() => {
    const searchParams = new URLSearchParams(window.location.search);
    if (searchParams.get("installed") === "true") {
      fetchStatus();
      setMessage(t("github.installed"));
      setMessageType("success");
      const url = new URL(window.location.href);
      url.searchParams.delete("installed");
      window.history.replaceState({}, "", url.toString());
    }
  }, []);

  if (loading) {
    return <p className="text-[13px] text-[#94A3B8]">{t("app.loading")}</p>;
  }

  const inputClass =
    "h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full";

  return (
    <div className="flex flex-col gap-6">
      <div className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6">
        <div className="flex items-center justify-between mb-6">
          <div>
            <span className="text-[15px] font-bold text-[#F8FAFC]">GitHub App</span>
            <p className="text-[12px] text-[#94A3B8] mt-0.5">{t("github.configDesc")}</p>
          </div>
          {status?.configured && (
            <div className="flex items-center gap-3">
              <Badge
                variant="outline"
                className={`gap-1.5 text-[11px] font-medium ${
                  status.connected
                    ? "border-emerald-500/20 bg-emerald-500/[0.10] text-emerald-300"
                    : "border-amber-500/20 bg-amber-500/[0.10] text-amber-300"
                }`}
              >
                <span
                  className={`size-1.5 rounded-full ${
                    status.connected ? "bg-emerald-400" : "bg-amber-400"
                  }`}
                />
                {status.connected ? t("github.connected") : t("github.notConnected")}
              </Badge>
              {status.connected && status.org_login && (
                <span className="text-[12px] text-[#64748B]">{status.org_login}</span>
              )}
            </div>
          )}
        </div>

        <div className="flex flex-col gap-4 max-w-lg">
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
              {t("github.appId")}
            </label>
            <Input
              value={appId}
              onChange={(e) => setAppId(e.target.value)}
              placeholder="123456"
              className={inputClass}
            />
          </div>

          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
              {t("github.appSlug")}
            </label>
            <Input
              value={appSlug}
              onChange={(e) => setAppSlug(e.target.value)}
              placeholder="my-orbit-app"
              className={inputClass}
            />
          </div>

          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
              {t("github.clientId")}
            </label>
            <Input
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
              placeholder="Iv23li..."
              className={inputClass}
            />
          </div>

          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
              {t("github.clientSecret")}
            </label>
            <div className="relative">
              <Input
                type={showSecret ? "text" : "password"}
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                placeholder={status?.configured ? "•••••••• (empty = keep current)" : t("github.clientSecret")}
                className={inputClass + " pr-10"}
              />
              <button
                type="button"
                title="Show/hide secret"
                onClick={() => setShowSecret(!showSecret)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-none border-none cursor-pointer"
              >
                {showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
          </div>

          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
              {t("github.privateKey")}
            </label>
            <div className="relative">
              <textarea
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
                placeholder={status?.configured ? "•••••••• (empty = keep current)" : t("github.privateKeyPlaceholder")}
                rows={4}
                className={
                  inputClass +
                  " h-auto resize-none font-mono text-[12px] py-2 pr-10"
                }
              />
              <button
                type="button"
                title="Show/hide private key"
                onClick={() => setShowPrivateKey(!showPrivateKey)}
                className="absolute right-3 top-3 text-[#64748B] hover:text-[#F8FAFC] bg-none border-none cursor-pointer"
              >
                {showPrivateKey ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
          </div>

          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
              {t("github.webhookSecret")}
            </label>
            <Input
              type="password"
              value={webhookSecret}
              onChange={(e) => setWebhookSecret(e.target.value)}
              placeholder={status?.configured ? "•••••••• (empty = keep current)" : t("github.webhookSecret")}
              className={inputClass}
            />
          </div>
        </div>

        {message && (
          <p className={`mt-4 text-[12px] ${messageType === "success" ? "text-green-400" : "text-red-400"}`}>
            {message}
          </p>
        )}

        <div className="flex items-center gap-3 mt-5">
          <Button
            onClick={handleSave}
            disabled={saving}
            className="gap-2 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
            style={{
              background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
            }}
          >
            {saving ? (
              <>
                <Loader2 size={14} className="animate-spin" /> {t("brand.saving")}
              </>
            ) : (
              <>
                <Save size={14} /> {t("brand.save")}
              </>
            )}
          </Button>

          {status?.configured && (
            <>
              <Button
                onClick={handleInstall}
                disabled={installing || status.connected}
                variant="outline"
                className="gap-2 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
              >
                {installing ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  <Link2 size={14} />
                )}
                {status.connected ? t("github.installed") : t("github.install")}
              </Button>

              <Button
                onClick={() => setShowDisconnectDialog(true)}
                disabled={disconnecting}
                variant="outline"
                className="gap-2 rounded-xl border-red-500/20 bg-red-500/[0.06] text-red-400 hover:bg-red-500/10 hover:text-red-400 font-medium"
              >
                <Trash2 size={14} />
                {t("github.disconnect")}
              </Button>
            </>
          )}
        </div>
      </div>

      <Dialog open={showDisconnectDialog} onOpenChange={setShowDisconnectDialog}>
        <DialogContent
          className="max-w-[420px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0 [&>button]:text-[#94A3B8] [&>button]:hover:text-[#F8FAFC] [&>button]:hover:bg-white/[0.08]"
          style={{ boxShadow: "0 0 40px rgba(var(--brand-primary-rgb), 0.10)" }}
        >
          <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
            <DialogHeader className="mb-0">
              <div className="w-11 h-11 rounded-xl bg-red-500/[0.12] border border-red-500/[0.20] flex items-center justify-center mb-[18px]">
                <Trash2 size={18} strokeWidth={1.5} className="text-red-500" />
              </div>
              <DialogTitle className="text-base font-bold text-[#F8FAFC] mb-2">
                {t("github.disconnectTitle")}
              </DialogTitle>
              <DialogDescription className="text-[13px] text-[#94A3B8] leading-relaxed mb-6">
                {t("github.disconnectDesc")}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter className="flex flex-row gap-2.5 sm:flex-row sm:justify-start sm:space-x-0">
              <Button
                variant="outline"
                onClick={() => setShowDisconnectDialog(false)}
                className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
              >
                {t("github.disconnectCancel")}
              </Button>
              <Button
                onClick={handleDisconnect}
                disabled={disconnecting}
                className="flex-1 rounded-xl bg-red-500 hover:bg-red-600 text-white font-semibold border-0 disabled:bg-red-500/40"
              >
                {disconnecting ? t("github.disconnecting") : t("github.disconnectConfirm")}
              </Button>
            </DialogFooter>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function GitHubTemplatesTab() {
  const { t } = useTranslation();
  const [templates, setTemplates] = useState<GitHubTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [editingTemplate, setEditingTemplate] = useState<GitHubTemplate | null>(null);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [githubOwner, setGithubOwner] = useState("");
  const [githubRepo, setGithubRepo] = useState("");
  const [framework, setFramework] = useState("");
  const [renderServiceType, setRenderServiceType] = useState("");
  const [buildCommand, setBuildCommand] = useState("");
  const [publishPath, setPublishPath] = useState("");
  const [startCommand, setStartCommand] = useState("");

  useEffect(() => {
    fetchTemplates();
  }, []);

  const fetchTemplates = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch("/dashboard/api/github/templates", { credentials: "include" });
      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || t("common.connectionError"));
      }
      const data = await res.json();
      setTemplates(data);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };

  const openCreateModal = () => {
    setEditingTemplate(null);
    setName("");
    setDescription("");
    setGithubOwner("");
    setGithubRepo("");
    setFramework("");
    setRenderServiceType("");
    setBuildCommand("");
    setPublishPath("");
    setStartCommand("");
    setFormError(null);
    setShowModal(true);
  };

  const openEditModal = (tpl: GitHubTemplate) => {
    setEditingTemplate(tpl);
    setName(tpl.name);
    setDescription(tpl.description);
    setGithubOwner(tpl.github_owner);
    setGithubRepo(tpl.github_repo);
    setFramework(tpl.framework);
    setRenderServiceType(tpl.render_service_type || "");
    setBuildCommand(tpl.build_command || "");
    setPublishPath(tpl.publish_path || "");
    setStartCommand(tpl.start_command || "");
    setFormError(null);
    setShowModal(true);
  };

  const handleSubmit = async () => {
    setSaving(true);
    setFormError(null);
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
      };

      const url = editingTemplate
        ? `/dashboard/api/github/templates/${editingTemplate.id}`
        : "/dashboard/api/github/templates";
      const method = editingTemplate ? "PUT" : "POST";

      const res = await fetch(url, {
        method,
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });

      if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || t("common.errorSaving"));
      }

      setShowModal(false);
      await fetchTemplates();
    } catch (err) {
      setFormError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const handleToggleActive = async (tpl: GitHubTemplate) => {
    try {
      const res = await fetch(`/dashboard/api/github/templates/${tpl.id}/active`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ active: !tpl.active }),
      });
      if (res.ok) {
        setTemplates((prev) =>
          prev.map((t) => (t.id === tpl.id ? { ...t, active: !t.active } : t))
        );
      }
    } catch {}
  };

  const handleDelete = async (tpl: GitHubTemplate) => {
    try {
      const res = await fetch(`/dashboard/api/github/templates/${tpl.id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (res.ok) {
        setTemplates((prev) => prev.filter((t) => t.id !== tpl.id));
      }
    } catch {}
  };

  const inputClass =
    "h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-[12px] text-[#94A3B8]">{t("github.templatesDesc")}</p>
        </div>
        <Button
          onClick={openCreateModal}
          className="gap-2 rounded-xl px-5 py-2.5 text-sm font-semibold text-white border-0 hover:opacity-90"
          style={{
            background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
          }}
        >
          {t("github.addTemplate")}
          <span className="flex size-6 items-center justify-center rounded-full bg-white/[0.12]">
            <Plus size={12} strokeWidth={2} />
          </span>
        </Button>
      </div>

      {loading && (
        <div className="flex items-center justify-center rounded-2xl border border-white/[0.06] bg-white/[0.02] px-6 py-12">
          <p className="text-[13px] text-[#94A3B8]">{t("app.loading")}</p>
        </div>
      )}

      {!loading && error && (
        <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">
          {error}
        </div>
      )}

      {!loading && !error && templates.length === 0 && (
        <div className="flex items-center justify-center rounded-2xl border border-white/[0.06] bg-white/[0.02] px-6 py-12">
          <div className="text-center">
            <Save size={32} strokeWidth={1} className="mx-auto mb-3 text-[#64748B]" />
            <p className="text-sm text-[#94A3B8]">{t("github.noTemplates")}</p>
          </div>
        </div>
      )}

      {!loading && !error && templates.length > 0 && (
        <div className="overflow-hidden rounded-2xl border border-white/[0.06] bg-white/[0.02]">
          <table className="w-full">
            <thead>
              <tr className="border-b border-white/[0.06]">
                <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                  Template
                </th>
                <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                  Framework
                </th>
                <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                  Repository
                </th>
                <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                  Status
                </th>
                <th className="px-4 py-3 text-right text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {templates.map((tpl, i) => (
                <motion.tr
                  key={tpl.id}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{ delay: i * 0.04 }}
                  className="group border-b border-white/[0.04] last:border-0 hover:bg-white/[0.03]"
                >
                  <td className="px-4 py-3.5">
                    <div className="flex flex-col">
                      <span className="text-[13px] font-medium text-[#F8FAFC]">{tpl.name}</span>
                      {tpl.description && (
                        <span className="text-[11px] text-[#64748B] mt-0.5 line-clamp-1">
                          {tpl.description}
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3.5">
                    {tpl.framework && (
                      <Badge
                        variant="outline"
                        className="text-[11px] border-white/[0.08] bg-white/[0.04] text-[#94A3B8] font-normal"
                      >
                        {tpl.framework}
                      </Badge>
                    )}
                  </td>
                  <td className="px-4 py-3.5">
                    <span className="text-[12px] text-[#64748B] font-mono">
                      {tpl.github_owner}/{tpl.github_repo}
                    </span>
                  </td>
                  <td className="px-4 py-3.5">
                    <div className="flex items-center gap-2">
                      <Switch
                        checked={tpl.active}
                        onCheckedChange={() => handleToggleActive(tpl)}
                      />
                      <span className="text-[11px] text-[#64748B]">
                        {tpl.active ? t("github.active") : t("github.inactive")}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-3.5 text-right">
                    <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                      <Button
                        variant="outline"
                        size="icon"
                        onClick={() => openEditModal(tpl)}
                        title={t("github.editTemplate")}
                        className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-[#F8FAFC]"
                      >
                        <Pencil size={12} strokeWidth={1.5} />
                      </Button>
                      <Button
                        variant="outline"
                        size="icon"
                        onClick={() => handleDelete(tpl)}
                        title={t("github.deleteTemplate")}
                        className="size-7 rounded-lg border-red-500/20 bg-red-500/[0.06] text-red-400 hover:bg-red-500/10 hover:text-red-400"
                      >
                        <Trash2 size={12} strokeWidth={1.5} />
                      </Button>
                    </div>
                  </td>
                </motion.tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog open={showModal} onOpenChange={setShowModal}>
        <DialogContent
          className="max-w-[480px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0 [&>button]:text-[#94A3B8] [&>button]:hover:text-[#F8FAFC] [&>button]:hover:bg-white/[0.08]"
          style={{ boxShadow: "0 0 40px rgba(var(--brand-primary-rgb), 0.10)" }}
        >
          <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
            <DialogHeader className="mb-0">
              <div className="w-11 h-11 rounded-xl bg-white/[0.08] border border-white/[0.10] flex items-center justify-center mb-[18px]">
                <Save size={18} strokeWidth={1.5} className="text-[#94A3B8]" />
              </div>
              <DialogTitle className="text-base font-bold text-[#F8FAFC] mb-2">
                {editingTemplate ? t("github.editTemplate") : t("github.addTemplate")}
              </DialogTitle>
              <DialogDescription className="text-[13px] text-[#94A3B8] leading-relaxed mb-6">
                {t("github.templateFormDesc")}
              </DialogDescription>
            </DialogHeader>

            <div className="flex flex-col gap-4">
              <div>
                <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                  {t("github.templateName")}
                </label>
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Vite React"
                  className={inputClass}
                />
              </div>

              <div>
                <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                  {t("github.templateDescription")}
                </label>
                <Input
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder={t("github.templateDescriptionPlaceholder")}
                  className={inputClass}
                />
              </div>

              <div className="flex gap-3">
                <div className="flex-1">
                  <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                    {t("github.templateOwner")}
                  </label>
                  <Input
                    value={githubOwner}
                    onChange={(e) => setGithubOwner(e.target.value)}
                    placeholder="zeeplabs"
                    className={inputClass}
                  />
                </div>
                <div className="flex-[2]">
                  <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                    {t("github.templateRepo")}
                  </label>
                  <Input
                    value={githubRepo}
                    onChange={(e) => setGithubRepo(e.target.value)}
                    placeholder="orbit-template-vite-react"
                    className={inputClass}
                  />
                </div>
              </div>

              <div>
                <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                  {t("github.templateFramework")}
                </label>
                <Input
                  value={framework}
                  onChange={(e) => setFramework(e.target.value)}
                  placeholder="Vite + React"
                  className={inputClass}
                />
              </div>

              <div className="border-t border-white/[0.06] pt-4 mt-2">
                <p className="mb-3 text-[11px] font-medium text-[#64748B] uppercase tracking-wider">{t("github.deployConfig", "Deploy Configuration (Render)")}</p>
                <div className="flex flex-col gap-3">
                  <div>
                    <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8]">Service Type</label>
                    <select
                      value={renderServiceType}
                      onChange={(e) => setRenderServiceType(e.target.value)}
                      className="w-full h-9 rounded-lg border border-white/[0.10] bg-white/[0.06] px-3 text-[13px] text-[#F8FAFC] outline-none"
                    >
                      <option value="">{t("github.deployNone", "No deploy config")}</option>
                      <option value="static_site">Static Site</option>
                      <option value="web_service">Web Service</option>
                    </select>
                  </div>

                  {renderServiceType && (
                    <>
                      <div>
                        <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8]">Build Command</label>
                        <Input value={buildCommand} onChange={(e) => setBuildCommand(e.target.value)}
                          placeholder="npm run build" className={inputClass} />
                      </div>
                      {renderServiceType === "static_site" && (
                        <div>
                          <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8]">Publish Path</label>
                          <Input value={publishPath} onChange={(e) => setPublishPath(e.target.value)}
                            placeholder="dist" className={inputClass} />
                        </div>
                      )}
                      {renderServiceType === "web_service" && (
                        <div>
                          <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8]">Start Command</label>
                          <Input value={startCommand} onChange={(e) => setStartCommand(e.target.value)}
                            placeholder="npm start" className={inputClass} />
                        </div>
                      )}
                    </>
                  )}
                </div>
              </div>
            </div>

            {formError && (
              <p className="mt-4 text-[12px] text-red-400">{formError}</p>
            )}

            <DialogFooter className="mt-6 flex flex-row gap-2.5 sm:flex-row sm:justify-start sm:space-x-0">
              <Button
                variant="outline"
                onClick={() => setShowModal(false)}
                disabled={saving}
                className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
              >
                {t("github.cancel")}
              </Button>
              <Button
                onClick={handleSubmit}
                disabled={saving}
                className="flex-1 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
                style={{
                  background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
                }}
              >
                {saving ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : editingTemplate ? (
                  t("github.updateTemplate")
                ) : (
                  t("github.createTemplate")
                )}
              </Button>
            </DialogFooter>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function DeployTab() {
  const { t } = useTranslation();
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ connected: boolean; provider: string } | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [messageType, setMessageType] = useState<"success" | "error">("success");

  useEffect(() => { fetchStatus(); }, []);

  const fetchStatus = async () => {
    try {
      const res = await fetch("/dashboard/api/deploy-provider/status", { credentials: "include" });
      if (res.ok) setStatus(await res.json());
    } catch {}
  };

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const res = await fetch("/dashboard/api/deploy-provider/config", {
        method: "POST", credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ api_key: apiKey }),
      });
      const data = await res.json();
      if (!res.ok) { setMessage(data.error || "Failed to connect"); setMessageType("error"); return; }
      setMessage("Provider connected successfully");
      setMessageType("success");
      setApiKey("");
      await fetchStatus();
    } catch { setMessage("Connection error"); setMessageType("error"); }
    finally { setSaving(false); }
  };

  const inputClass = "h-9 rounded-lg border border-white/[0.10] bg-white/[0.06] px-3 text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none focus:border-[rgba(var(--brand-primary-rgb),0.35)] transition-colors w-full";

  return (
    <div>
      <div className="rounded-2xl border border-white/[0.08] bg-white/[0.03] p-6">
        <div className="flex items-center gap-4 mb-6">
          <div className={`flex size-10 shrink-0 items-center justify-center rounded-xl border ${status?.connected ? "border-[#22C55E]/20 bg-[#22C55E]/10" : "border-white/[0.08] bg-white/[0.06]"}`}>
            <Rocket size={18} className={status?.connected ? "text-[#22C55E]" : "text-[#64748B]"} />
          </div>
          <div>
            <h3 className="text-sm font-bold text-[#F8FAFC]">{t("deploy.title", "Deploy Provider")}</h3>
            <p className="text-xs text-[#64748B]">
              {status?.connected
                ? t("deploy.connected", "Connected to Render — services will be created automatically on frontend app creation")
                : t("deploy.notConnected", "Not connected — connect your Render account to enable automatic deploys")}
            </p>
          </div>
          {status?.connected && (
            <span className="ml-auto inline-flex items-center gap-1 text-xs font-medium text-[#22C55E] bg-[#22C55E]/10 px-2 py-1 rounded-full">
              <span className="size-1.5 rounded-full bg-[#22C55E]" /> {t("deploy.active", "Active")}
            </span>
          )}
        </div>
        <div className="space-y-4">
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("deploy.apiKey", "Render API Key")}</label>
            <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
              placeholder="rnd_..." className={inputClass}
              onKeyDown={(e) => { if (e.key === "Enter") handleSave(); }} />
            <p className="mt-1 text-[11px] text-[#64748B]">{t("deploy.apiKeyHint", "Create an API key in your Render dashboard at Account Settings → API Keys")}</p>
          </div>
          {message && <p className={`text-[12px] ${messageType === "error" ? "text-[#EF4444]" : "text-[#22C55E]"}`}>{message}</p>}
          <Button onClick={handleSave} disabled={saving || !apiKey.trim()}
            className="gap-2 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
            style={{ background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))" }}>
            {saving ? <><Loader2 size={14} className="animate-spin" /> {t("brand.saving")}</>
              : <><Save size={14} /> {status?.connected ? t("deploy.update", "Update Key") : t("deploy.connect", "Connect Render")}</>}
          </Button>
        </div>
      </div>
    </div>
  );
}
