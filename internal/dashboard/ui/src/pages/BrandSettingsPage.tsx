import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { Icon } from "@/components/ui/icon";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { PageHeader, ProviderCard, SettingRow } from "@/components/patterns";
import { useBrandConfig, useUpdateBrandConfig } from "../lib/api";

const TABS = [
  { value: "branding", icon: "palette", labelKey: "settings.tabBranding" },
  { value: "database", icon: "database", labelKey: "settings.tabDatabase" },
  { value: "auth", icon: "public", labelKey: "settings.tabAuth" },
  { value: "storage", icon: "hard_drive", labelKey: "settings.tabStorage" },
  { value: "license", icon: "workspace_premium", labelKey: "settings.tabLicense" },
] as const;

// Disabled roadmap control: handoff shows it, backend not built yet (D-DT08 pattern).
function SoonRow({ label, description }: { label: string; description: string }) {
  const { t } = useTranslation();
  return (
    <div className="opacity-60" title={t("apps.soon")}>
      <SettingRow
        label={
          <span className="flex items-center gap-2">
            {label}
            <span
              className="rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider"
              style={{ background: "var(--accent-tint)", color: "var(--accent)" }}
            >
              {t("apps.soon")}
            </span>
          </span>
        }
        description={description}
        control={<Switch checked={false} disabled />}
      />
    </div>
  );
}

