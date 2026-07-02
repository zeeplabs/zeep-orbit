import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import { Palette, Save, Eye, EyeOff, Loader2, Globe, Shield, HardDrive, Upload, Image as ImageIcon } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { THEMES, BrandTheme, applyTheme } from "../lib/themes";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";

const EASE = [0.32, 0.72, 0, 1] as const;

export default function BrandSettingsPage() {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") || "branding";

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
        <span className="mb-3 inline-block rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{ borderColor: "rgba(var(--brand-primary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)", color: "var(--brand-light)" }}>
          {t("nav.settings")}
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">{t("brand.title")}</h2>
        <p className="mt-1 text-sm text-[#94A3B8]">{t("brand.subtitle")}</p>
      </div>

      <Tabs value={tab} onValueChange={setTab} className="w-full">
        <TabsList className="w-full justify-start gap-1 rounded-2xl border border-white/[0.08] bg-white/[0.03] p-1.5 mb-6 h-auto overflow-x-auto">
          <TabsTrigger value="branding" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <Palette size={14} /> {t("settings.tabBranding")}
          </TabsTrigger>
          <TabsTrigger value="logo" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <ImageIcon size={14} /> {t("settings.tabLogo")}
          </TabsTrigger>
          <TabsTrigger value="database" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <Shield size={14} /> {t("settings.tabDatabase")}
          </TabsTrigger>
          <TabsTrigger value="auth" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <Globe size={14} /> {t("settings.tabAuth")}
          </TabsTrigger>
          <TabsTrigger value="storage" className="rounded-xl data-[state=active]:bg-white/[0.08] data-[state=active]:shadow-none text-[13px] gap-1.5">
            <HardDrive size={14} /> {t("settings.tabStorage")}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="branding" className="mt-0">
          <BrandingTab />
        </TabsContent>

        <TabsContent value="logo" className="mt-0">
          <BrandLogoCard />
        </TabsContent>

        <TabsContent value="database" className="mt-0">
          <SoftDeleteCard />
        </TabsContent>

        <TabsContent value="auth" className="mt-0">
          <GoogleAuthProviderCard />
        </TabsContent>

        <TabsContent value="storage" className="mt-0">
          <GlobalStorageCard />
        </TabsContent>
      </Tabs>
    </motion.div>
  );
}

function BrandingTab() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [companyName, setCompanyName] = useState("");
  const [selectedTheme, setSelectedTheme] = useState("azure");
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    fetch("/dashboard/api/config", { credentials: "include" })
      .then((res) => res.json())
      .then((data) => {
        setCompanyName(data.company_name);
        setSelectedTheme(data.theme);
      })
      .catch(() => {});
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const res = await fetch("/dashboard/api/config", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ theme: selectedTheme, company_name: companyName, logo_url: "", icon_url: "" }),
      });
      if (!res.ok) { const data = await res.json(); setMessage(data.error || t("settings.error")); return; }
      applyTheme(THEMES[selectedTheme] || THEMES.azure);
      qc.invalidateQueries({ queryKey: ["brand-config"] });
      setMessage(t("settings.saved"));
    } catch { setMessage(t("settings.error")); }
    finally { setSaving(false); }
  };

  const currentTheme = THEMES[selectedTheme] || THEMES.azure;

  return (
    <div className="flex flex-col gap-6">
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <Label className="text-[13px] font-semibold text-[#94A3B8]">{t("brand.companyName")}</Label>
        <Input value={companyName} onChange={(e) => setCompanyName(e.target.value)} placeholder="Zeep Tecnologia"
          className="bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus h-10" />
      </div>

      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <div className="flex items-center gap-2"><Palette size={16} className="text-[#94A3B8]" /><Label className="text-[13px] font-semibold text-[#94A3B8]">{t("brand.theme")}</Label></div>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
          {Object.entries(THEMES).map(([key, theme]) => (
            <ThemeCard key={key} themeKey={key} theme={theme} selected={selectedTheme === key} onClick={() => setSelectedTheme(key)} />
          ))}
        </div>
      </div>

      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <Label className="text-[13px] font-semibold text-[#94A3B8]">{t("settings.preview")}</Label>
        <div className="rounded-xl border p-4 flex items-center gap-3" style={{ borderColor: "rgba(var(--brand-primary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-primary-rgb), 0.06)" }}>
          <div className="size-9 rounded-lg flex items-center justify-center text-white text-sm font-bold"
            style={{ background: `linear-gradient(135deg, ${currentTheme.primary}, ${currentTheme.secondary})` }}>Z</div>
          <div>
            <p className="text-sm font-bold" style={{ color: currentTheme.primary }}>{companyName || "Zeep Tecnologia"}</p>
            <p className="text-[11px] text-[#94A3B8]">{t("brand.save")} <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-semibold text-white"
              style={{ background: `linear-gradient(135deg, ${currentTheme.primary}, ${currentTheme.secondary})` }}>{t("settings.example")}</span></p>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-3">
        <Button onClick={handleSave} disabled={saving}
          className="gap-2 rounded-xl px-6 py-2.5 text-sm font-bold text-white border-0"
          style={{ background: saving ? "rgba(var(--brand-primary-rgb), 0.5)" : "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))" }}>
          <Save size={14} />{saving ? t("brand.saving") : t("brand.save")}
        </Button>
        {message && <span className={cn("text-sm", message === t("settings.saved") ? "text-green-400" : "text-red-400")}>{message}</span>}
      </div>
    </div>
  );
}

