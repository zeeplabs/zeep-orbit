import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { motion } from "framer-motion";
import { ArrowLeft } from "lucide-react";
import { useCreateApp } from "../lib/api";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";

function validateName(name: string): string | null {
  if (!name.trim()) return "Nome obrigatório";
  if (!/^[a-z][a-z0-9_-]*$/.test(name))
    return "Apenas letras minúsculas, números, hífen e _ (máx 32), começando com letra";
  if (name.length > 32) return "Máximo de 32 caracteres";
  return null;
}

export default function AppOnboardingPage() {
  const navigate = useNavigate();
  const createApp = useCreateApp();

  const [appName, setAppName] = useState("");
  const [authEmail, setAuthEmail] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const err = validateName(appName);
    setNameError(err);
    if (err) return;

    setSubmitError(null);
    try {
      const app = await createApp.mutateAsync({ name: appName, auth_email_enabled: authEmail });
      navigate(`/apps/${app.id}`);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Erro inesperado ao criar o app");
    }
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
          NEW APP
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">Criar Aplicativo</h2>
        <p className="mt-1 text-sm text-[#94A3B8]">
          Dê um nome ao seu app pra começar. Tabelas, colunas, login e storage você configura na
          próxima tela, salvando um pedaço de cada vez.
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-6">
        <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-5">
          <div className="flex flex-col gap-2">
            <Label className="text-[13px] font-semibold text-[#94A3B8]">Nome do App</Label>
            <Input
              value={appName}
              onChange={(e) => setAppName(e.target.value.toLowerCase().replace(/[\s]+/g, "-"))}
              placeholder="my-app"
              className={
                "h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus" +
                (nameError ? " border-red-500/50" : "")
              }
            />
            {nameError && <p className="text-xs text-red-400">{nameError}</p>}
            <p className="text-[11px] text-[#94A3B8]">
              Letras minúsculas, números, hífen e _. Máx 32 caracteres, começando com letra.
            </p>
          </div>

          <div className="border-t border-white/[0.06]" />

          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-0.5">
              <p className="text-sm font-semibold text-[#F8FAFC]">Autenticação por e-mail</p>
              <p className="text-xs text-[#94A3B8]">
                Habilita registro de usuários e login via email/senha em seu app. Necessária se
                alguma tabela precisar de acesso restrito (RLS).
              </p>
            </div>
            <Switch checked={authEmail} onCheckedChange={setAuthEmail} className="shrink-0" />
          </div>
        </div>

        {submitError && (
          <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-4 text-sm text-red-400">
            {submitError}
          </div>
        )}

        <div className="flex items-center gap-3">
          <button
            type="submit"
            disabled={createApp.isPending}
            className="text-[13px] font-semibold px-5 py-2.5 rounded-full text-white cursor-pointer disabled:opacity-50"
            style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}
          >
            {createApp.isPending ? "Criando..." : "Criar App"}
          </button>
          <button
            type="button"
            onClick={() => navigate("/apps")}
            className="text-[13px] font-medium px-5 py-2.5 rounded-full border border-white/[0.10] bg-transparent text-[#94A3B8] cursor-pointer hover:bg-white/[0.06]"
          >
            Cancelar
          </button>
        </div>
      </form>
    </motion.div>
  );
}
