import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import { Globe, Plus, RotateCcw, Trash2, Loader2, CheckCircle2, XCircle } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
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

const EASE = [0.32, 0.72, 0, 1] as const;

interface FrontendApp {
  id: string;
  name: string;
  slug: string;
  template_id: string;
  template_name: string;
  github_repo_url: string;
  status: string;
  error_message: string;
  created_by: string;
  created_at: string;
  archived_at: string | null;
}

interface Template {
  id: string;
  name: string;
  description: string;
  github_owner: string;
  github_repo: string;
  framework: string;
  active: boolean;
}

export default function FrontendAppsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [apps, setApps] = useState<FrontendApp[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [newTemplateId, setNewTemplateId] = useState("");

  const [deleting, setDeleting] = useState<string | null>(null);
  const [retrying, setRetrying] = useState<string | null>(null);

  const fetchApps = async () => {
    try {
      const res = await fetch("/dashboard/api/frontend-apps", { credentials: "include" });
      if (!res.ok) throw new Error("failed");
      const data = await res.json();
      setApps(data);
      setError(null);
    } catch {
      setError("Failed to load frontend apps");
    }
  };

  const fetchTemplates = async () => {
    try {
      const res = await fetch("/dashboard/api/github/templates", { credentials: "include" });
      if (!res.ok) throw new Error("failed");
      const data = await res.json();
      setTemplates((data || []).filter((t: Template) => t.active));
    } catch { /* non-critical */ }
  };

  useEffect(() => {
    Promise.all([fetchApps(), fetchTemplates()]).finally(() => setLoading(false));
  }, []);

  const handleCreate = async () => {
    setCreating(true);
    setCreateError(null);
    try {
      const res = await fetch("/dashboard/api/frontend-apps", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: newName, template_id: newTemplateId }),
      });
      const data = await res.json();
      if (!res.ok) {
        setCreateError(data.error || "Failed to create");
        return;
      }
      setShowCreate(false);
      setNewName("");
      setNewTemplateId("");
      qc.invalidateQueries({ queryKey: ["frontend-apps"] });
      await fetchApps();
      if (data.status === "failed") {
        setCreateError(`App created but repo generation failed: ${data.error_message}`);
      }
    } catch {
      setCreateError("Network error");
    } finally {
      setCreating(false);
    }
  };

  const handleRetry = async (id: string) => {
    setRetrying(id);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${id}/retry`, {
        method: "POST",
        credentials: "include",
      });
      if (res.ok) {
        await fetchApps();
      }
    } finally {
      setRetrying(null);
    }
  };

  const handleDelete = async (id: string) => {
    setDeleting(id);
    try {
      const res = await fetch(`/dashboard/api/frontend-apps/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (res.ok) {
        setApps((prev) => prev.filter((a) => a.id !== id));
      }
    } finally {
      setDeleting(null);
    }
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: EASE }}
    >
      <div className="mb-8">
        <span className="inline-flex items-center gap-1.5 text-xs font-medium text-[#94A3B8] uppercase tracking-wider mb-1">
          <Globe className="h-3.5 w-3.5" /> Frontend Apps
        </span>
        <h2 className="text-2xl font-semibold text-[#F8FAFC]">{t("frontendApps.title", "Frontend Apps")}</h2>
        <p className="text-sm text-[#94A3B8]">{t("frontendApps.subtitle", "Create and manage frontend applications deployed from GitHub templates")}</p>
      </div>

      <div className="flex items-center justify-between mb-6">
        <div />
        <Button
          onClick={() => {
            setShowCreate(true);
            setCreateError(null);
            setNewName("");
            setNewTemplateId("");
          }}
          disabled={templates.length === 0}
          className="bg-[#3B82F6] hover:bg-[#2563EB] text-white"
        >
          <Plus className="h-4 w-4 mr-2" />
          {t("frontendApps.create", "New Frontend App")}
        </Button>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="h-6 w-6 animate-spin text-[#64748B]" />
        </div>
      ) : error ? (
        <div className="text-center py-20 text-[#EF4444] text-sm">{error}</div>
      ) : apps.length === 0 ? (
        <div className="text-center py-20 text-[#64748B] text-sm">
          {t("frontendApps.noApps", "No frontend apps yet. Create your first one!")}
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-white/[0.02]">
          <table className="w-full">
            <thead>
              <tr className="border-b border-white/[0.06] text-xs text-[#64748B] uppercase tracking-wider">
                <th className="text-left px-4 py-3 font-medium">{t("frontendApps.name", "Name")}</th>
                <th className="text-left px-4 py-3 font-medium">{t("frontendApps.slug", "Slug")}</th>
                <th className="text-left px-4 py-3 font-medium">{t("frontendApps.template", "Template")}</th>
                <th className="text-left px-4 py-3 font-medium">{t("frontendApps.status", "Status")}</th>
                <th className="text-right px-4 py-3 font-medium">{t("frontendApps.actions", "Actions")}</th>
              </tr>
            </thead>
            <tbody>
              {apps.map((app) => (
                <tr key={app.id} className="border-b border-white/[0.04] hover:bg-white/[0.02] transition-colors">
                  <td className="px-4 py-3">
                    <div className="text-sm font-medium text-[#F8FAFC]">{app.name}</div>
                    <div className="text-xs text-[#64748B]">{app.created_by}</div>
                  </td>
                  <td className="px-4 py-3">
                    <code className="text-xs text-[#94A3B8] bg-white/[0.04] px-1.5 py-0.5 rounded">{app.slug}</code>
                  </td>
                  <td className="px-4 py-3">
                    <span className="text-sm text-[#94A3B8]">{app.template_name}</span>
                  </td>
                  <td className="px-4 py-3">
                    {app.status === "ready" ? (
                      <span className="inline-flex items-center gap-1 text-xs font-medium text-[#22C55E]">
                        <CheckCircle2 className="h-3.5 w-3.5" />
                        {t("frontendApps.statusReady", "Ready")}
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 text-xs font-medium text-[#EF4444]" title={app.error_message}>
                        <XCircle className="h-3.5 w-3.5" />
                        {t("frontendApps.statusFailed", "Failed")}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {app.status === "failed" && (
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => handleRetry(app.id)}
                          disabled={retrying === app.id}
                          className="h-8 text-xs text-[#F59E0B] hover:text-[#FBBF24] hover:bg-[#F59E0B]/10"
                        >
                          {retrying === app.id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <RotateCcw className="h-3.5 w-3.5" />
                          )}
                          <span className="ml-1">{t("frontendApps.retry", "Retry")}</span>
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => handleDelete(app.id)}
                        disabled={deleting === app.id}
                        className="h-8 text-xs text-[#EF4444] hover:text-[#F87171] hover:bg-[#EF4444]/10"
                      >
                        {deleting === app.id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog open={showCreate} onOpenChange={setShowCreate}>
        <DialogContent className="bg-[#0F0F17] border-white/[0.08] text-[#F8FAFC] max-w-md">
          <DialogHeader>
            <DialogTitle>{t("frontendApps.createTitle", "New Frontend App")}</DialogTitle>
            <DialogDescription className="text-[#94A3B8]">
              {t("frontendApps.createDesc", "Choose a name and a GitHub template to generate a new repository.")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <Label htmlFor="fa-name" className="text-[#94A3B8] text-xs">{t("frontendApps.formName", "App Name")}</Label>
              <Input
                id="fa-name"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder={t("frontendApps.formNamePlaceholder", "My Awesome App")}
                className="mt-1 bg-white/[0.04] border-white/[0.08] text-[#F8FAFC]"
              />
            </div>
            <div>
              <Label htmlFor="fa-template" className="text-[#94A3B8] text-xs">{t("frontendApps.formTemplate", "Template")}</Label>
              <Select value={newTemplateId} onValueChange={setNewTemplateId}>
                <SelectTrigger className="mt-1 bg-white/[0.04] border-white/[0.08] text-[#F8FAFC]">
                  <SelectValue placeholder={t("frontendApps.selectTemplate", "Select a template...")} />
                </SelectTrigger>
                <SelectContent className="bg-[#0F0F17] border-white/[0.08]">
                  {templates.map((tmpl) => (
                    <SelectItem key={tmpl.id} value={tmpl.id} className="text-[#F8FAFC]">
                      <span>{tmpl.name}</span>
                      <span className="ml-2 text-xs text-[#64748B]">{tmpl.framework}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {createError && (
              <p className="text-xs text-[#EF4444]">{createError}</p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setShowCreate(false)}
              className="text-[#94A3B8] hover:text-[#F8FAFC]"
            >
              {t("frontendApps.cancel", "Cancel")}
            </Button>
            <Button
              onClick={handleCreate}
              disabled={!newName.trim() || !newTemplateId || creating}
              className="bg-[#3B82F6] hover:bg-[#2563EB] text-white"
            >
              {creating && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("frontendApps.creating", "Create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </motion.div>
  );
}