function ThemeCard({
  themeKey,
  theme,
  selected,
  onClick,
}: {
  themeKey: string;
  theme: BrandTheme;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex flex-col gap-2 p-3 rounded-xl border transition-all duration-200 cursor-pointer text-left",
        selected
          ? "border-white/[0.20] bg-white/[0.06]"
          : "border-white/[0.08] bg-white/[0.03] hover:bg-white/[0.05]",
      )}
    >
      {/* Color swatches */}
      <div className="flex gap-1">
        <div
          className="h-6 flex-1 rounded-md"
          style={{ backgroundColor: theme.primary }}
        />
        <div
          className="h-6 flex-1 rounded-md"
          style={{ backgroundColor: theme.secondary }}
        />
        <div
          className="h-6 flex-1 rounded-md"
          style={{ backgroundColor: theme.tertiary }}
        />
      </div>
      <div className="flex items-center justify-between">
        <span className="text-[12px] font-semibold text-[#F8FAFC]">
          {theme.name}
        </span>
        {selected && (
          <span
            className="size-2 rounded-full"
            style={{ backgroundColor: theme.primary }}
          />
        )}
      </div>
    </button>
  );
}

function GoogleAuthProviderCard() {
  const { t } = useTranslation();
  const [config, setConfig] = useState<{
    enabled: boolean;
    config: { client_id?: string; client_secret?: string; redirect_url?: string; allowed_domains?: string[] };
    config_set: boolean;
  } | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [messageType, setMessageType] = useState<"success" | "error">("success");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [redirectUrl, setRedirectUrl] = useState("");
  const [allowedDomains, setAllowedDomains] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [showSecret, setShowSecret] = useState(false);

  useEffect(() => {
    fetch("/dashboard/api/config/auth/providers/google?reveal=true", { credentials: "include" })
      .then((r) => r.json())
      .then((d) => {
        setConfig(d);
        setEnabled(d.enabled);
        setClientId(d.config?.client_id || "");
        setClientSecret("");
        setRedirectUrl(d.config?.redirect_url || "");
        setAllowedDomains((d.config?.allowed_domains || []).join(", "));
      })
      .catch(() => setMessage(t("settings.loadConfigError")))
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const configBody: Record<string, unknown> = {};
      if (clientId) configBody.client_id = clientId;
      if (clientSecret) configBody.client_secret = clientSecret;
      if (redirectUrl) configBody.redirect_url = redirectUrl;
      if (allowedDomains) configBody.allowed_domains = allowedDomains.split(",").map((d) => d.trim()).filter(Boolean);

      const res = await fetch("/dashboard/api/config/auth/providers/google", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled, config: Object.keys(configBody).length > 0 ? configBody : undefined }),
      });
      if (!res.ok) { const err = await res.json(); throw new Error(err.error || t("settings.error")); }
      const result = await res.json();
      setConfig(result as typeof config);
      setClientSecret("");
      setMessage(t("settings.savedSuccess"));
      setMessageType("success");
    } catch (err) {
      setMessage((err as Error).message);
      setMessageType("error");
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[#94A3B8]">{t("app.loading")}</p>;

  const inputClass = "h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full";

  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 mt-4">
      <div className="flex items-center justify-between mb-6">
        <div>
          <span className="text-[15px] font-bold text-[#F8FAFC]">Google</span>
          <p className="text-[12px] text-[#94A3B8] mt-0.5">{t("settings.googleDesc")}</p>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[12px] text-[#94A3B8]">{t("settings.active")}</span>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </div>

      {enabled && (
        <div className="flex flex-col gap-4 max-w-lg">
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("settings.clientId")}</label>
            <Input value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="Google OAuth Client ID" className={inputClass} />
          </div>
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("settings.clientSecret")}</label>
            <div className="relative">
              <Input type={showSecret ? "text" : "password"} value={clientSecret} onChange={(e) => setClientSecret(e.target.value)}
                placeholder={config?.config_set ? t("settings.googleClientSecretPlaceholder") : t("settings.googleClientSecretPlaceholderNew")} className={inputClass + " pr-10"} />
              <button type="button" onClick={() => setShowSecret(!showSecret)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-none border-none cursor-pointer">
                {showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
          </div>
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("settings.redirectUrl")}</label>
            <Input value={redirectUrl} onChange={(e) => setRedirectUrl(e.target.value)}
              placeholder="https://orbit.zeeplabs.com/dashboard/api/auth/google/callback" className={inputClass} />
          </div>
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("settings.allowedDomains")}</label>
            <Input value={allowedDomains} onChange={(e) => setAllowedDomains(e.target.value)} placeholder="zeeplabs.com, zeepfly.com" className={inputClass} />
            <p className="mt-1 text-[11px] text-[#64748B]">{t("settings.domainsHint")}</p>
          </div>
        </div>
      )}

      {message && <p className={`mt-4 text-[12px] ${messageType === "success" ? "text-green-400" : "text-red-400"}`}>{message}</p>}

      <Button onClick={handleSave} disabled={saving}
        className="mt-5 gap-2 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
        style={{ background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))' }}>
        {saving ? <><Loader2 size={14} className="animate-spin" /> {t("brand.saving")}</> : <><Save size={14} /> {t("brand.save")}</>}
      </Button>
    </div>
  );
}

