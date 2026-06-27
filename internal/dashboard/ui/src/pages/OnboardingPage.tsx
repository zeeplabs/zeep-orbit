import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useBootstrap } from "../lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

export interface OnboardingPageProps {
  onComplete: () => void;
}

type Step = "welcome" | "create-superadmin" | "done";

const EASE = [0.32, 0.72, 0, 1] as const;
const DURATION = 0.5;

const stepVariants = {
  enter: (direction: number) => ({
    x: direction > 0 ? 60 : -60,
    opacity: 0,
  }),
  center: {
    x: 0,
    opacity: 1,
  },
  exit: (direction: number) => ({
    x: direction > 0 ? -60 : 60,
    opacity: 0,
  }),
};

const STEPS: Step[] = ["welcome", "create-superadmin", "done"];

function StepDots({ current }: { current: Step }) {
  const idx = STEPS.indexOf(current);
  return (
    <div className="flex gap-2 justify-center mb-8">
      {STEPS.map((_, i) => (
        <div
          key={i}
          className={cn(
            "h-2 rounded-full transition-all",
            i === idx
              ? "w-5 bg-[#0347A5]"
              : i < idx
                ? "w-2 bg-[#0347A5]/40"
                : "w-2 bg-white/15"
          )}
          style={{ transitionDuration: `${DURATION}s`, transitionTimingFunction: `cubic-bezier(${EASE.join(",")})` }}
        />
      ))}
    </div>
  );
}

function WelcomeStep({ onNext }: { onNext: () => void }) {
  return (
    <div className="text-center">
      <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-[#0347A5] to-[#7C3AED] flex items-center justify-center mx-auto mb-6 text-[28px]">
        ⚡
      </div>
      <h1 className="text-[26px] font-bold mb-3 tracking-tight leading-tight text-[#F8FAFC]">
        Bem-vindo ao ZeepCore
      </h1>
      <p className="text-[#94A3B8] text-sm leading-relaxed mb-10 max-w-[340px] mx-auto">
        Sua plataforma Backend As A Service está pronta para ser configurada.
        Vamos criar o administrador para liberar acesso ao dashboard.
      </p>
      <Button
        onClick={onNext}
        className="bg-gradient-to-br from-[#0347A5] to-[#7C3AED] border-0 text-white font-semibold px-8 py-[13px] h-auto rounded-lg hover:opacity-90 transition-opacity"
      >
        Começar configuração
      </Button>
    </div>
  );
}

function CreateSuperadminStep({ onSuccess }: { onSuccess: () => void }) {
  const [secret, setSecret] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [validationError, setValidationError] = useState("");

  const bootstrap = useBootstrap();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setValidationError("");

    if (password.length < 12) {
      setValidationError("A senha deve ter no mínimo 12 caracteres.");
      return;
    }
    if (password !== confirmPassword) {
      setValidationError("As senhas não coincidem.");
      return;
    }

    bootstrap.mutate(
      { secret, email, password },
      {
        onSuccess: () => onSuccess(),
        onError: (err) => setValidationError(err.message),
      },
    );
  };

  const error =
    validationError || (bootstrap.isError ? bootstrap.error?.message : "");

  return (
    <div>
      <h2 className="text-xl font-bold mb-1.5 tracking-tight text-[#F8FAFC]">
        Criar superadmin
      </h2>
      <p className="text-[#94A3B8] text-[13px] mb-7 leading-relaxed">
        Informe o secret de bootstrap e as credenciais da conta de
        administrador.
      </p>
      <form onSubmit={handleSubmit} className="flex flex-col gap-[18px]">
        <div className="flex flex-col gap-1.5">
          <Label className="text-[11px] font-semibold text-white/50 uppercase tracking-[0.06em]">
            Bootstrap Secret
          </Label>
          <Input
            type="password"
            placeholder="••••••••••••"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            required
            autoComplete="off"
            className="bg-white/[0.06] border-white/10 text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/50 focus-visible:border-[#0347A5]/50 rounded-lg h-auto px-4 py-3 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label className="text-[11px] font-semibold text-white/50 uppercase tracking-[0.06em]">
            Email
          </Label>
          <Input
            type="email"
            placeholder="admin@exemplo.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoComplete="email"
            className="bg-white/[0.06] border-white/10 text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/50 focus-visible:border-[#0347A5]/50 rounded-lg h-auto px-4 py-3 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label className="text-[11px] font-semibold text-white/50 uppercase tracking-[0.06em]">
            Senha
          </Label>
          <Input
            type="password"
            placeholder="Mínimo 12 caracteres"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            autoComplete="new-password"
            className="bg-white/[0.06] border-white/10 text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/50 focus-visible:border-[#0347A5]/50 rounded-lg h-auto px-4 py-3 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label className="text-[11px] font-semibold text-white/50 uppercase tracking-[0.06em]">
            Confirmar Senha
          </Label>
          <Input
            type="password"
            placeholder="Repita a senha"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            required
            autoComplete="new-password"
            className="bg-white/[0.06] border-white/10 text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/50 focus-visible:border-[#0347A5]/50 rounded-lg h-auto px-4 py-3 text-sm"
          />
        </div>
        {error && (
          <p className="text-red-400 text-[13px] m-0 bg-red-500/[0.08] border border-red-500/20 rounded-md px-3 py-2.5">
            {error}
          </p>
        )}
        <Button
          type="submit"
          disabled={bootstrap.isPending}
          className="bg-gradient-to-br from-[#0347A5] to-[#7C3AED] border-0 text-white font-semibold px-6 py-[13px] h-auto rounded-lg mt-1 hover:opacity-90 transition-opacity disabled:opacity-65"
        >
          {bootstrap.isPending ? "Criando conta..." : "Criar superadmin"}
        </Button>
      </form>
    </div>
  );
}

