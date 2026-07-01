import { useState, useEffect } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { motion } from "framer-motion";
import {
  ArrowLeft,
  Plus,
  Trash2,
  ChevronDown,
  ChevronUp,
  Table2,
  Eye,
  EyeOff,
  Copy,
} from "lucide-react";
import {
  useCreateApp,
  useUpdateApp,
  useApps,
  AppDef,
  TableDef,
  ColumnDef,
} from "../lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";


const COLUMN_TYPES = [
  "text",
  "integer",
  "bigint",
  "boolean",
  "uuid",
  "timestamptz",
  "numeric",
  "jsonb",
];

const emptyColumn = (): ColumnDef => ({
  name: "",
  type: "text",
  required: false,
  default: "",
  unique: false,
});

const emptyTable = (): TableDef => ({
  name: "",
  rls: "disabled",
  columns: [emptyColumn()],
});


function validateName(name: string): string | null {
  if (!name.trim()) return "Nome obrigatório";
  if (!/^[a-z][a-z0-9_-]*$/.test(name))
    return "Apenas letras minúsculas, números, hífen e _ (máx 32), começando com letra";
  if (name.length > 32) return "Máximo de 32 caracteres";
  return null;
}


export default function AppFormPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") || "database";
  const isEdit = Boolean(id);

  const setTab = (value: string) => {
    setSearchParams({ tab: value }, { replace: true });
  };

  const { data: apps } = useApps();
  const editTarget = isEdit && apps ? apps.find((a) => a.id === id) : null;

  const [appName, setAppName] = useState("");
  const [authEmail, setAuthEmail] = useState(false);
  const [googleEnabled, setGoogleEnabled] = useState(false);
  const [googleClientId, setGoogleClientId] = useState("");
  const [googleClientSecret, setGoogleClientSecret] = useState("");
  const [googleRedirectUrl, setGoogleRedirectUrl] = useState("");
  const [googleAllowedDomains, setGoogleAllowedDomains] = useState("");
  const [showGoogleSecret, setShowGoogleSecret] = useState(false);
  const [storageEnabled, setStorageEnabled] = useState(false);
  const [storageBucket, setStorageBucket] = useState("");
  const [storageRegion, setStorageRegion] = useState("");
  const [storageEndpoint, setStorageEndpoint] = useState("");
  const [storageAccessKey, setStorageAccessKey] = useState("");
  const [storageSecretKey, setStorageSecretKey] = useState("");
  const [showStorageSecret, setShowStorageSecret] = useState(false);
  const [rateLimitEnabled, setRateLimitEnabled] = useState(false);
  const [rateLimitRPM, setRateLimitRPM] = useState(60);
  const [tables, setTables] = useState<TableDef[]>([]);
  const [collapsedTables, setCollapsedTables] = useState<Set<number>>(
    new Set(),
  );
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [submitError, setSubmitError] = useState<string | null>(null);

  const createApp = useCreateApp();
  const updateApp = useUpdateApp();
  const isMutating = createApp.isPending || updateApp.isPending;

  useEffect(() => {
    if (editTarget) {
      setAppName(editTarget.name);
      setAuthEmail(editTarget.auth_email_enabled);
      const providers = (editTarget as any).auth_providers;
      if (providers?.google?.enabled) {
        setGoogleEnabled(true);
        setGoogleClientId(providers.google.client_id || "");
        setGoogleClientSecret("");
        setGoogleRedirectUrl(providers.google.redirect_url || "");
        setGoogleAllowedDomains((providers.google.allowed_domains || []).join(", "));
      }
      const sc = (editTarget as any).storage_config;
      if (sc?.bucket) {
        setStorageEnabled(true);
        setStorageBucket(sc.bucket || "");
        setStorageRegion(sc.region || "");
        setStorageEndpoint(sc.endpoint || "");
        setStorageAccessKey(sc.access_key_id || "");
        setStorageSecretKey("");
      }
      const rl = (editTarget as any).rate_limit;
      if (rl?.enabled) {
        setRateLimitEnabled(true);
        setRateLimitRPM(rl.requests_per_minute || 60);
      }
      setTables(
        editTarget.tables.map((t) => ({
          ...t,
          columns: t.columns.map((c) => ({ ...c })),
        })),
      );
    } else if (!isEdit) {
      setAppName("");
      setAuthEmail(false);
      setGoogleEnabled(false);
      setGoogleClientId("");
      setGoogleClientSecret("");
      setGoogleRedirectUrl("");
      setGoogleAllowedDomains("");
      setStorageEnabled(false);
      setStorageBucket("");
      setStorageRegion("");
      setStorageEndpoint("");
      setStorageAccessKey("");
      setStorageSecretKey("");
      setRateLimitEnabled(false);
      setRateLimitRPM(60);
      setTables([]);
      setCollapsedTables(new Set());
    }
    setErrors({});
    setSubmitError(null);
  }, [editTarget, isEdit]);


  const addTable = () => setTables((prev) => [...prev, emptyTable()]);

  const removeTable = (ti: number) => {
    setTables((prev) => prev.filter((_, i) => i !== ti));
    setCollapsedTables((prev) => {
      const next = new Set(prev);
      next.delete(ti);
      return next;
    });
  };

  const updateTable = (ti: number, patch: Partial<TableDef>) => {
    setTables((prev) =>
      prev.map((t, i) => (i === ti ? { ...t, ...patch } : t)),
    );
  };

  const toggleCollapse = (ti: number) => {
    setCollapsedTables((prev) => {
      const next = new Set(prev);
      next.has(ti) ? next.delete(ti) : next.add(ti);
      return next;
    });
  };

  const addColumn = (ti: number) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti ? { ...t, columns: [...t.columns, emptyColumn()] } : t,
      ),
    );
  };

  const removeColumn = (ti: number, ci: number) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti ? { ...t, columns: t.columns.filter((_, j) => j !== ci) } : t,
      ),
    );
  };

  const updateColumn = (ti: number, ci: number, patch: Partial<ColumnDef>) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti
          ? {
              ...t,
              columns: t.columns.map((c, j) =>
                j === ci ? { ...c, ...patch } : c,
              ),
            }
          : t,
      ),
    );
  };


  function validate(): boolean {
    const errs: Record<string, string> = {};

    const nameErr = validateName(appName);
    if (nameErr) errs["appName"] = nameErr;

    tables.forEach((table, ti) => {
      if (!table.name.trim())
        errs[`table_${ti}_name`] = t("appForm.tableNameRequired");
      if (table.columns.length === 0)
        errs[`table_${ti}_cols`] = t("appForm.tableMinColumns");
      table.columns.forEach((col, ci) => {
        if (!col.name.trim()) errs[`col_${ti}_${ci}_name`] = t("appForm.colNameRequired");
        if (!col.type) errs[`col_${ti}_${ci}_type`] = t("appForm.colTypeRequired");
      });
    });

    setErrors(errs);
    return Object.keys(errs).length === 0;
  }


  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!validate()) return;
    setSubmitError(null);

    const payload: Record<string, unknown> = { name: appName, auth_email_enabled: authEmail, tables };

    if (googleEnabled) {
      const domains = googleAllowedDomains.split(",").map((d) => d.trim()).filter(Boolean);
      payload.auth_providers = {
        google: {
          enabled: true,
          client_id: googleClientId,
          client_secret: googleClientSecret,
          redirect_url: googleRedirectUrl || `/${appName}/auth/google/callback`,
          ...(domains.length > 0 ? { allowed_domains: domains } : {}),
        },
      };
    }

    if (storageEnabled && storageBucket && storageRegion && storageEndpoint && storageAccessKey && storageSecretKey) {
      payload.storage_config = {
        bucket: storageBucket,
        region: storageRegion,
        endpoint: storageEndpoint,
        access_key_id: storageAccessKey,
        secret_access_key: storageSecretKey,
      };
    }

    if (rateLimitEnabled) {
      payload.rate_limit = {
        enabled: true,
        requests_per_minute: rateLimitRPM,
      };
    }

    try {
      if (isEdit && editTarget) {
        await updateApp.mutateAsync({ id: editTarget.id, ...payload } as any);
      } else {
        await createApp.mutateAsync(payload as any);
      }
      navigate("/apps");
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : t("appForm.unexpectedError"));
    }
  }


  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
    >
      {/* Back button */}
      <button
        type="button"
        onClick={() => navigate("/apps")}
        className="mb-6 flex items-center gap-2 text-[13px] text-[#94A3B8] hover:text-white transition-colors bg-transparent border-none cursor-pointer"
      >
        <ArrowLeft size={14} strokeWidth={1.5} />
        Voltar para Apps
      </button>

      {/* Header */}
      <div className="mb-8">
        <span className="mb-3 inline-block rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: 'rgba(var(--brand-primary-rgb), 0.2)',
            backgroundColor: 'rgba(var(--brand-primary-rgb), 0.12)',
            color: 'var(--brand-light)',
          }}
        >
          {isEdit ? t("appForm.editBadge") : t("appForm.newBadge")}
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">
          {isEdit
            ? editTarget
              ? `Editar "${editTarget.name}"`
              : t("appForm.notFound")
            : "Criar Aplicativo"}
        </h2>
        <p className="mt-1 text-sm text-[#94A3B8]">
          {isEdit
            ? "Altere as configurações do app e clique em Salvar"
            : "Configure tabelas, colunas e permissões do seu novo app"}
        </p>
      </div>

      {isEdit && !editTarget ? (
        <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">
          {t("appForm.notFound")}
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col gap-6">
          <Tabs value={tab} onValueChange={setTab} className="w-full">
            <TabsList className="w-full justify-start gap-1 rounded-2xl border border-white/[0.08] bg-white/[0.03] p-1.5 mb-2 h-auto">
              <TabsTrigger value="database"
                className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
              >
                Banco de Dados
              </TabsTrigger>
              <TabsTrigger value="auth"
                className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
              >
                Provedores de Login
              </TabsTrigger>
              <TabsTrigger value="storage"
                className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
              >
                Storage (S3)
              </TabsTrigger>
              <TabsTrigger value="api"
                className="rounded-xl px-4 py-2 text-[13px] font-semibold text-[#94A3B8] data-[state=active]:bg-white/[0.08] data-[state=active]:text-[#F8FAFC] data-[state=active]:shadow-none"
              >
                {t("appForm.tabApi")}
              </TabsTrigger>
            </TabsList>

            <TabsContent value="database" className="mt-0 flex flex-col gap-6">
              {/* ── Basic Info Card ── */}
              <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-5">
                {/* App name */}
                <div className="flex flex-col gap-2">
                  <Label className="text-[13px] font-semibold text-[#94A3B8]">
                    Nome do App
                  </Label>
                  <Input
                    value={appName}
                    onChange={(e) =>
                      setAppName(
                        e.target.value.toLowerCase().replace(/[\s]+/g, "-"),
                      )
                    }
                    placeholder={t("appForm.namePlaceholder")}
                    className={cn(
                      "h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus",
                      errors["appName"] &&
                        "border-red-500/50",
                    )}
                  />
                  {errors["appName"] && (
                    <p className="text-xs text-red-400">{errors["appName"]}</p>
                  )}
                  <p className="text-[11px] text-[#94A3B8]">
                    {t("appForm.nameHint")}
                  </p>
                </div>

            {/* Divider */}
            <div className="border-t border-white/[0.06]" />

            {/* Auth toggle */}
            <div className="flex items-center justify-between">
              <div className="flex flex-col gap-0.5">
                <p className="text-sm font-semibold text-[#F8FAFC]">
                  Autenticação por e-mail
                </p>
                <p className="text-xs text-[#94A3B8]">
                  Habilita registro de usuários e permite login via email/senha
                  em seu app
                </p>
              </div>
              <Switch
                checked={authEmail}
                onCheckedChange={setAuthEmail}
                className="shrink-0"
              />
            </div>

          </div>

              {/* ── Tables Section ── */}
              <div className="flex flex-col gap-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="h-6 w-1 rounded-full"
                  style={{
                    background: 'linear-gradient(to bottom, var(--brand-primary), var(--brand-secondary))',
                  }}
                />
                <p className="text-[15px] font-extrabold text-[#F8FAFC]">
                  Tabelas
                </p>
              </div>
              <button
                type="button"
                onClick={addTable}
                className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#F8FAFC] text-[13px] font-medium cursor-pointer hover:bg-white/[0.08] transition-colors"
              >
                <Plus size={13} strokeWidth={2} />
                Adicionar Tabela
              </button>
            </div>

            {tables.length === 0 && (
              <div className="flex flex-col items-center justify-center gap-3 py-14 text-[#94A3B8] border border-dashed border-white/[0.08] rounded-2xl">
                <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-white/[0.04] border border-white/[0.06]">
                  <Table2 size={18} strokeWidth={1} className="opacity-40" />
                </div>
                <div className="text-center">
                  <p className="text-[13px] font-medium">Nenhuma tabela</p>
                  <p className="text-[12px] text-white/30 mt-1">
                    Adicione tabelas para começar a estruturar seu app
                  </p>
                </div>
              </div>
            )}

            <div className="flex flex-col gap-3">
              {tables.map((table, ti) => {
                const isCollapsed = collapsedTables.has(ti);
                return (
                  <motion.div
                    key={ti}
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
                    className="bg-white/[0.04] border border-white/[0.08] rounded-xl overflow-hidden"
                  >
                    {/* Table header row */}
                    <div className="flex items-center gap-3 px-4 py-3">
                      <Table2
                        size={15}
                        strokeWidth={1.5}
                        className="text-[#B3D1FF] shrink-0"
                      />
                      <Input
                        value={table.name}
                        onChange={(e) =>
                          updateTable(ti, {
                            name: e.target.value
                              .toLowerCase()
                              .replace(/[\s-]+/g, "_"),
                          })
                        }
                        placeholder={`tabela_${ti + 1}`}
                        className={cn(
                          "h-8 px-3 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus",
                          errors[`table_${ti}_name`] && "border-red-500/50",
                        )}
                      />
                      <div className="flex flex-col gap-1 shrink-0">
                        <span className="text-[10px] font-semibold text-[#94A3B8] uppercase tracking-[0.06em] leading-none">
                          Acesso
                        </span>
                        <span className="text-[9px] text-[#64748B] leading-tight">
                          {table.rls === "enabled"
                            ? "Só o dono vê"
                            : "Todos veem"}
                        </span>
                      </div>
                      <Select
                        value={table.rls}
                        onValueChange={(val) => updateTable(ti, { rls: val })}
                      >
                        <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-3 brand-focus">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                          <SelectItem
                            value="disabled"
                            className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
                          >
                            Público
                          </SelectItem>
                          <SelectItem
                            value="enabled"
                            className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
                          >
                            Restrito
                          </SelectItem>
                        </SelectContent>
                      </Select>

                      {/* Collapse button */}
                      <button
                        type="button"
                        onClick={() => toggleCollapse(ti)}
                        className="w-7 h-7 shrink-0 flex items-center justify-center rounded-md border border-white/[0.08] bg-transparent text-[#94A3B8] cursor-pointer hover:bg-white/[0.06] transition-colors"
                      >
                        {isCollapsed ? (
                          <ChevronDown size={13} strokeWidth={1.5} />
                        ) : (
                          <ChevronUp size={13} strokeWidth={1.5} />
                        )}
                      </button>

                      {/* Remove table button */}
                      <button
                        type="button"
                        onClick={() => removeTable(ti)}
                        className="w-7 h-7 shrink-0 flex items-center justify-center rounded-md border border-red-500/[0.15] bg-red-500/[0.08] text-red-400 cursor-pointer hover:bg-red-500/[0.14] transition-colors"
                      >
                        <Trash2 size={13} strokeWidth={1.5} />
                      </button>
                    </div>

                    {!isCollapsed && (
                      <div className="border-t border-white/[0.06] px-4 py-3.5 flex flex-col gap-3">
                        {errors[`table_${ti}_cols`] && (
                          <p className="text-xs text-red-400 mb-1">
                            {errors[`table_${ti}_cols`]}
                          </p>
                        )}

                        {/* Auto columns info */}
                        <p className="text-[11px] text-[#64748B] italic">
                          As colunas <code className="text-[#94A3B8] not-italic">id</code>,{" "}
                          <code className="text-[#94A3B8] not-italic">created_at</code> e{" "}
                          <code className="text-[#94A3B8] not-italic">updated_at</code> são criadas automaticamente.
                        </p>

                        {/* Column header — hidden on mobile */}
                        <div
                          className="grid gap-3 items-center max-md:hidden"
                          style={{
                            gridTemplateColumns: "1fr 140px 80px 80px 40px",
                          }}
                        >
                          <span className="text-[11px] text-[#94A3B8] font-semibold">
                            Nome
                          </span>
                          <span className="text-[11px] text-[#94A3B8] font-semibold">
                            Tipo
                          </span>
                          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">
                            Req.
                          </span>
                          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">
                            Único
                          </span>
                          <span />
                        </div>

                        {/* Column rows */}
                        <div className="flex flex-col gap-2.5 mb-3">
                          {table.columns.map((col, ci) => (
                            <div
                              key={ci}
                              className="grid gap-3 items-center max-md:flex max-md:flex-col max-md:gap-2 max-md:p-3 max-md:bg-white/[0.03] max-md:rounded-xl max-md:border max-md:border-white/[0.06]"
                              style={{
                                gridTemplateColumns: "1fr 140px 80px 80px 40px",
                              }}
                            >
                              <Input
                                value={col.name}
                                onChange={(e) =>
                                  updateColumn(ti, ci, {
                                    name: e.target.value
                                      .toLowerCase()
                                      .replace(/[\s-]+/g, "_"),
                                  })
                                }
                                placeholder="nome_coluna"
                                className={cn(
                                  "h-8 px-2.5 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus",
                                  errors[`col_${ti}_${ci}_name`] &&
                                    "border-red-500/50",
                                )}
                              />

                              <div className="contents max-md:flex max-md:items-center max-md:gap-2">
                                <Select
                                  value={col.type}
                                  onValueChange={(val) =>
                                    updateColumn(ti, ci, { type: val })
                                  }
                                >
                                  <SelectTrigger className="h-8 w-[130px] max-md:flex-1 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-2 brand-focus">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                                    {COLUMN_TYPES.map((t) => (
                                      <SelectItem
                                        key={t}
                                      value={t}
                                      className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
                                      >
                                        {t}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>

                                {/* Required toggle */}
                                <div className="flex justify-center">
                                  <Switch
                                    checked={col.required}
                                    onCheckedChange={(val) =>
                                      updateColumn(ti, ci, { required: val })
                                    }
                                    className="h-5 w-9"
                                  />
                                </div>

                                {/* Unique toggle */}
                                <div className="flex justify-center">
                                  <Switch
                                    checked={col.unique}
                                    onCheckedChange={(val) =>
                                      updateColumn(ti, ci, { unique: val })
                                    }
                                    className="h-5 w-9"
                                  />
                                </div>

                                {/* Remove column button */}
                                <button
                                  type="button"
                                  onClick={() => removeColumn(ti, ci)}
                                  disabled={table.columns.length <= 1}
                                  className={cn(
                                    "w-7 h-7 flex items-center justify-center rounded-md border border-red-500/[0.12] bg-red-500/[0.06] transition-colors",
                                    table.columns.length <= 1
                                      ? "text-red-400/30 cursor-not-allowed"
                                      : "text-red-400 cursor-pointer hover:bg-red-500/[0.12]",
                                  )}
                                >
                                  <Trash2 size={12} strokeWidth={1.5} />
                                </button>
                              </div>
                            </div>
                          ))}
                        </div>

                        <button
                          type="button"
                          onClick={() => addColumn(ti)}
                          className="flex items-center gap-1.5 text-[12px] font-semibold bg-transparent border border-white/[0.08] rounded-full px-3 py-1.5 cursor-pointer hover:bg-white/[0.06] transition-colors self-start"
                          style={{ color: 'var(--brand-light)' }}
                          onMouseEnter={(e) => e.currentTarget.style.color = '#D6E8FF'}
                          onMouseLeave={(e) => e.currentTarget.style.color = 'var(--brand-light)'}
                        >
                          <Plus size={11} strokeWidth={2} />
                          Adicionar Coluna
                        </button>
                      </div>
                    )}
                  </motion.div>
                );
              })}
            </div>
          </div>
            </TabsContent>

            <TabsContent value="auth" className="mt-0">
              <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
                <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">
                  Provedores de Login
                </h3>

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
                        <Input value={googleClientId} onChange={(e) => setGoogleClientId(e.target.value)}
                          placeholder="Google OAuth Client ID"
                          className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                      </div>
                      <div>
                        <Label className="text-[12px] font-medium text-[#94A3B8]">Client Secret</Label>
                        <div className="relative mt-1">
                          <Input type={showGoogleSecret ? "text" : "password"} value={googleClientSecret}
                            onChange={(e) => setGoogleClientSecret(e.target.value)}
                            placeholder="Client Secret"
                            className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus w-full pr-10" />
                          <button type="button" onClick={() => setShowGoogleSecret(!showGoogleSecret)}
                            className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-transparent border-none cursor-pointer">
                            {showGoogleSecret ? <EyeOff size={16} /> : <Eye size={16} />}
                          </button>
                        </div>
                      </div>
                      <div>
                        <Label className="text-[12px] font-medium text-[#94A3B8]">Redirect URL</Label>
                        <Input value={googleRedirectUrl} onChange={(e) => setGoogleRedirectUrl(e.target.value)}
                          placeholder={`https://seu-dominio.com/${appName || "meu-app"}/auth/google/callback`}
                          className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                        <p className="text-[11px] text-[#64748B] mt-1">
                          Configure este URL no Google Cloud Console
                        </p>
                      </div>
                      <div>
                        <Label className="text-[12px] font-medium text-[#94A3B8]">Domínios permitidos</Label>
                        <Input value={googleAllowedDomains} onChange={(e) => setGoogleAllowedDomains(e.target.value)}
                          placeholder="zeeplabs.com, zeepfly.com"
                          className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                        <p className="text-[11px] text-[#64748B] mt-1">
                          Separados por vírgula. Vazio = qualquer domínio.
                        </p>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </TabsContent>

            <TabsContent value="storage" className="mt-0">
              <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
                <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">
                  Storage (S3)
                </h3>
                <div className="flex items-center justify-between">
                  <div className="flex flex-col gap-0.5">
                    <p className="text-sm font-semibold text-[#F8FAFC]">Ativar S3</p>
                    <p className="text-xs text-[#94A3B8]">
                      Conecte um bucket S3-compatible (DO Spaces, Magalu, AWS, MinIO)
                    </p>
                  </div>
                  <Switch checked={storageEnabled} onCheckedChange={setStorageEnabled} className="shrink-0" />
                </div>
                {storageEnabled && (
                  <div className="flex flex-col gap-3 border-t border-white/[0.06] pt-3">
                    <div>
                      <Label className="text-[12px] font-medium text-[#94A3B8]">Bucket</Label>
                      <Input value={storageBucket} onChange={(e) => setStorageBucket(e.target.value)}
                        placeholder="meu-bucket"
                        className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                    </div>
                    <div>
                      <Label className="text-[12px] font-medium text-[#94A3B8]">Região</Label>
                      <Input value={storageRegion} onChange={(e) => setStorageRegion(e.target.value)}
                        placeholder="us-east-1"
                        className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                    </div>
                    <div>
                      <Label className="text-[12px] font-medium text-[#94A3B8]">Endpoint</Label>
                      <Input value={storageEndpoint} onChange={(e) => setStorageEndpoint(e.target.value)}
                        placeholder="https://nyc3.digitaloceanspaces.com"
                        className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                    </div>
                    <div>
                      <Label className="text-[12px] font-medium text-[#94A3B8]">Access Key ID</Label>
                      <Input value={storageAccessKey} onChange={(e) => setStorageAccessKey(e.target.value)}
                        placeholder="DO00XXXXXXXXXXXX"
                        className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
                    </div>
                    <div>
                      <Label className="text-[12px] font-medium text-[#94A3B8]">Secret Access Key</Label>
                      <div className="relative mt-1">
                        <Input type={showStorageSecret ? "text" : "password"} value={storageSecretKey}
                          onChange={(e) => setStorageSecretKey(e.target.value)}
                          placeholder="Secret Key"
                          className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus w-full pr-10" />
                        <button type="button" onClick={() => setShowStorageSecret(!showStorageSecret)}
                          className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-transparent border-none cursor-pointer">
                          {showStorageSecret ? <EyeOff size={16} /> : <Eye size={16} />}
                        </button>
                      </div>
                    </div>
                    <p className="text-[11px] text-[#94A3B8]">
                      Os arquivos ficarão disponíveis em <code className="text-[#B3D1FF]">/{appName || "meu-app"}/files/*</code> via API.
                    </p>
                  </div>
                )}
              </div>
             </TabsContent>

            <TabsContent value="api" className="mt-0 flex flex-col gap-6">

              {isEdit && (
                <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
                  <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">
                    {t("appForm.apiBaseUrl.title")}
                  </h3>
                  <div className="flex flex-col gap-3">
                    <p className="text-xs text-[#94A3B8]">
                      {t("appForm.apiBaseUrl.desc")}
                    </p>
                    <div className="flex items-center gap-2 bg-black/30 rounded-xl px-4 py-3">
                      <code className="text-sm text-[#B3D1FF] break-all font-mono">
                        {window.location.origin}/{appName}
                      </code>
                      <button
                        type="button"
                        onClick={() => navigator.clipboard.writeText(`${window.location.origin}/${appName}`)}
                        className="shrink-0 p-1.5 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors"
                      >
                        <Copy size={14} />
                      </button>
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <p className="text-[11px] text-[#64748B] font-medium">
                        {t("appForm.apiBaseUrl.examples")}
                      </p>
                      <code className="text-[11px] text-[#64748B] font-mono">
                        {t("appForm.apiBaseUrl.register", { name: appName })}
                      </code>
                      <code className="text-[11px] text-[#64748B] font-mono">
                        {t("appForm.apiBaseUrl.login", { name: appName })}
                      </code>
                      <code className="text-[11px] text-[#64748B] font-mono">
                        {t("appForm.apiBaseUrl.list", { name: appName })}
                      </code>
                      <code className="text-[11px] text-[#64748B] font-mono">
                        {t("appForm.apiBaseUrl.create", { name: appName })}
                      </code>
                      <code className="text-[11px] text-[#64748B] font-mono">
                        {t("appForm.apiBaseUrl.health", { name: appName })}
                      </code>
                    </div>
                  </div>
                </div>
              )}

              <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
                <h3 className="text-[13px] font-semibold text-[#94A3B8] uppercase tracking-wider">
                  API
                </h3>

                <div className="flex flex-col gap-3">
                  <div className="flex items-center justify-between">
                    <div className="flex flex-col gap-0.5">
                      <p className="text-sm font-semibold text-[#F8FAFC]">
                        Rate Limit
                      </p>
                      <p className="text-xs text-[#94A3B8]">
                        Limitar requisições por minuto por IP para este app
                      </p>
                    </div>
                    <Switch checked={rateLimitEnabled} onCheckedChange={setRateLimitEnabled} className="shrink-0" />
                  </div>
                  {rateLimitEnabled && (
                    <div className="flex flex-col gap-2 border-t border-white/[0.06] pt-3">
                      <Label className="text-[12px] font-medium text-[#94A3B8]">
                        Requests per minute
                      </Label>
                      <Input
                        type="number"
                        min={1}
                        max={10000}
                        value={rateLimitRPM}
                        onChange={(e) => setRateLimitRPM(parseInt(e.target.value) || 60)}
                        className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus w-32"
                      />
                      <p className="text-[11px] text-[#94A3B8]">
                        Máximo de requisições por IP a cada 60 segundos
                      </p>
                    </div>
                  )}
                </div>
              </div>
            </TabsContent>
          </Tabs>

          {/* Submit error */}
          {submitError && (
            <div className="px-4 py-3 rounded-xl bg-red-500/[0.08] border border-red-500/[0.20] text-red-400 text-[13px]">
              {submitError}
            </div>
          )}

          {/* Submit button */}
          <div className="flex items-center gap-3">
            <Button
              type="button"
              variant="outline"
              onClick={() => navigate("/apps")}
              className="rounded-xl px-5 py-2.5 text-sm font-semibold border-white/[0.10] bg-transparent text-[#94A3B8] hover:bg-white/[0.05] hover:text-white"
            >
              Cancelar
            </Button>
            <Button
              type="submit"
              disabled={isMutating}
              className={cn(
                "rounded-xl px-6 py-2.5 text-sm font-bold text-white border-none",
                isMutating
                  ? "cursor-not-allowed opacity-50"
                  : "cursor-pointer hover:opacity-90",
              )}
              style={{
                background: isMutating
                  ? 'rgba(var(--brand-primary-rgb), 0.5)'
                  : 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
              }}
            >
              {isMutating
                ? "Salvando..."
                : isEdit
                  ? "Salvar Alterações"
                  : "Criar App"}
            </Button>
          </div>
        </form>
      )}
    </motion.div>
  );
}
