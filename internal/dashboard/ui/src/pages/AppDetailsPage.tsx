import { useState, useEffect } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Trans } from "react-i18next";
import { motion } from "framer-motion";
import { ArrowLeft, Plus, Eye, EyeOff, Copy, Table2, Key, RefreshCw, X, AlertTriangle } from "lucide-react";
import {
  useApp,
  useUpdateApp,
  useCreateAppTable,
  useUpdateAppTable,
  useDeleteAppTable,
  useAppTokens,
  useCreateAppToken,
  useRevokeAppToken,
  useRegenerateAppSecret,
  AppToken,
  TableDef,
} from "../lib/api";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import TableCard from "@/components/TableCard";

export default function AppDetailsPage() {
  const navigate = useNavigate();
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") || "database";
  const setTab = (value: string) => setSearchParams({ tab: value }, { replace: true });

  const { data: app, isLoading } = useApp(id!);

  if (isLoading) {
    return <p className="text-sm text-[#94A3B8]">Carregando...</p>;
  }

  if (!app) {
    return (
      <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">
        App não encontrado.
      </div>
    );
  }

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
    >
      <button
        type="button"
        onClick={() => navigate("/apps")}
        className="mb-6 flex items-center gap-2 text-[13px] text-[#94A3B8] hover:text-white transition-colors bg-transparent border-none cursor-pointer"
      >
        <ArrowLeft size={14} strokeWidth={1.5} />
        Voltar para Apps
      </button>

      <div className="mb-8">
        <span
          className="mb-3 inline-block rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          APP
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">"{app.name}"</h2>
        <p className="mt-1 text-sm text-[#94A3B8]">
          Cada bloco salva de forma independente — tabelas, uma por vez.
        </p>
      </div>

      <Tabs value={tab} onValueChange={setTab} className="w-full">
        <TabsList className="w-full justify-start gap-1 rounded-2xl border border-white/[0.08] bg-white/[0.03] p-1.5 mb-2 h-auto">
          <TabsTrigger
            value="database"
            className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
          >
            Banco de Dados
          </TabsTrigger>
          <TabsTrigger
            value="auth"
            className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
          >
            Provedores de Login
          </TabsTrigger>
          <TabsTrigger
            value="storage"
            className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
          >
            Storage (S3)
          </TabsTrigger>
          <TabsTrigger
            value="api"
            className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
          >
            API
          </TabsTrigger>
          <TabsTrigger
            value="tokens"
            className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
          >
            Tokens
          </TabsTrigger>
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
      </Tabs>
    </motion.div>
  );
}

function emptyDraftTable(): TableDef {
  return { name: "", rls: "disabled", columns: [] };
}

function DatabaseTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const [draftTable, setDraftTable] = useState<TableDef | null>(null);
  const [editingKey, setEditingKey] = useState<string | null>(null);

  const createTable = useCreateAppTable(app.id);
  const updateTable = useUpdateAppTable(app.id);
  const deleteTable = useDeleteAppTable(app.id);

  const addTable = () => {
    setDraftTable(emptyDraftTable());
    setEditingKey("draft");
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div
            className="h-6 w-1 rounded-full"
            style={{ background: "linear-gradient(to bottom, var(--brand-primary), var(--brand-secondary))" }}
          />
          <p className="text-[15px] font-extrabold text-[#F8FAFC]">Tabelas</p>
        </div>
        <button
          type="button"
          onClick={addTable}
          disabled={editingKey !== null}
          className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#F8FAFC] text-[13px] font-medium cursor-pointer hover:bg-white/[0.08] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <Plus size={14} strokeWidth={2} />
          Adicionar Tabela
        </button>
      </div>

      {app.tables.length === 0 && !draftTable && (
        <div className="flex flex-col items-center justify-center gap-2 py-10 text-center text-[#94A3B8]">
          <Table2 size={18} strokeWidth={1} className="opacity-40" />
          <p className="text-[13px] font-medium">Nenhuma tabela</p>
          <p className="text-[11px]">Adicione tabelas para começar a estruturar seu app</p>
        </div>
      )}

      <div className="flex flex-col gap-3">
        {app.tables.map((t) => (
          <TableCard
            key={t.id}
            table={t}
            otherTables={app.tables.filter((other) => other.id && other.id !== t.id)}
            authEmailEnabled={app.auth_email_enabled}
            locked={editingKey !== null && editingKey !== t.id}
            startInEdit={false}
            onEnterEdit={() => setEditingKey(t.id!)}
            onExitEdit={() => setEditingKey(null)}
            onCreate={(input) => createTable.mutateAsync(input)}
            onUpdate={(input) => updateTable.mutateAsync({ tableId: t.id!, ...input })}
            onDelete={() => deleteTable.mutateAsync(t.id!)}
            onSaved={() => setEditingKey(null)}
            onDiscardDraft={() => {}}
            onDeleted={() => setEditingKey(null)}
          />
        ))}
        {draftTable && (
          <TableCard
            key="draft"
            table={draftTable}
            otherTables={app.tables.filter((other) => other.id)}
            authEmailEnabled={app.auth_email_enabled}
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
      setError(err instanceof Error ? err.message : "Erro ao salvar");
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">Provedores de Login</h3>

        <div className="flex items-center justify-between">
          <div className="flex flex-col gap-0.5">
            <p className="text-sm font-semibold text-[#F8FAFC]">E-mail</p>
            <p className="text-xs text-[#94A3B8]">Registro e login via email/senha</p>
          </div>
          <Switch checked={authEmail} onCheckedChange={setAuthEmail} className="shrink-0" />
        </div>

        <div className="border-t border-white/[0.06]" />

        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-0.5">
              <p className="text-sm font-semibold text-[#F8FAFC]">Google</p>
              <p className="text-xs text-[#94A3B8]">Login via conta Google</p>
            </div>
            <Switch checked={googleEnabled} onCheckedChange={setGoogleEnabled} className="shrink-0" />
          </div>
          {googleEnabled && (
            <div className="flex flex-col gap-3 border-t border-white/[0.06] pt-3">
              <div>
                <Label className="text-[12px] font-medium text-[#94A3B8]">Client ID</Label>
                <Input
                  value={googleClientId}
                  onChange={(e) => setGoogleClientId(e.target.value)}
                  placeholder="Google OAuth Client ID"
                  className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                />
              </div>
              <div>
                <Label className="text-[12px] font-medium text-[#94A3B8]">Client Secret</Label>
                <div className="relative mt-1">
                  <Input
                    type={showGoogleSecret ? "text" : "password"}
                    value={googleClientSecret}
                    onChange={(e) => setGoogleClientSecret(e.target.value)}
                    placeholder="Client Secret"
                    className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus w-full pr-10"
                  />
                  <button
                    type="button"
                    title="Show/hide secret"
                    onClick={() => setShowGoogleSecret(!showGoogleSecret)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-transparent border-none cursor-pointer"
                  >
                    {showGoogleSecret ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                </div>
              </div>
              <div>
                <Label className="text-[12px] font-medium text-[#94A3B8]">Redirect URL</Label>
                <Input
                  value={googleRedirectUrl}
                  onChange={(e) => setGoogleRedirectUrl(e.target.value)}
                  placeholder={`https://seu-dominio.com/${app.name}/auth/google/callback`}
                  className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                />
                <p className="text-[11px] text-[#64748B] mt-1">Configure este URL no Google Cloud Console</p>
              </div>
              <div>
                <Label className="text-[12px] font-medium text-[#94A3B8]">Domínios permitidos</Label>
                <Input
                  value={googleAllowedDomains}
                  onChange={(e) => setGoogleAllowedDomains(e.target.value)}
                  placeholder="zeeplabs.com, zeepfly.com"
                  className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                />
                <p className="text-[11px] text-[#64748B] mt-1">Separados por vírgula. Vazio = qualquer domínio.</p>
              </div>
            </div>
          )}
        </div>
      </div>

      {error && <p className="text-xs text-red-400">{error}</p>}
      <SaveBar onSave={save} saving={updateApp.isPending} saved={saved} />
    </div>
  );
}

function StorageTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
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
      setError(err instanceof Error ? err.message : "Erro ao salvar");
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">Storage (S3)</h3>
        <div className="flex items-center justify-between">
          <div className="flex flex-col gap-0.5">
            <p className="text-sm font-semibold text-[#F8FAFC]">Habilitar storage</p>
            <p className="text-xs text-[#94A3B8]">Permite upload de arquivos para este app</p>
          </div>
          <Switch checked={storageEnabled} onCheckedChange={setStorageEnabled} className="shrink-0" />
        </div>
        {storageEnabled && (
          <div className="flex flex-col gap-3 border-t border-white/[0.06] pt-3">
            <div>
              <Label className="text-[12px] font-medium text-[#94A3B8]">Bucket</Label>
              {globalStorage ? (
                <>
                  <p className="text-[11px] text-[#94A3B8] mt-1 mb-1">
                    Usando o storage global configurado pela plataforma.
                  </p>
                  <Input
                    value={app.name}
                    readOnly
                    className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#94A3B8] placeholder:text-white/30 brand-focus mt-1 cursor-not-allowed opacity-60"
                  />
                </>
              ) : (
                <Input
                  value={storageBucket}
                  onChange={(e) => setStorageBucket(e.target.value)}
                  placeholder="meu-bucket"
                  className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                />
              )}
            </div>
            {!globalStorage && (
              <>
                <div>
                  <Label className="text-[12px] font-medium text-[#94A3B8]">Region</Label>
                  <Input
                    value={storageRegion}
                    onChange={(e) => setStorageRegion(e.target.value)}
                    placeholder="us-east-1"
                    className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                  />
                </div>
                <div>
                  <Label className="text-[12px] font-medium text-[#94A3B8]">Endpoint</Label>
                  <Input
                    value={storageEndpoint}
                    onChange={(e) => setStorageEndpoint(e.target.value)}
                    placeholder="https://nyc3.digitaloceanspaces.com"
                    className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                  />
                </div>
                <div>
                  <Label className="text-[12px] font-medium text-[#94A3B8]">Access Key ID</Label>
                  <Input
                    value={storageAccessKey}
                    onChange={(e) => setStorageAccessKey(e.target.value)}
                    placeholder="DO00XXXXXXXXXXXX"
                    className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
                  />
                </div>
                <div>
                  <Label className="text-[12px] font-medium text-[#94A3B8]">Secret Access Key</Label>
                  <div className="relative mt-1">
                    <Input
                      type={showStorageSecret ? "text" : "password"}
                      value={storageSecretKey}
                      onChange={(e) => setStorageSecretKey(e.target.value)}
                      placeholder="Secret Key"
                      className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus w-full pr-10"
                    />
                    <button
                      type="button"
                      title="Show/hide secret"
                      onClick={() => setShowStorageSecret(!showStorageSecret)}
                      className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-transparent border-none cursor-pointer"
                    >
                      {showStorageSecret ? <EyeOff size={16} /> : <Eye size={16} />}
                    </button>
                  </div>
                </div>
              </>
            )}
            <p className="text-[11px] text-[#94A3B8]">
              <Trans
                i18nKey="appForm.storageHint"
                values={{ path: `/${app.name}/files/*` }}
                components={{ 1: <code className="text-[#B3D1FF]" /> }}
              />
            </p>
          </div>
        )}
      </div>

      {error && <p className="text-xs text-red-400">{error}</p>}
      <SaveBar onSave={save} saving={updateApp.isPending} saved={saved} />
    </div>
  );
}

function ApiTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
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
      setError(err instanceof Error ? err.message : "Erro ao salvar");
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">
          Base da API
        </h3>
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 bg-black/30 rounded-xl px-4 py-3">
            <code className="text-sm text-[#B3D1FF] break-all font-mono">
              {window.location.origin}/{app.name}
            </code>
            <button
              type="button"
              title="Copy to clipboard"
              onClick={() => navigator.clipboard.writeText(`${window.location.origin}/${app.name}`)}
              className="shrink-0 p-1.5 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors"
            >
              <Copy size={14} />
            </button>
          </div>
        </div>
      </div>

      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
        <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">API</h3>
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-0.5">
              <p className="text-sm font-semibold text-[#F8FAFC]">Rate Limit</p>
              <p className="text-xs text-[#94A3B8]">Limitar requisições por minuto por IP para este app</p>
            </div>
            <Switch checked={rateLimitEnabled} onCheckedChange={setRateLimitEnabled} className="shrink-0" />
          </div>
          {rateLimitEnabled && (
            <div className="flex flex-col gap-2 border-t border-white/[0.06] pt-3">
              <Label className="text-[12px] font-medium text-[#94A3B8]">Requests per minute</Label>
              <Input
                type="number"
                min={1}
                max={10000}
                value={rateLimitRPM}
                onChange={(e) => setRateLimitRPM(parseInt(e.target.value) || 60)}
                className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus w-32"
              />
              <p className="text-[11px] text-[#94A3B8]">Máximo de requisições por IP a cada 60 segundos</p>
            </div>
          )}
        </div>
      </div>

      {error && <p className="text-xs text-red-400">{error}</p>}
      <SaveBar onSave={save} saving={updateApp.isPending} saved={saved} />
    </div>
  );
}

function TokensTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { data: tokens, isLoading } = useAppTokens(app.id);
  const createToken = useCreateAppToken(app.id);
  const revokeToken = useRevokeAppToken(app.id);
  const regenerateSecret = useRegenerateAppSecret(app.id);
  const [showCreate, setShowCreate] = useState(false);
  const [showRegenerateConfirm, setShowRegenerateConfirm] = useState(false);
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);

  if (app.auth_email_enabled) {
    return (
      <div className="rounded-2xl border border-yellow-500/[0.18] bg-yellow-500/[0.06] px-6 py-5 text-sm text-yellow-400">
        App tokens estão disponíveis apenas para apps sem autenticação por e-mail.
      </div>
    );
  }

  const statusBadge = (t: AppToken) => {
    if (t.revoked_at) return <span className="text-[11px] font-medium text-red-400 bg-red-500/[0.12] px-2 py-0.5 rounded-full">Revogado</span>;
    if (t.expires_at && new Date(t.expires_at) < new Date()) return <span className="text-[11px] font-medium text-yellow-400 bg-yellow-500/[0.12] px-2 py-0.5 rounded-full">Expirado</span>;
    return <span className="text-[11px] font-medium text-emerald-400 bg-emerald-500/[0.12] px-2 py-0.5 rounded-full">Ativo</span>;
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div
            className="h-6 w-1 rounded-full"
            style={{ background: "linear-gradient(to bottom, var(--brand-primary), var(--brand-secondary))" }}
          />
          <p className="text-[15px] font-extrabold text-[#F8FAFC]">Access Tokens</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowRegenerateConfirm(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#94A3B8] text-[13px] font-medium cursor-pointer hover:text-white transition-colors"
          >
            <RefreshCw size={14} /> Regenerar Secret
          </button>
          <button
            type="button"
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#F8FAFC] text-[13px] font-medium cursor-pointer hover:bg-white/[0.08] transition-colors"
          >
            <Plus size={14} /> Novo Token
          </button>
        </div>
      </div>

      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-4">
        <div className="flex items-center justify-between mb-2">
          <p className="text-[12px] font-semibold text-[#94A3B8] uppercase tracking-wider">JWT Secret</p>
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
            className="flex items-center gap-1 text-[11px] text-[#94A3B8] hover:text-white bg-transparent border-none cursor-pointer"
          >
            {revealedSecret ? <EyeOff size={13} /> : <Eye size={13} />}
            {revealedSecret ? "Ocultar" : "Revelar"}
          </button>
        </div>
        {revealedSecret ? (
          <div className="flex items-center gap-2 bg-black/30 rounded-xl px-4 py-3">
            <code className="text-sm text-[#B3D1FF] break-all font-mono flex-1">{revealedSecret}</code>
            <button
              type="button"
              title="Copy to clipboard"
              onClick={() => navigator.clipboard.writeText(revealedSecret)}
              className="shrink-0 p-1.5 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors bg-transparent border-none cursor-pointer"
            >
              <Copy size={14} />
            </button>
          </div>
        ) : (
          <p className="text-xs text-[#64748B]">O secret é usado para assinar JWTs. Clique em "Revelar" para vê-lo.</p>
        )}
      </div>

      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-4">
        <p className="text-[12px] font-semibold text-[#94A3B8] uppercase tracking-wider mb-3">Tokens</p>
        {isLoading ? (
          <p className="text-sm text-[#94A3B8]">Carregando...</p>
        ) : !tokens || tokens.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-8 text-center text-[#94A3B8]">
            <Key size={18} className="opacity-40" />
            <p className="text-[13px] font-medium">Nenhum token</p>
            <p className="text-[11px]">Crie um token para gerar um JWT</p>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {tokens.map((t) => (
              <div key={t.id} className="flex items-center justify-between bg-black/20 rounded-xl px-4 py-3">
                <div className="flex flex-col gap-0.5 min-w-0">
                  <p className="text-sm font-semibold text-[#F8FAFC] truncate">{t.name}</p>
                  <p className="text-[11px] text-[#64748B]">
                    {t.expires_at ? `Expira ${new Date(t.expires_at).toLocaleDateString()}` : "Nunca expira"}
                    {t.last_used_at && ` · Último uso ${new Date(t.last_used_at).toLocaleDateString()}`}
                  </p>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {statusBadge(t)}
                  {!t.revoked_at && (
                    <button
                      type="button"
                      title="Revoke token"
                      onClick={() => revokeToken.mutate(t.id)}
                      className="p-1.5 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-red-400 transition-colors bg-transparent border-none cursor-pointer"
                    >
                      <X size={14} />
                    </button>
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

      {showRegenerateConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#0F172A] border border-white/[0.08] rounded-2xl p-6 max-w-md w-full mx-4">
            <div className="flex items-center gap-3 mb-4">
              <AlertTriangle size={20} className="text-yellow-400" />
              <p className="text-[15px] font-bold text-[#F8FAFC]">Regenerar JWT Secret</p>
            </div>
            <p className="text-sm text-[#94A3B8] mb-6">
              Todos os tokens existentes serão imediatamente invalidados. Esta ação não pode ser desfeita.
            </p>
            <div className="flex items-center justify-end gap-3">
              <button
                type="button"
                onClick={() => setShowRegenerateConfirm(false)}
                className="px-4 py-2 rounded-full border border-white/[0.12] text-[13px] text-[#94A3B8] cursor-pointer bg-transparent hover:text-white transition-colors"
              >
                Cancelar
              </button>
              <button
                type="button"
                onClick={() => {
                  regenerateSecret.mutate(undefined, {
                    onSuccess: (data) => {
                      setShowRegenerateConfirm(false);
                      setRevealedSecret(data.jwt_secret);
                    },
                  });
                }}
                disabled={regenerateSecret.isPending}
                className="px-4 py-2 rounded-full text-[13px] font-semibold text-white cursor-pointer disabled:opacity-50"
                style={{ background: "linear-gradient(to right, #EF4444, #DC2626)" }}
              >
                {regenerateSecret.isPending ? "Regenerando..." : "Confirmar Regeneração"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function CreateTokenModal({ appId, onClose, onCreated }: { appId: string; onClose: () => void; onCreated: (jwt: string) => void }) {
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
      setError(err instanceof Error ? err.message : "Erro ao criar token");
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-[#0F172A] border border-white/[0.08] rounded-2xl p-6 max-w-md w-full mx-4">
        <p className="text-[15px] font-bold text-[#F8FAFC] mb-4">Novo Token</p>
        <div className="flex flex-col gap-3">
          <div>
            <Label className="text-[12px] font-medium text-[#94A3B8]">Nome</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Ex: Production frontend"
              className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1"
            />
          </div>
          <div>
            <Label className="text-[12px] font-medium text-[#94A3B8]">Expiração</Label>
            <select
              value={expiration}
              onChange={(e) => setExpiration(e.target.value)}
              className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] brand-focus mt-1 w-full px-3"
            >
              <option value="7d">7 dias</option>
              <option value="30d">30 dias</option>
              <option value="365d">365 dias</option>
              <option value="never">Nunca expira</option>
            </select>
          </div>
          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>
        <div className="flex items-center justify-end gap-3 mt-6">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 rounded-full border border-white/[0.12] text-[13px] text-[#94A3B8] cursor-pointer bg-transparent hover:text-white transition-colors"
          >
            Cancelar
          </button>
          <button
            type="button"
            onClick={handleCreate}
            disabled={!name || createToken.isPending}
            className="px-4 py-2 rounded-full text-[13px] font-semibold text-white cursor-pointer disabled:opacity-50"
            style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}
          >
            {createToken.isPending ? "Criando..." : "Criar Token"}
          </button>
        </div>
      </div>
    </div>
  );
}

function TokenRevealModal({ jwt, onClose }: { jwt: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-[#0F172A] border border-white/[0.08] rounded-2xl p-6 max-w-lg w-full mx-4">
        <div className="flex items-center gap-3 mb-4">
          <Key size={20} className="text-[var(--brand-light)]" />
          <p className="text-[15px] font-bold text-[#F8FAFC]">Token Criado</p>
        </div>
        <p className="text-xs text-yellow-400 mb-3">Copie este token agora. Não será possível vê-lo novamente.</p>
        <div className="flex items-center gap-2 bg-black/30 rounded-xl px-4 py-3">
          <code className="text-sm text-[#B3D1FF] break-all font-mono flex-1 max-h-32 overflow-y-auto">{jwt}</code>
          <button
            type="button"
            title="Copy to clipboard"
            onClick={() => { navigator.clipboard.writeText(jwt); setCopied(true); }}
            className="shrink-0 p-2 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors bg-transparent border-none cursor-pointer"
          >
            <Copy size={16} />
          </button>
        </div>
        {copied && <p className="text-[11px] text-emerald-400 mt-2">Copiado!</p>}
        <div className="flex justify-end mt-4">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 rounded-full text-[13px] font-semibold text-white cursor-pointer"
            style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}
          >
            Fechar
          </button>
        </div>
      </div>
    </div>
  );
}

function SaveBar({ onSave, saving, saved }: { onSave: () => void; saving: boolean; saved: boolean }) {
  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        onClick={onSave}
        disabled={saving}
        className="text-[13px] font-semibold px-5 py-2.5 rounded-full text-white cursor-pointer disabled:opacity-50"
        style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}
      >
        {saving ? "Salvando..." : "Salvar"}
      </button>
      {saved && !saving && <span className="text-xs text-[#94A3B8]">Salvo.</span>}
    </div>
  );
}