function DoneStep({ onComplete }: { onComplete: () => void }) {
  return (
    <div className="text-center">
      <div className="w-16 h-16 rounded-full bg-green-500/15 border border-green-500/30 flex items-center justify-center mx-auto mb-6 text-[28px]">
        ✓
      </div>
      <h2 className="text-[22px] font-bold mb-2.5 tracking-tight text-white">
        Tudo pronto!
      </h2>
      <p className="text-[#94A3B8] text-sm leading-relaxed mb-9">
        O superadmin foi criado com sucesso. Agora você pode entrar no dashboard
        com as credenciais configuradas.
      </p>
      <Button
        onClick={onComplete}
        className="bg-gradient-to-br from-[#0347A5] to-[#7C3AED] border-0 text-white font-semibold px-8 py-[13px] h-auto rounded-lg hover:opacity-90 transition-opacity"
      >
        Ir para o login
      </Button>
    </div>
  );
}

export default function OnboardingPage({ onComplete }: OnboardingPageProps) {
  const [step, setStep] = useState<Step>("welcome");
  const [direction, setDirection] = useState(1);

  const goTo = (next: Step) => {
    const currentIdx = STEPS.indexOf(step);
    const nextIdx = STEPS.indexOf(next);
    setDirection(nextIdx > currentIdx ? 1 : -1);
    setStep(next);
  };

  return (
    <div
      className="min-h-screen flex items-center justify-center p-4"
      style={{
        background:
          "radial-gradient(ellipse 60% 50% at 20% 60%, rgba(59,130,246,0.12) 0%, transparent 60%), radial-gradient(ellipse 50% 40% at 80% 20%, rgba(124,58,237,0.12) 0%, transparent 60%), #0A0A0F",
      }}
    >
      {/* Outer bezel */}
      <div className="w-[min(480px,calc(100vw-2rem))] rounded-[20px] border border-white/[0.08] p-0.5 bg-white/[0.03]">
        {/* Inner bezel — glass content */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: DURATION, ease: EASE }}
          className="rounded-[18px] border border-white/[0.06] bg-white/[0.04] px-10 pt-10 pb-11 overflow-hidden"
        >
          <StepDots current={step} />

          <AnimatePresence mode="wait" custom={direction}>
            <motion.div
              key={step}
              custom={direction}
              variants={stepVariants}
              initial="enter"
              animate="center"
              exit="exit"
              transition={{ duration: DURATION, ease: EASE }}
            >
              {step === "welcome" && (
                <WelcomeStep onNext={() => goTo("create-superadmin")} />
              )}
              {step === "create-superadmin" && (
                <CreateSuperadminStep onSuccess={() => goTo("done")} />
              )}
              {step === "done" && <DoneStep onComplete={onComplete} />}
            </motion.div>
          </AnimatePresence>
        </motion.div>
      </div>
    </div>
  );
}
