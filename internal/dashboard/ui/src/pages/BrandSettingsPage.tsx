import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { Palette, Save } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { THEMES, BrandTheme, applyTheme } from "../lib/themes";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

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
