import { useState } from "react";
import { motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Eye, EyeOff, Loader2, Check } from "lucide-react";
import logo from "@/assets/images/logo/logo.svg";

export default function GoogleSetupPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [done, setDone] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");

    if (password !== confirmPassword) {
      setError(t("googleSetup.passwordsDontMatch", "Passwords do not match"));
      return;
    }

    setLoading(true);
    try {
      const res = await fetch("/dashboard/api/me/google-setup", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, password, confirm_password: confirmPassword }),
      });

      if (!res.ok) {
        const data = await res.json();
        setError(data.error || t("common.errorSaving"));
        return;
      }

      setDone(true);
      qc.invalidateQueries({ queryKey: ["me"] });
      qc.clear();
      setTimeout(() => {
        window.location.href = "/dashboard/apps";
      }, 1500);
    } catch {
      setError(t("common.connectionError"));
    } finally {
      setLoading(false);
    }
  };

  const inputClass =
    "h-10 rounded-lg bg-white/[0.05] border border-white/[0.10] px-4 py-2.5 text-sm text-[#F8FAFC] placeholder:text-white/30 outline-none brand-focus transition-colors";

  if (done) {
    return (
      <div className="flex min-h-screen items-center justify-center relative" style={{
        background: "radial-gradient(ellipse at 50% 50%, rgba(var(--brand-primary-rgb),0.15) 0%, transparent 50%), var(--bg)",
      }}>
        <motion.div
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          className="flex flex-col items-center gap-4 text-[#F8FAFC]"
        >
          <div className="size-14 rounded-full bg-green-500/20 flex items-center justify-center">
            <Check size={28} className="text-green-400" />
          </div>
          <p className="text-sm text-[#94A3B8]">{t("googleSetup.redirecting", "Redirecting to dashboard...")}</p>
        </motion.div>
      </div>
    );
  }

  return (
    <div
      className="flex min-h-screen items-center justify-center relative"
      style={{
        background:
          "radial-gradient(ellipse at 20% 50%, rgba(var(--brand-primary-rgb),0.15) 0%, transparent 50%), radial-gradient(ellipse at 80% 20%, rgba(var(--brand-secondary-rgb),0.15) 0%, transparent 50%), var(--bg)",
      }}
    >
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
        className="w-full max-w-[400px] mx-4 border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-8 max-md:p-6"
        style={{ boxShadow: "0 0 40px rgba(var(--brand-primary-rgb), 0.10)" }}
      >
        <div className="flex flex-col items-center mb-8">
          <img src={logo} alt="Zeep Orbit" className="size-42 max-md:size-32 object-contain mb-3" />
          <h1 className="text-lg font-bold text-[#F8FAFC]">
            {t("googleSetup.title", "Complete your account")}
          </h1>
          <p className="text-[13px] text-[#94A3B8] mt-0.5 text-center">
            {t("googleSetup.description", "Set your name and a password as a backup login method in case Google is unavailable.")}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <input
            type="text"
            placeholder={t("googleSetup.name", "Your name")}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className={inputClass}
          />
          <div className="relative">
            <input
              type={showPassword ? "text" : "password"}
              placeholder={t("googleSetup.password", "Password")}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
              className={inputClass + " w-full pr-10"}
            />
            <button
              type="button"
              onClick={() => setShowPassword(!showPassword)}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-white/40 hover:text-white/70 transition-colors"
            >
              {showPassword ? <EyeOff size={18} /> : <Eye size={18} />}
            </button>
          </div>
          <input
            type="password"
            placeholder={t("googleSetup.confirmPassword", "Confirm password")}
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            required
            minLength={8}
            className={inputClass}
          />

          {error && (
            <p className="text-[13px] text-red-400 bg-red-500/[0.08] border border-red-500/[0.20] rounded-lg px-3 py-2">
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={loading}
            className="h-10 rounded-lg text-sm font-bold text-white border-0 mt-1 cursor-pointer hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50 transition-opacity"
            style={{
              background: loading
                ? "linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.5), rgba(var(--brand-secondary-rgb), 0.5))"
                : "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
            }}
          >
            {loading ? (
              <span className="flex items-center justify-center gap-2">
                <Loader2 size={14} className="animate-spin" />
                {t("googleSetup.saving", "Saving...")}
              </span>
            ) : (
              t("googleSetup.save", "Save")
            )}
          </button>
        </form>
      </motion.div>
    </div>
  );
}
