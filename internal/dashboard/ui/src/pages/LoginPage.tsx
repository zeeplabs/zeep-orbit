import { useState } from "react";
import { motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Eye, EyeOff } from "lucide-react";
import logo from "@/assets/images/logo/logo.svg";
import pkg from "../../package.json";

export default function LoginPage() {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      const res = await fetch("/dashboard/api/login", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      if (!res.ok) {
        const data = await res.json();
        setError(data.error || "Invalid credentials");
        return;
      }
      qc.invalidateQueries({ queryKey: ["me"] });
    } catch {
      setError("Connection error");
    } finally {
      setLoading(false);
    }
  };

  const inputClass =
    "h-10 rounded-lg bg-white/[0.05] border border-white/[0.10] px-4 py-2.5 text-sm text-[#F8FAFC] placeholder:text-white/30 outline-none brand-focus transition-colors";

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
        className="w-[400px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-8"
      style={{ boxShadow: '0 0 40px rgba(var(--brand-primary-rgb), 0.10)' }}
      >
        {/* Header */}
        <div className="flex flex-col items-center mb-8">
          <img
            src={logo}
            alt="Zeep Orbit"
            className="size-42 object-contain mb-3"
          />
          <h1 className="text-lg font-bold text-[#F8FAFC]">
            BaaS Platform Manager
          </h1>
          <p className="text-[13px] text-[#94A3B8] mt-0.5">
            Access your account
          </p>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <input
            type="email"
            placeholder="E-mail"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            className={inputClass}
          />
          <div className="relative">
            <input
              type={showPassword ? "text" : "password"}
              placeholder="Password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
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

          {error && (
            <p className="text-[13px] text-red-400 bg-red-500/[0.08] border border-red-500/[0.20] rounded-lg px-3 py-2">
              {error}
            </p>
          )}

          <Button
            type="submit"
            disabled={loading}
            className={cn(
              "h-10 rounded-lg text-sm font-bold text-white border-0 mt-1",
              loading
                ? "cursor-not-allowed opacity-50"
                : "cursor-pointer hover:opacity-90",
            )}
            style={{
              background: loading
                ? 'linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.5), rgba(var(--brand-secondary-rgb), 0.5))'
                : 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
            }}
          >
            {loading ? "Accessing..." : "Access"}
          </Button>
        </form>
      </motion.div>

      <p className="absolute bottom-6 text-[13px] text-white/20">
        v{pkg.version}
      </p>
    </div>
  );
}
