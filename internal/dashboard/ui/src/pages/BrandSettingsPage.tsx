import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { Palette, Save, Eye, EyeOff, CheckCircle, Loader2, Globe } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { THEMES, BrandTheme, applyTheme } from "../lib/themes";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";

const EASE = [0.32, 0.72, 0, 1] as const;

interface BrandConfig {
  theme: string;
  company_name: string;
  logo_url: string;
}

export default function BrandSettingsPage() {
  const [config, setConfig] = useState<BrandConfig | null>(null);
  const [companyName, setCompanyName] = useState("");
  const [selectedTheme, setSelectedTheme] = useState("azure");
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const qc = useQueryClient();

  useEffect(() => {
    fetch("/dashboard/api/config", { credentials: "include" })
      .then((res) => res.json())
      .then((data) => {
        setConfig(data);
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
        body: JSON.stringify({
          theme: selectedTheme,
          company_name: companyName,
          logo_url: "",
        }),
      });
      if (!res.ok) {
        const data = await res.json();
        setMessage(data.error || "Erro ao salvar");
        return;
      }
      const updated = await res.json();
      setConfig(updated);
      applyTheme(THEMES[selectedTheme] || THEMES.azure);
      qc.invalidateQueries({ queryKey: ["brand-config"] });
      setMessage("Configurações salvas!");
    } catch {
      setMessage("Erro de conexão");
    } finally {
      setSaving(false);
    }
  };

  const currentTheme = THEMES[selectedTheme] || THEMES.azure;

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: EASE }}
    >
      {/* Header */}
      <div className="mb-8">
        <span
          className="mb-3 inline-block rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          CONFIGURAÇÕES
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">Aparência</h2>
        <p className="mt-1 text-sm text-[#94A3B8]">
          Personalize as cores e nome da plataforma
        </p>
      </div>

      <div className="flex flex-col gap-6 w-full">
        {/* Company Name */}
        <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label className="text-[13px] font-semibold text-[#94A3B8]">
              Nome da Empresa
            </Label>
            <Input
              value={companyName}
              onChange={(e) => setCompanyName(e.target.value)}
              placeholder="Zeep Tecnologia"
              className="bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus h-10"
            />
            <p className="text-[11px] text-[#94A3B8]">
              Exibido no sidebar e outras áreas do dashboard.
            </p>
          </div>
        </div>

        {/* Theme Selector */}
        <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
          <div className="flex items-center gap-2">
            <Palette size={16} strokeWidth={1.5} className="text-[#94A3B8]" />
            <Label className="text-[13px] font-semibold text-[#94A3B8]">
              Tema de Cores
            </Label>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            {Object.entries(THEMES).map(([key, theme]) => (
              <ThemeCard
                key={key}
                themeKey={key}
                theme={theme}
                selected={selectedTheme === key}
                onClick={() => setSelectedTheme(key)}
              />
            ))}
          </div>
        </div>

        {/* Preview */}
        <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-4">
          <Label className="text-[13px] font-semibold text-[#94A3B8]">
            Prévia
          </Label>
          <div
            className="rounded-xl border p-4 flex items-center gap-3"
            style={{
              borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
              backgroundColor: "rgba(var(--brand-primary-rgb), 0.06)",
            }}
          >
            <div
              className="size-9 rounded-lg flex items-center justify-center text-white text-sm font-bold"
              style={{
                background: `linear-gradient(135deg, ${currentTheme.primary}, ${currentTheme.secondary})`,
              }}
            >
              Z
            </div>
            <div>
              <p
                className="text-sm font-bold"
                style={{ color: currentTheme.primary }}
              >
                {companyName || "Zeep Tecnologia"}
              </p>
              <p className="text-[11px] text-[#94A3B8]">
                Botão{" "}
                <span
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-semibold text-white"
                  style={{
                    background: `linear-gradient(135deg, ${currentTheme.primary}, ${currentTheme.secondary})`,
                  }}
                >
                  Exemplo
                </span>
              </p>
            </div>
          </div>
        </div>

        {/* Save */}
        <div className="flex items-center gap-3">
          <Button
            onClick={handleSave}
            disabled={saving}
            className="gap-2 rounded-xl px-6 py-2.5 text-sm font-bold text-white border-0"
            style={{
              background: saving
                ? "rgba(var(--brand-primary-rgb), 0.5)"
                : "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
            }}
          >
            <Save size={14} strokeWidth={1.5} />
            {saving ? "Salvando..." : "Salvar"}
          </Button>

          {message && (
            <span
              className={cn(
                "text-sm",
                message === "Configurações salvas!"
                  ? "text-green-400"
                  : "text-red-400",
              )}
            >
              {message}
            </span>
          )}
        </div>
      </div>

      {/* ── Google OAuth Config ── */}
      <div className="mt-10">
        <span className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{ borderColor: 'rgba(var(--brand-primary-rgb), 0.2)', backgroundColor: 'rgba(var(--brand-primary-rgb), 0.12)', color: 'var(--brand-light)' }}
        >
          <Globe size={12} strokeWidth={1.5} />
          Google OAuth
        </span>
        <GoogleAuthProviderCard />
      </div>
    </motion.div>
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
      .catch(() => setMessage("Erro ao carregar config"))
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
      if (!res.ok) { const err = await res.json(); throw new Error(err.error || "Erro ao salvar"); }
      const result = await res.json();
      setConfig(result as typeof config);
      setClientSecret("");
      setMessage("Configuração salva com sucesso");
      setMessageType("success");
    } catch (err) {
      setMessage((err as Error).message);
      setMessageType("error");
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="text-[13px] text-[#94A3B8]">Carregando...</p>;

  const inputClass = "h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] outline-none brand-focus w-full";

  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-6 mt-4">
      <div className="flex items-center justify-between mb-6">
        <div>
          <span className="text-[15px] font-bold text-[#F8FAFC]">Google</span>
          <p className="text-[12px] text-[#94A3B8] mt-0.5">Login via Google (credenciais criptografadas AES-256-GCM)</p>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[12px] text-[#94A3B8]">Ativo</span>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </div>

      {enabled && (
        <div className="flex flex-col gap-4 max-w-lg">
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">Client ID</label>
            <Input value={clientId} onChange={(e) => setClientId(e.target.value)} placeholder="Google OAuth Client ID" className={inputClass} />
          </div>
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">Client Secret</label>
            <div className="relative">
              <Input type={showSecret ? "text" : "password"} value={clientSecret} onChange={(e) => setClientSecret(e.target.value)}
                placeholder={config?.config_set ? "•••••••• (vazio = mantém atual)" : "Client Secret"} className={inputClass + " pr-10"} />
              <button type="button" onClick={() => setShowSecret(!showSecret)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-none border-none cursor-pointer">
                {showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
          </div>
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">Redirect URL</label>
            <Input value={redirectUrl} onChange={(e) => setRedirectUrl(e.target.value)}
              placeholder="https://orbit.zeeplabs.com/dashboard/api/auth/google/callback" className={inputClass} />
          </div>
          <div>
            <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">Domínios permitidos</label>
            <Input value={allowedDomains} onChange={(e) => setAllowedDomains(e.target.value)} placeholder="zeeplabs.com, zeepfly.com" className={inputClass} />
            <p className="mt-1 text-[11px] text-[#64748B]">Separados por vírgula. Vazio = qualquer domínio.</p>
          </div>
        </div>
      )}

      {message && <p className={`mt-4 text-[12px] ${messageType === "success" ? "text-green-400" : "text-red-400"}`}>{message}</p>}

      <Button onClick={handleSave} disabled={saving}
        className="mt-5 gap-2 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
        style={{ background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))' }}>
        {saving ? <><Loader2 size={14} className="animate-spin" /> Salvando...</> : <><Save size={14} /> Salvar</>}
      </Button>
    </div>
  );
}
