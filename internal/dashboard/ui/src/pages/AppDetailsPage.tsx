import { useState, useEffect } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Trans, useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  useApp,
  useUpdateApp,
  useUpdateAppEnduserRoles,
  useCreateAppTable,
  useUpdateAppTable,
  useDeleteAppTable,
  useAppTokens,
  useCreateAppToken,
  useRevokeAppToken,
  useRegenerateAppSecret,
  useSystemConfig,
  AppToken,
  TableDef,
} from "../lib/api";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Icon } from "@/components/ui/icon";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
import TableCard from "@/components/TableCard";
import { AppMembersList } from "@/components/patterns/AppMembersList";
import {
  ProviderCard,
  SettingRow,
  StatusPill,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  type StatusTone,
} from "@/components/patterns";

const TABS = ["database", "auth", "storage", "api", "tokens", "members", "observability"] as const;

export default function AppDetailsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") || "database";
  const setTab = (value: string) => setSearchParams({ tab: value }, { replace: true });

  const { data: app, isLoading } = useApp(id!);

  if (isLoading) {
    return <p className="text-sm text-[var(--text-secondary)]">{t("app.loading")}</p>;
  }

  if (!app) {
    return <ErrorState title={t("appForm.notFound")} />;
  }

  const tabDefs: { v: (typeof TABS)[number]; label: string; badge?: boolean }[] = [
    { v: "database", label: t("appForm.tabDatabase") },
    { v: "auth", label: t("appForm.tabAuth") },
    { v: "storage", label: t("appForm.tabStorage") },
    { v: "api", label: t("appForm.tabApi") },
    { v: "tokens", label: t("appDetails.tabTokens") },
    { v: "members", label: t("appDetails.tabMembers"), badge: true },
    { v: "observability", label: t("appDetails.tabObservability"), badge: true },
  ];

  return (
    <div>
      <button
        type="button"
        onClick={() => navigate("/apps")}
        className="mb-5 flex cursor-pointer items-center gap-1.5 border-none bg-transparent text-[13px] font-semibold text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
      >
        <Icon name="arrow_back" size={17} />
        {t("appForm.back")}
      </button>

      <div className="mb-7">
        <span className="mb-3 inline-block rounded-full bg-[var(--primary-tint)] px-2.5 py-1 text-[11px] font-bold uppercase tracking-wider text-[var(--primary)]">
          {t("appDetails.backendBadge")}
        </span>
        <h1 className="text-[26px] font-bold leading-tight text-[var(--text-primary)]" style={{ fontFamily: "var(--font-display)" }}>
          {app.name}
        </h1>
        <p className="mt-1.5 text-sm text-[var(--text-secondary)]">{t("appDetails.subtitle")}</p>
      </div>

      <Tabs value={tab} onValueChange={setTab} className="w-full">
        <TabsList className="mb-7 inline-flex h-auto w-auto justify-start gap-0.5 rounded-[10px] bg-[var(--sunken)] p-[3px]">
          {tabDefs.map(({ v, label, badge }) => (
            <TabsTrigger
              key={v}
              value={v}
              className="gap-1.5 rounded-[8px] px-4 py-2 text-[13px] font-bold text-[var(--text-secondary)] data-[state=active]:bg-[var(--surface-raised)] data-[state=active]:text-[var(--text-primary)] data-[state=active]:shadow-[var(--shadow-sm)]"
            >
              {label}
              {badge && (
                <span className="rounded-full px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wide"
                  style={{ background: "var(--accent-tint)", color: "var(--accent)" }}>
                  {t("appDetails.badgeNew")}
                </span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="database" className="mt-0">
          <DatabaseTab app={app} />
        </TabsContent>
        <TabsContent value="auth" className="mt-0">
          <LoginTab app={app} />
        </TabsContent>
        <TabsContent value="storage" className="mt-0">
          <StorageTab app={app} />
        </TabsContent>
        <TabsContent value="api" className="mt-0">
          <ApiTab app={app} />
        </TabsContent>
        <TabsContent value="tokens" className="mt-0">
          <TokensTab app={app} />
        </TabsContent>
        <TabsContent value="members" className="mt-0">
          <AppMembersList appId={app.id} axis="backend" />
        </TabsContent>
        <TabsContent value="observability" className="mt-0">
          <EmptyState icon="monitoring" title={t("appDetails.observabilitySoon")} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function emptyDraftTable(defaultRls: string): TableDef {
  return { name: "", rls: defaultRls, columns: [] };
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3">
      <div className="h-6 w-1 rounded-full" style={{ background: "var(--primary)" }} />
      <p className="text-[15px] font-bold text-[var(--text-primary)]">{children}</p>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-4 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <h3 className="text-[13px] font-semibold uppercase tracking-wider text-[var(--text-secondary)]">{title}</h3>
      {children}
    </div>
  );
}

function DatabaseTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { t } = useTranslation();
  const [draftTable, setDraftTable] = useState<TableDef | null>(null);
  const [editingKey, setEditingKey] = useState<string | null>(null);

  const createTable = useCreateAppTable(app.id);
  const updateTable = useUpdateAppTable(app.id);
  const deleteTable = useDeleteAppTable(app.id);

  const { data: sysCfg } = useSystemConfig();
  const requireRls = Boolean(sysCfg?.require_rls_default) && app.auth_email_enabled;
  const defaultRls = requireRls ? "enabled" : "disabled";

  const addTable = () => {
    setDraftTable(emptyDraftTable(defaultRls));
    setEditingKey("draft");
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <p className="text-[15px] font-bold text-[var(--text-primary)]">{t("appForm.tables")}</p>
        <Button className="gap-1.5" onClick={addTable} disabled={editingKey !== null}>
          <Icon name="add" size={17} />
          {t("appForm.addTable")}
        </Button>
      </div>

      {app.tables.length === 0 && !draftTable && (
        <EmptyState icon="table_chart" title={t("appForm.noTables")} description={t("appForm.noTablesDesc")} />
      )}

      <div className="flex flex-col gap-3">
        {app.tables.map((tbl) => (
          <TableCard
            key={tbl.id}
            appId={app.id}
            table={tbl}
            otherTables={app.tables.filter((other) => other.id && other.id !== tbl.id)}
            authEmailEnabled={app.auth_email_enabled}
            locked={editingKey !== null && editingKey !== tbl.id}
            startInEdit={false}
            onEnterEdit={() => setEditingKey(tbl.id!)}
            onExitEdit={() => setEditingKey(null)}
            onCreate={(input) => createTable.mutateAsync(input)}
            onUpdate={(input) => updateTable.mutateAsync({ tableId: tbl.id!, ...input })}
            onDelete={() => deleteTable.mutateAsync(tbl.id!)}
            onSaved={() => setEditingKey(null)}
            onDiscardDraft={() => {}}
            onDeleted={() => setEditingKey(null)}
          />
        ))}
        {draftTable && (
          <TableCard
            key="draft"
            appId={app.id}
            table={draftTable}
            otherTables={app.tables.filter((other) => other.id)}
            authEmailEnabled={app.auth_email_enabled}
            draftRlsHint={requireRls ? t("tableCard.defaultRlsHint") : ""}
            locked={false}
            startInEdit
            onEnterEdit={() => setEditingKey("draft")}
            onExitEdit={() => setEditingKey(null)}
            onCreate={(input) => createTable.mutateAsync(input)}
            onUpdate={() => Promise.reject(new Error("draft table has no id"))}
            onDelete={() => Promise.resolve()}
            onSaved={() => {
              setDraftTable(null);
              setEditingKey(null);
            }}
            onDiscardDraft={() => {
              setDraftTable(null);
              setEditingKey(null);
            }}
            onDeleted={() => {}}
          />
        )}
      </div>
    </div>
  );
}

function LoginTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { t } = useTranslation();
  const updateApp = useUpdateApp();
  const [authEmail, setAuthEmail] = useState(app.auth_email_enabled);
  const [googleEnabled, setGoogleEnabled] = useState(false);
  const [googleClientId, setGoogleClientId] = useState("");
  const [googleClientSecret, setGoogleClientSecret] = useState("");
  const [googleRedirectUrl, setGoogleRedirectUrl] = useState("");
  const [googleAllowedDomains, setGoogleAllowedDomains] = useState("");
  const [showGoogleSecret, setShowGoogleSecret] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setAuthEmail(app.auth_email_enabled);
    const providers = (app as any).auth_providers;
    if (providers?.google?.enabled) {
      setGoogleEnabled(true);
      setGoogleClientId(providers.google.client_id || "");
      setGoogleRedirectUrl(providers.google.redirect_url || "");
      setGoogleAllowedDomains((providers.google.allowed_domains || []).join(", "));
    }
  }, [app]);

  async function save() {
    setError(null);
    setSaved(false);
    const payload: Record<string, unknown> = { id: app.id, name: app.name, auth_email_enabled: authEmail };
    if (googleEnabled) {
      const domains = googleAllowedDomains.split(",").map((d) => d.trim()).filter(Boolean);
      payload.auth_providers = {
        google: {
          enabled: true,
          client_id: googleClientId,
          client_secret: googleClientSecret,
          redirect_url: googleRedirectUrl || `/${app.name}/auth/google/callback`,
          ...(domains.length > 0 ? { allowed_domains: domains } : {}),
        },
      };
    }
    try {
      await updateApp.mutateAsync(payload as any);
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.errorSaving"));
    }
  }

  const activePill = (on: boolean): { label: string; tone: StatusTone } => ({
    label: on ? t("appDetails.providerActive") : t("appDetails.providerInactive"),
    tone: on ? "success" : "neutral",
  });

  return (
    <div className="flex flex-col gap-4">
      <ProviderCard
        name={t("appForm.email")}
        icon="mail"
        description={t("appForm.emailDesc")}
        status={activePill(authEmail)}
        defaultOpen
      >
        <SettingRow
          label={t("appForm.email")}
          description={t("appForm.emailDesc")}
          control={<Switch checked={authEmail} onCheckedChange={setAuthEmail} />}
        />
      </ProviderCard>

      <ProviderCard
        name={t("appForm.google")}
        icon="account_circle"
        description={t("appForm.googleDesc")}
        status={activePill(googleEnabled)}
        defaultOpen={googleEnabled}
      >
        <SettingRow
          label={t("appForm.google")}
          description={t("appForm.googleDesc")}
          control={<Switch checked={googleEnabled} onCheckedChange={setGoogleEnabled} />}
        />
        {googleEnabled && (
          <div className="flex flex-col gap-3 border-t border-[var(--border)] pt-3">
            <div className="flex flex-col gap-1.5">
              <Label>{t("appForm.googleClientId")}</Label>
              <Input
                value={googleClientId}
                onChange={(e) => setGoogleClientId(e.target.value)}
                placeholder={t("appDetails.googleClientIdPlaceholder")}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label>{t("appForm.googleClientSecret")}</Label>
              <div className="relative">
                <Input
                  type={showGoogleSecret ? "text" : "password"}
                  value={googleClientSecret}
                  onChange={(e) => setGoogleClientSecret(e.target.value)}
                  placeholder={t("appDetails.googleClientSecretPlaceholder")}
                  className="pr-10"
                />
                <button
                  type="button"
                  title={t("appDetails.showHideSecret")}
                  onClick={() => setShowGoogleSecret(!showGoogleSecret)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 cursor-pointer border-none bg-transparent text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                >
                  <Icon name={showGoogleSecret ? "visibility_off" : "visibility"} size={16} />
                </button>
              </div>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label>{t("appForm.googleRedirectUrl")}</Label>
              <Input
                value={googleRedirectUrl}
                onChange={(e) => setGoogleRedirectUrl(e.target.value)}
                placeholder={`https://seu-dominio.com/${app.name}/auth/google/callback`}
              />
              <p className="text-[11px] text-[var(--text-tertiary)]">{t("appForm.googleRedirectHint")}</p>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label>{t("appForm.googleDomains")}</Label>
              <Input
                value={googleAllowedDomains}
                onChange={(e) => setGoogleAllowedDomains(e.target.value)}
                placeholder="zeeplabs.com, zeepfy.com"
              />
              <p className="text-[11px] text-[var(--text-tertiary)]">{t("appForm.googleDomainsHint")}</p>
            </div>
          </div>
        )}
      </ProviderCard>

      <EnduserRolesSection app={app} />

      {error && <p className="text-xs text-[var(--danger)]">{error}</p>}
      <SaveBar onSave={save} saving={updateApp.isPending} saved={saved} />
    </div>
  );
}

// End-user business roles (ROLECFG-01..08). Gated by auth_email_enabled —
// same pattern as the fix ROWPOL-25 for the `_auth_users` FK dropdown — since
// the end-user "role" claim only exists when end-user auth is on.
function EnduserRolesSection({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { t } = useTranslation();
  const updateRoles = useUpdateAppEnduserRoles();
  const [newRole, setNewRole] = useState("");

  if (!app.auth_email_enabled) return null;

  const roles = app.enduser_roles_config;
  const identRe = /^[a-z][a-z0-9_]{0,62}$/;

  const addRole = () => {
    const trimmed = newRole.trim();
    if (!trimmed) return;
    if (!identRe.test(trimmed)) {
      toast.error(t("appDetails.enduserRolesFormatError"));
      return;
    }
    if (roles.includes(trimmed)) {
      toast.error(t("appDetails.enduserRolesDuplicateError"));
      return;
    }
    updateRoles.mutate(
      { id: app.id, roles: [...roles, trimmed] },
      { onSuccess: () => setNewRole("") },
    );
  };

  const removeRole = (role: string) => {
    updateRoles.mutate({ id: app.id, roles: roles.filter((r) => r !== role) });
  };

  return (
    <Panel title={t("appDetails.enduserRolesTitle")}>
      <p className="text-[11px] text-[var(--text-secondary)]">{t("appDetails.enduserRolesDesc")}</p>
      <div className="flex flex-wrap gap-2">
        {roles.map((role) => (
          <Badge key={role} variant="outline" className="gap-1.5 py-1">
            {role}
            <button
              type="button"
              onClick={() => removeRole(role)}
              disabled={updateRoles.isPending}
              title={t("appDetails.enduserRolesRemove")}
              className="flex cursor-pointer items-center justify-center border-none bg-transparent p-0 text-[var(--text-tertiary)] hover:text-[var(--danger)] disabled:cursor-not-allowed"
            >
              <Icon name="close" size={11} />
            </button>
          </Badge>
        ))}
      </div>
      <div className="flex items-center gap-2">
        <Input
          value={newRole}
          onChange={(e) => setNewRole(e.target.value)}
          placeholder={t("appDetails.enduserRolesAddPlaceholder")}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addRole();
            }
          }}
          className="h-9 max-w-[220px]"
        />
        <Button variant="outline" onClick={addRole} disabled={updateRoles.isPending || !newRole.trim()}>
          {t("appDetails.enduserRolesAdd")}
        </Button>
      </div>
    </Panel>
  );
}

function StorageTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { t } = useTranslation();
  const updateApp = useUpdateApp();
  const [globalStorage, setGlobalStorage] = useState(false);
  const [storageEnabled, setStorageEnabled] = useState(false);
  const [storageBucket, setStorageBucket] = useState("");
  const [storageRegion, setStorageRegion] = useState("");
  const [storageEndpoint, setStorageEndpoint] = useState("");
  const [storageAccessKey, setStorageAccessKey] = useState("");
  const [storageSecretKey, setStorageSecretKey] = useState("");
  const [showStorageSecret, setShowStorageSecret] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    fetch("/dashboard/api/config", { cache: "no-cache" })
      .then((r) => r.json())
      .then((d) => setGlobalStorage(d.storage_configured === true))
      .catch(() => {});
  }, []);

  useEffect(() => {
    const sc = (app as any).storage_config;
    if (sc?.bucket) {
      setStorageEnabled(true);
      setStorageBucket(sc.bucket || "");
      setStorageRegion(sc.region || "");
      setStorageEndpoint(sc.endpoint || "");
      setStorageAccessKey(sc.access_key_id || "");
    }
  }, [app]);

  async function save() {
    setError(null);
    setSaved(false);
    const payload: Record<string, unknown> = {
      id: app.id,
      name: app.name,
      auth_email_enabled: app.auth_email_enabled,
    };
    if (storageEnabled) {
      if (globalStorage) {
        payload.storage_config = { bucket: app.name };
      } else if (storageBucket && storageRegion && storageEndpoint && storageAccessKey && storageSecretKey) {
        payload.storage_config = {
          bucket: storageBucket,
          region: storageRegion,
          endpoint: storageEndpoint,
          access_key_id: storageAccessKey,
          secret_access_key: storageSecretKey,
        };
      }
    }
    try {
      await updateApp.mutateAsync(payload as any);
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.errorSaving"));
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <ProviderCard
        name={t("appForm.storageTitle")}
        icon="cloud"
        description={t("appDetails.storageEnableDesc")}
        status={{
          label: storageEnabled ? t("appDetails.providerActive") : t("appDetails.providerInactive"),
          tone: storageEnabled ? "success" : "neutral",
        }}
        defaultOpen
      >
        <SettingRow
          label={t("appDetails.storageEnableTitle")}
          description={t("appDetails.storageEnableDesc")}
          control={<Switch checked={storageEnabled} onCheckedChange={setStorageEnabled} />}
        />
        {storageEnabled && (
          <div className="flex flex-col gap-3 border-t border-[var(--border)] pt-3">
            <div className="flex flex-col gap-1.5">
              <Label>{t("appForm.storageBucket")}</Label>
              {globalStorage ? (
                <>
                  <p className="text-[11px] text-[var(--text-secondary)]">{t("appForm.storageGlobalHint")}</p>
                  <Input value={app.name} readOnly className="cursor-not-allowed opacity-60" />
                </>
              ) : (
                <Input value={storageBucket} onChange={(e) => setStorageBucket(e.target.value)} placeholder="meu-bucket" />
              )}
            </div>
            {!globalStorage && (
              <>
                <div className="flex flex-col gap-1.5">
                  <Label>{t("appForm.storageRegion")}</Label>
                  <Input value={storageRegion} onChange={(e) => setStorageRegion(e.target.value)} placeholder="us-east-1" />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label>{t("appForm.storageEndpoint")}</Label>
                  <Input value={storageEndpoint} onChange={(e) => setStorageEndpoint(e.target.value)} placeholder="https://nyc3.digitaloceanspaces.com" />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label>{t("appForm.storageAccessKey")}</Label>
                  <Input value={storageAccessKey} onChange={(e) => setStorageAccessKey(e.target.value)} placeholder="DO00XXXXXXXXXXXX" />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label>{t("appForm.storageSecretKey")}</Label>
                  <div className="relative">
                    <Input
                      type={showStorageSecret ? "text" : "password"}
                      value={storageSecretKey}
                      onChange={(e) => setStorageSecretKey(e.target.value)}
                      placeholder="Secret Key"
                      className="pr-10"
                    />
                    <button
                      type="button"
                      title={t("appDetails.showHideSecret")}
                      onClick={() => setShowStorageSecret(!showStorageSecret)}
                      className="absolute right-3 top-1/2 -translate-y-1/2 cursor-pointer border-none bg-transparent text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                    >
                      <Icon name={showStorageSecret ? "visibility_off" : "visibility"} size={16} />
                    </button>
                  </div>
                </div>
              </>
            )}
            <p className="text-[11px] text-[var(--text-secondary)]">
              <Trans
                i18nKey="appForm.storageHint"
                values={{ path: `/${app.name}/files/*` }}
                components={{ 1: <code className="text-[var(--primary)]" /> }}
              />
            </p>
          </div>
        )}
      </ProviderCard>

      {error && <p className="text-xs text-[var(--danger)]">{error}</p>}
      <SaveBar onSave={save} saving={updateApp.isPending} saved={saved} />
    </div>
  );
}

function ApiTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { t } = useTranslation();
  const updateApp = useUpdateApp();
  const [rateLimitEnabled, setRateLimitEnabled] = useState(false);
  const [rateLimitRPM, setRateLimitRPM] = useState(60);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    const rl = (app as any).rate_limit;
    if (rl?.enabled) {
      setRateLimitEnabled(true);
      setRateLimitRPM(rl.requests_per_minute || 60);
    }
  }, [app]);

  async function save() {
    setError(null);
    setSaved(false);
    try {
      await updateApp.mutateAsync({
        id: app.id,
        name: app.name,
        auth_email_enabled: app.auth_email_enabled,
        rate_limit: { enabled: rateLimitEnabled, requests_per_minute: rateLimitRPM },
      } as any);
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("common.errorSaving"));
    }
  }

  const baseUrl = `${window.location.origin}/${app.name}`;

  return (
    <div className="flex flex-col gap-4">
      <Panel title={t("appDetails.apiBaseTitle")}>
        <div className="flex items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
          <code className="flex-1 break-all font-mono text-sm text-[var(--primary)]">{baseUrl}</code>
          <Button
            variant="outline"
            size="icon"
            className="size-8 shrink-0"
            title={t("common.copyToClipboard")}
            onClick={() => { navigator.clipboard.writeText(baseUrl); toast.success(t("appDetails.copied")); }}
          >
            <Icon name="content_copy" size={15} />
          </Button>
        </div>
      </Panel>

      <Panel title={t("appDetails.apiSectionTitle")}>
        <SettingRow
          label={t("appDetails.rateLimitTitle")}
          description={t("appDetails.rateLimitDesc")}
          control={<Switch checked={rateLimitEnabled} onCheckedChange={setRateLimitEnabled} />}
        />
        {rateLimitEnabled && (
          <div className="flex flex-col gap-2 border-t border-[var(--border)] pt-3">
            <Label>{t("appDetails.requestsPerMinute")}</Label>
            <Input
              type="number"
              min={1}
              max={10000}
              value={rateLimitRPM}
              onChange={(e) => setRateLimitRPM(parseInt(e.target.value) || 60)}
              className="w-32"
            />
            <p className="text-[11px] text-[var(--text-secondary)]">{t("appDetails.rateLimitHint")}</p>
          </div>
        )}
      </Panel>

      {error && <p className="text-xs text-[var(--danger)]">{error}</p>}
      <SaveBar onSave={save} saving={updateApp.isPending} saved={saved} />
    </div>
  );
}

function TokensTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { t } = useTranslation();
  const { data: tokens, isLoading } = useAppTokens(app.id);
  const revokeToken = useRevokeAppToken(app.id);
  const regenerateSecret = useRegenerateAppSecret(app.id);
  const [showCreate, setShowCreate] = useState(false);
  const [showRegenerateConfirm, setShowRegenerateConfirm] = useState(false);
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);

  if (app.auth_email_enabled) {
    return (
      <div className="rounded-[10px] border border-[var(--warning)]/20 bg-[var(--warning-tint)] px-6 py-5 text-sm text-[var(--warning)]">
        {t("appDetails.tokensAuthWarning")}
      </div>
    );
  }

  const statusBadge = (tok: AppToken) => {
    if (tok.revoked_at) return <StatusPill label={t("appDetails.tokenRevoked")} tone="danger" dot={false} />;
    if (tok.expires_at && new Date(tok.expires_at) < new Date())
      return <StatusPill label={t("appDetails.tokenExpired")} tone="warning" dot={false} />;
    return <StatusPill label={t("appDetails.tokenActive")} tone="success" dot={false} />;
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <SectionTitle>{t("appDetails.tokensTitle")}</SectionTitle>
        <div className="flex items-center gap-2">
          <Button variant="outline" className="gap-1.5" onClick={() => setShowRegenerateConfirm(true)}>
            <Icon name="refresh" size={16} /> {t("appDetails.regenerateSecret")}
          </Button>
          <Button variant="outline" className="gap-1.5" onClick={() => setShowCreate(true)}>
            <Icon name="add" size={16} /> {t("appDetails.newToken")}
          </Button>
        </div>
      </div>

      <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-4">
        <div className="mb-2 flex items-center justify-between">
          <p className="text-[12px] font-semibold uppercase tracking-wider text-[var(--text-secondary)]">{t("appDetails.jwtSecretLabel")}</p>
          <button
            type="button"
            onClick={async () => {
              if (!revealedSecret) {
                try {
                  const res = await fetch(`/dashboard/api/apps/${app.id}/secret`, { credentials: "include" });
                  const data = await res.json();
                  setRevealedSecret(data.jwt_secret);
                } catch {}
              } else {
                setRevealedSecret(null);
              }
            }}
            className="flex cursor-pointer items-center gap-1 border-none bg-transparent text-[11px] text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
          >
            <Icon name={revealedSecret ? "visibility_off" : "visibility"} size={14} />
            {revealedSecret ? t("appDetails.hide") : t("appDetails.reveal")}
          </button>
        </div>
        {revealedSecret ? (
          <div className="flex items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
            <code className="flex-1 break-all font-mono text-sm text-[var(--primary)]">{revealedSecret}</code>
            <Button variant="outline" size="icon" className="size-8 shrink-0" title={t("common.copyToClipboard")}
              onClick={() => { navigator.clipboard.writeText(revealedSecret); toast.success(t("appDetails.copied")); }}>
              <Icon name="content_copy" size={15} />
            </Button>
          </div>
        ) : (
          <p className="text-xs text-[var(--text-tertiary)]">{t("appDetails.jwtSecretHint")}</p>
        )}
      </div>

      <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-4">
        <p className="mb-3 text-[12px] font-semibold uppercase tracking-wider text-[var(--text-secondary)]">{t("appDetails.tokensListTitle")}</p>
        {isLoading ? (
          <p className="text-sm text-[var(--text-secondary)]">{t("app.loading")}</p>
        ) : !tokens || tokens.length === 0 ? (
          <EmptyState icon="key" title={t("appDetails.noTokens")} description={t("appDetails.noTokensDesc")} />
        ) : (
          <div className="flex flex-col gap-2">
            {tokens.map((tok) => (
              <div key={tok.id} className="flex items-center justify-between rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
                <div className="flex min-w-0 flex-col gap-0.5">
                  <p className="truncate text-sm font-semibold text-[var(--text-primary)]">{tok.name}</p>
                  <p className="text-[11px] text-[var(--text-tertiary)]">
                    {tok.expires_at
                      ? t("appDetails.tokenExpires", { date: new Date(tok.expires_at).toLocaleDateString() })
                      : t("appDetails.tokenNeverExpires")}
                    {tok.last_used_at && ` ${t("appDetails.tokenLastUsed", { date: new Date(tok.last_used_at).toLocaleDateString() })}`}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {statusBadge(tok)}
                  {!tok.revoked_at && (
                    <Button variant="outline" size="icon" className="size-8 text-[var(--text-secondary)] hover:text-[var(--danger)]"
                      title={t("appDetails.revokeTokenAction")} onClick={() => revokeToken.mutate(tok.id)}>
                      <Icon name="close" size={15} />
                    </Button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {showCreate && (
        <CreateTokenModal
          appId={app.id}
          onClose={() => { setShowCreate(false); setCreatedToken(null); }}
          onCreated={(jwt) => { setCreatedToken(jwt); setShowCreate(false); }}
        />
      )}
      {createdToken && (
        <TokenRevealModal jwt={createdToken} onClose={() => setCreatedToken(null)} />
      )}

      <ConfirmDialog
        open={showRegenerateConfirm}
        title={t("appDetails.regenerateTitle")}
        message={t("appDetails.regenerateDesc")}
        confirmLabel={regenerateSecret.isPending ? t("appDetails.regenerating") : t("appDetails.regenerateConfirm")}
        cancelLabel={t("appForm.cancel")}
        destructive
        icon="refresh"
        loading={regenerateSecret.isPending}
        onConfirm={() => {
          regenerateSecret.mutate(undefined, {
            onSuccess: (data) => {
              setShowRegenerateConfirm(false);
              setRevealedSecret(data.jwt_secret);
            },
          });
        }}
        onCancel={() => setShowRegenerateConfirm(false)}
      />
    </div>
  );
}

function CreateTokenModal({ appId, onClose, onCreated }: { appId: string; onClose: () => void; onCreated: (jwt: string) => void }) {
  const { t } = useTranslation();
  const createToken = useCreateAppToken(appId);
  const [name, setName] = useState("");
  const [expiration, setExpiration] = useState("30d");
  const [error, setError] = useState<string | null>(null);

  async function handleCreate() {
    setError(null);
    try {
      const res = await createToken.mutateAsync({ name, expiration: expiration as any });
      onCreated(res.token);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("tableCard.saveError"));
    }
  }

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("appDetails.newToken")}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label>{t("appDetails.newTokenNameLabel")}</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("appDetails.newTokenNamePlaceholder")} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>{t("appDetails.expirationLabel")}</Label>
            <Select value={expiration} onValueChange={setExpiration}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="7d">{t("appDetails.expiration7d")}</SelectItem>
                <SelectItem value="30d">{t("appDetails.expiration30d")}</SelectItem>
                <SelectItem value="365d">{t("appDetails.expiration365d")}</SelectItem>
                <SelectItem value="never">{t("appDetails.tokenNeverExpires")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {error && <p className="text-xs text-[var(--danger)]">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("appForm.cancel")}</Button>
          <Button onClick={handleCreate} disabled={!name || createToken.isPending}>
            {createToken.isPending ? t("appDetails.creatingToken") : t("appDetails.createToken")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TokenRevealModal({ jwt, onClose }: { jwt: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <div className="mb-1 flex h-11 w-11 items-center justify-center rounded-[12px]"
            style={{ background: "var(--primary-tint)", color: "var(--primary)" }}>
            <Icon name="key" size={20} />
          </div>
          <DialogTitle>{t("appDetails.tokenCreatedTitle")}</DialogTitle>
          <DialogDescription className="text-[var(--warning)]">{t("appDetails.tokenCreatedWarning")}</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
          <code className="max-h-32 flex-1 overflow-y-auto break-all font-mono text-sm text-[var(--primary)]">{jwt}</code>
          <Button variant="outline" size="icon" className="size-9 shrink-0" title={t("common.copyToClipboard")}
            onClick={() => { navigator.clipboard.writeText(jwt); setCopied(true); }}>
            <Icon name="content_copy" size={16} />
          </Button>
        </div>
        {copied && <p className="text-[11px] text-[var(--success)]">{t("appDetails.copied")}</p>}
        <DialogFooter>
          <Button onClick={onClose}>{t("appDetails.close")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SaveBar({ onSave, saving, saved }: { onSave: () => void; saving: boolean; saved: boolean }) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-3">
      <Button onClick={onSave} disabled={saving}>
        {saving ? t("appDetails.saving") : t("appDetails.save")}
      </Button>
      {saved && !saving && <span className="text-xs text-[var(--text-secondary)]">{t("appDetails.saved")}</span>}
    </div>
  );
}