export default function BrandSettingsPage() {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") || "branding";

  const setTab = (value: string) => {
    setSearchParams({ tab: value }, { replace: true });
  };

  return (
    <>
      <PageHeader title={t("settings.pageTitle")} subtitle={t("settings.pageSubtitle")} />

      <Tabs value={tab} onValueChange={setTab} className="w-full">
        <TabsList className="mb-6 h-auto w-full justify-start gap-1 overflow-x-auto rounded-[10px] border border-[var(--border)] bg-[var(--surface)] p-1.5">
          {TABS.map((tb) => (
            <TabsTrigger
              key={tb.value}
              value={tb.value}
              className="gap-1.5 rounded-[8px] text-[13px] text-[var(--text-secondary)] data-[state=active]:bg-[var(--hover-surface)] data-[state=active]:text-[var(--text-primary)] data-[state=active]:shadow-none"
            >
              <Icon name={tb.icon} size={14} /> {t(tb.labelKey)}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="branding" className="mt-0">
          <BrandingTab />
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
        <TabsContent value="license" className="mt-0">
          <LicenseTab />
        </TabsContent>
      </Tabs>
    </>
  );
}

/**
 * Branding: só nome da empresa. O antigo seletor de brand-theme
 * (azure/emerald/ruby) foi removido — DRD-02/T0.4 trocou runtime brand-theming
 * por dark/light via classe (toggle no rodapé da sidebar). O PUT ainda reenvia
 * `theme` inalterado porque o contrato do campo no backend não foi confirmado
 * para drop (T0.5).
 */
function BrandingTab() {
  const { t } = useTranslation();
  const { data } = useBrandConfig();
  const update = useUpdateBrandConfig();
  const [companyName, setCompanyName] = useState("");

  useEffect(() => {
    if (data) setCompanyName(data.company_name);
  }, [data]);

  const handleSave = () => {
    if (!data) return;
    update.mutate(
      // logo_url:"" preserva o comportamento legado (esta tela não edita logo).
      { theme: data.theme, company_name: companyName, logo_url: "" },
      {
        onSuccess: () => toast.success(t("settings.savedSuccess")),
        onError: (err) => toast.error(err.message),
      }
    );
  };

  return (
    <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <Label className="text-[13px] font-semibold text-[var(--text-secondary)]">
        {t("brand.companyName")}
      </Label>
      <Input
        value={companyName}
        onChange={(e) => setCompanyName(e.target.value)}
        placeholder="Zeep Tecnologia"
        className="mt-2 max-w-md"
      />
      <div className="mt-5">
        <Button onClick={handleSave} disabled={!data || update.isPending} className="gap-2">
          <Icon name="save" size={16} />
          {update.isPending ? t("brand.saving") : t("brand.save")}
        </Button>
      </div>
    </div>
  );
}

function SoftDeleteCard() {
  const { t } = useTranslation();
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [maxCsvRows, setMaxCsvRows] = useState(10000);
  const [statementTimeoutMs, setStatementTimeoutMs] = useState(30000);
  const [requireRlsDefault, setRequireRlsDefault] = useState(false);

  useEffect(() => {
    fetch("/dashboard/api/config/system", { credentials: "include" })
      .then((r) => r.json())
      .then((d) => {
        setEnabled(d.soft_delete_enabled);
        setMaxCsvRows(d.max_csv_export_rows ?? 10000);
        setStatementTimeoutMs(d.statement_timeout_ms ?? 30000);
        setRequireRlsDefault(d.require_rls_default ?? false);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const res = await fetch("/dashboard/api/config/system", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          soft_delete_enabled: enabled,
          max_csv_export_rows: maxCsvRows,
          statement_timeout_ms: statementTimeoutMs,
          require_rls_default: requireRlsDefault,
        }),
      });
      if (!res.ok) throw new Error(t("system.error"));
      toast.success(t("system.saved"));
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[var(--text-secondary)]">{t("app.loading")}</p>;

  return (
    <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <SettingRow
        label={t("system.softDelete")}
        description={t("system.softDeleteDesc")}
        control={<Switch checked={enabled} onCheckedChange={setEnabled} />}
      />
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <SettingRow
          label={t("settings.requireRlsDefault")}
          description={t("settings.requireRlsDefaultHint")}
          control={<Switch checked={requireRlsDefault} onCheckedChange={setRequireRlsDefault} />}
        />
      </div>
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <SoonRow label={t("settings.requireApproval")} description={t("settings.requireApprovalDesc")} />
      </div>
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <Label className="text-[13px] font-semibold text-[var(--text-secondary)]">
          {t("settings.maxCsvExportRows")}
        </Label>
        <Input
          type="number"
          min={1}
          value={maxCsvRows}
          onChange={(e) => setMaxCsvRows(Math.max(1, Number(e.target.value) || 1))}
          className="mt-2 max-w-[200px]"
        />
        <p className="mt-1 text-[11px] text-[var(--text-tertiary)]">{t("settings.maxCsvExportRowsHint")}</p>
      </div>
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <Label className="text-[13px] font-semibold text-[var(--text-secondary)]">
          {t("settings.statementTimeoutMs")}
        </Label>
        <Input
          type="number"
          min={0}
          value={statementTimeoutMs}
          onChange={(e) => setStatementTimeoutMs(Math.max(0, Number(e.target.value) || 0))}
          className="mt-2 max-w-[200px]"
        />
        <p className="mt-1 text-[11px] text-[var(--text-tertiary)]">{t("settings.statementTimeoutMsHint")}</p>
      </div>
      <div className="mt-4 border-t border-[var(--border)] pt-4">
        <Button onClick={handleSave} disabled={saving} className="gap-2">
          <Icon name="save" size={16} />
          {saving ? t("system.saving") : t("system.save")}
        </Button>
      </div>
    </div>
  );
}

function GoogleAuthProviderCard() {
  const { t } = useTranslation();
  const [configSet, setConfigSet] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
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
        setConfigSet(Boolean(d.config_set));
        setEnabled(d.enabled);
        setClientId(d.config?.client_id || "");
        setClientSecret("");
        setRedirectUrl(d.config?.redirect_url || "");
        setAllowedDomains((d.config?.allowed_domains || []).join(", "));
      })
      .catch(() => toast.error(t("settings.loadConfigError")))
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const configBody: Record<string, unknown> = {};
      if (clientId) configBody.client_id = clientId;
      if (clientSecret) configBody.client_secret = clientSecret;
      if (redirectUrl) configBody.redirect_url = redirectUrl;
      configBody.allowed_domains = allowedDomains
        ? allowedDomains.split(",").map((d) => d.trim()).filter(Boolean)
        : [];

      const res = await fetch("/dashboard/api/config/auth/providers/google", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          enabled,
          config: Object.keys(configBody).length > 0 ? configBody : undefined,
        }),
      });
      if (!res.ok) {
        const err = await res.json();
        throw new Error(err.error || t("settings.error"));
      }
      const result = await res.json();
      setConfigSet(Boolean(result.config_set));
      setClientSecret("");
      toast.success(t("settings.savedSuccess"));
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[var(--text-secondary)]">{t("app.loading")}</p>;

  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
        <SoonRow label={t("settings.require2fa")} description={t("settings.require2faDesc")} />
      </div>
      <ProviderCard
        name="Google"
        icon="public"
        description={t("settings.googleDesc")}
        status={
          enabled
            ? { label: t("settings.active"), tone: "success" }
            : { label: t("settings.inactive"), tone: "neutral" }
        }
        defaultOpen
      >
      <div className="flex flex-col gap-4">
        <SettingRow
          label={t("settings.active")}
          control={<Switch checked={enabled} onCheckedChange={setEnabled} />}
        />

        {enabled && (
          <div className="flex max-w-lg flex-col gap-4">
            <div>
              <Label className="mb-1.5 block text-[12px] font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                {t("settings.clientId")}
              </Label>
              <Input value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="Google OAuth Client ID" />
            </div>
            <div>
              <Label className="mb-1.5 block text-[12px] font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                {t("settings.clientSecret")}
              </Label>
              <div className="relative">
                <Input
                  type={showSecret ? "text" : "password"}
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                  placeholder={
                    configSet
                      ? t("settings.googleClientSecretPlaceholder")
                      : t("settings.googleClientSecretPlaceholderNew")
                  }
                  className="pr-10 font-mono"
                />
                <button
                  type="button"
                  title={t("settings.clientSecret")}
                  onClick={() => setShowSecret((v) => !v)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 cursor-pointer text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                >
                  <Icon name={showSecret ? "visibility_off" : "visibility"} size={16} />
                </button>
              </div>
            </div>
            <div>
              <Label className="mb-1.5 block text-[12px] font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                {t("settings.redirectUrl")}
              </Label>
              <Input
                value={redirectUrl}
                onChange={(e) => setRedirectUrl(e.target.value)}
                placeholder="https://orbit.zeeplabs.com/dashboard/api/auth/google/callback"
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-[12px] font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                {t("settings.allowedDomains")}
              </Label>
              <Input value={allowedDomains} onChange={(e) => setAllowedDomains(e.target.value)} placeholder="zeeplabs.com, zeepfly.com" />
              <p className="mt-1 text-[11px] text-[var(--text-tertiary)]">{t("settings.domainsHint")}</p>
            </div>
          </div>
        )}

        <div>
          <Button onClick={handleSave} disabled={saving} className="gap-2">
            <Icon name="save" size={16} />
            {saving ? t("brand.saving") : t("brand.save")}
          </Button>
        </div>
      </div>
      </ProviderCard>
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
      toast.success(t("settings.savedSuccess"));
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[var(--text-secondary)]">{t("app.loading")}</p>;

  return (
    <ProviderCard
      name={t("settings.globalStorage")}
      icon="hard_drive"
      description={t("settings.globalStorageDesc")}
      status={
        enabled
          ? { label: t("settings.active"), tone: "success" }
          : { label: t("settings.inactive"), tone: "neutral" }
      }
      defaultOpen
    >
      <div className="flex flex-col gap-4">
        <SettingRow
          label={t("settings.globalStorage")}
          control={<Switch checked={enabled} onCheckedChange={setEnabled} />}
        />

        {enabled && (
          <div className="flex max-w-lg flex-col gap-4">
            <Input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="https://s3.amazonaws.com" />
            <div className="flex gap-3">
              <Input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="Region (us-east-1)" />
              <Input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder={t("settings.bucketName")} />
            </div>
            <Input value={accessKeyId} onChange={(e) => setAccessKeyId(e.target.value)} placeholder={t("settings.accessKeyId")} />
            <Input
              type="password"
              value={secretAccessKey}
              onChange={(e) => setSecretAccessKey(e.target.value)}
              placeholder={t("settings.secretAccessKey")}
              className="font-mono"
            />
          </div>
        )}

        <div>
          <Button onClick={handleSave} disabled={saving} className="gap-2">
            <Icon name="save" size={16} />
            {saving ? t("system.saving") : t("system.save")}
          </Button>
        </div>
      </div>
    </ProviderCard>
  );
}

function LicenseTab() {
  const { t } = useTranslation();
  return (
    <div className="flex items-start gap-3 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <div
        className="flex size-9 shrink-0 items-center justify-center rounded-[10px]"
        style={{ background: "var(--accent-tint)", color: "var(--accent)" }}
      >
        <Icon name="workspace_premium" size={18} />
      </div>
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[14px] font-semibold text-[var(--text-primary)]">
            {t("settings.licenseSoonTitle")}
          </span>
          <span
            className="rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider"
            style={{ background: "var(--accent-tint)", color: "var(--accent)" }}
          >
            {t("apps.soon")}
          </span>
        </div>
        <p className="mt-1 text-[13px] text-[var(--text-secondary)]">
          {t("settings.licenseSoonDesc")}
        </p>
      </div>
    </div>
  );
}