function SoftDeleteCard() {
  const { t } = useTranslation();
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    fetch("/dashboard/api/config/system", { credentials: "include" })
      .then((r) => r.json())
      .then((d) => { setEnabled(d.soft_delete_enabled); })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const res = await fetch("/dashboard/api/config/system", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ soft_delete_enabled: enabled }),
      });
      if (!res.ok) throw new Error(t("system.error"));
      setMessage(t("system.saved"));
    } catch {
      setMessage(t("system.error"));
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[#94A3B8]">{t("app.loading")}</p>;

  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 mt-4">
      <div className="flex items-center justify-between mb-6">
        <div>
          <span className="text-[15px] font-bold text-[#F8FAFC]">{t("system.softDelete")}</span>
          <p className="text-[12px] text-[#94A3B8] mt-0.5">{t("system.softDeleteDesc")}</p>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[12px] text-[#94A3B8]">{t("system.softDeleteEnabled")}</span>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </div>
      {message && <p className="text-[12px] text-green-400 mb-4">{message}</p>}
      <Button onClick={handleSave} disabled={saving}
        className="gap-2 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
        style={{ background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))' }}>
        {saving ? <><Loader2 size={14} className="animate-spin" /> {t("system.saving")}</> : <><Save size={14} /> {t("system.save")}</>}
      </Button>
    </div>
  );
}

function GlobalStorageCard() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [endpoint, setEndpoint] = useState("");
  const [region, setRegion] = useState("");
  const [bucket, setBucket] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretAccessKey, setSecretAccessKey] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [currentSoftDelete, setCurrentSoftDelete] = useState(false);

  useEffect(() => {
    fetch("/dashboard/api/config/system", { credentials: "include" })
      .then((r) => r.json())
      .then((d) => {
        setCurrentSoftDelete(d.soft_delete_enabled || false);
        if (d.storage_config) {
          setEnabled(true);
          setEndpoint(d.storage_config.endpoint || "");
          setRegion(d.storage_config.region || "");
          setBucket(d.storage_config.bucket || "");
          setAccessKeyId(d.storage_config.access_key_id || "");
          setSecretAccessKey("");
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const body: Record<string, unknown> = { soft_delete_enabled: currentSoftDelete };
      if (enabled && endpoint && region && bucket && accessKeyId) {
        body.storage_config = { endpoint, region, bucket, access_key_id: accessKeyId, secret_access_key: secretAccessKey };
      } else {
        body.storage_config = { bucket: "" };
      }
      const res = await fetch("/dashboard/api/config/system", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(t("settings.errorSaving"));
      setMessage(t("settings.savedSuccess"));
    } catch {
      setMessage(t("settings.errorSaving"));
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[#94A3B8]">{t("app.loading")}</p>;

  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 mt-4">
      <div className="flex items-center justify-between mb-6">
        <div>
          <span className="text-[15px] font-bold text-[#F8FAFC]">{t("settings.globalStorage")}</span>
          <p className="text-[12px] text-[#94A3B8] mt-0.5">{t("settings.globalStorageDesc")}</p>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      {enabled && (
        <div className="flex flex-col gap-4 max-w-lg">
          <Input value={endpoint} onChange={(e) => setEndpoint(e.target.value)}
            placeholder="https://s3.amazonaws.com" className="h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full" />
          <div className="flex gap-3">
            <Input value={region} onChange={(e) => setRegion(e.target.value)}
              placeholder="Region (us-east-1)" className="h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full" />
            <Input value={bucket} onChange={(e) => setBucket(e.target.value)}
              placeholder={t("settings.bucketName")} className="h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full" />
          </div>
          <Input value={accessKeyId} onChange={(e) => setAccessKeyId(e.target.value)}
            placeholder={t("settings.accessKeyId")} className="h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full" />
          <Input type="password" value={secretAccessKey} onChange={(e) => setSecretAccessKey(e.target.value)}
            placeholder={t("settings.secretAccessKey")} className="h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full" />
        </div>
      )}

      {message && <p className={`mt-4 text-[12px] ${message === t("settings.savedSuccess") ? "text-green-400" : "text-red-400"}`}>{message}</p>}

      <Button onClick={handleSave} disabled={saving}
        className="mt-5 gap-2 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
        style={{ background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))' }}>
        {saving ? <><Loader2 size={14} className="animate-spin" /> {t("system.saving")}</> : <><Save size={14} /> {t("system.save")}</>}
      </Button>
    </div>
  );
}

function BrandLogoCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [logoUrl, setLogoUrl] = useState("");
  const [iconUrl, setIconUrl] = useState("");
  const [uploading, setUploading] = useState<string | null>(null);

  useEffect(() => {
    fetch("/dashboard/api/brand/config", { credentials: "include" })
      .then((r) => r.json())
      .then((d) => {
        setLogoUrl(d.logo_url || "");
        setIconUrl(d.icon_url || "");
      })
      .catch(() => {});
  }, []);

  const handleUpload = async (type: "login-logo" | "icon") => {
    const fileInput = document.createElement("input");
    fileInput.type = "file";
    fileInput.accept = "image/*";
    fileInput.onchange = async () => {
      const file = fileInput.files?.[0];
      if (!file) return;
      setUploading(type);
      const form = new FormData();
      form.append("file", file);
      try {
        const res = await fetch(`/dashboard/api/brand/logo/${type}`, {
          method: "POST",
          credentials: "include",
          body: form,
        });
        if (!res.ok) throw new Error(t("settings.uploadFailed"));
        const data = await res.json();
        if (type === "login-logo") setLogoUrl(data.logo_url);
        else setIconUrl(data.icon_url);
        qc.invalidateQueries({ queryKey: ["brand-assets"] });
      } catch {
        toast.error(t("settings.uploadFailed"));
      } finally {
        setUploading(null);
      }
    };
    fileInput.click();
  };

  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 mt-4">
      <div className="flex flex-col gap-6 max-w-lg">
        <div>
          <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("settings.loginLogo")}</label>
          <p className="text-[11px] text-[#64748B] mb-3">{t("settings.loginLogoDesc")}</p>
          {logoUrl && (
            <div className="mb-3 p-3 rounded-lg bg-white/[0.04] border border-white/[0.06]">
              <img src={logoUrl} alt="Login logo" className="max-h-16 object-contain" />
            </div>
          )}
          <button onClick={() => handleUpload("login-logo")} disabled={uploading === "login-logo"}
            className="flex items-center gap-2 h-9 rounded-lg bg-white/[0.06] border border-white/[0.10] text-[12px] font-medium text-[#F8FAFC] px-4 hover:bg-white/[0.10] transition-colors disabled:opacity-40 cursor-pointer">
            {uploading === "login-logo" ? <Loader2 size={14} className="animate-spin" /> : <Upload size={14} />}
            {uploading === "login-logo" ? t("settings.uploading") : t("settings.uploadLogo")}
          </button>
        </div>

        <div>
          <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">{t("settings.appIcon")}</label>
          <p className="text-[11px] text-[#64748B] mb-3">{t("settings.appIconDesc")}</p>
          {iconUrl && (
            <div className="mb-3 p-3 rounded-lg bg-white/[0.04] border border-white/[0.06] inline-block">
              <img src={iconUrl} alt="App icon" className="size-10 object-contain rounded-lg" />
            </div>
          )}
          <button onClick={() => handleUpload("icon")} disabled={uploading === "icon"}
            className="flex items-center gap-2 h-9 rounded-lg bg-white/[0.06] border border-white/[0.10] text-[12px] font-medium text-[#F8FAFC] px-4 hover:bg-white/[0.10] transition-colors disabled:opacity-40 cursor-pointer">
            {uploading === "icon" ? <Loader2 size={14} className="animate-spin" /> : <Upload size={14} />}
            {uploading === "icon" ? t("settings.uploading") : t("settings.uploadIcon")}
          </button>
        </div>
      </div>
    </div>
  );
}
