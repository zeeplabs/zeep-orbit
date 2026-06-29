import { useState } from "react";
import { motion } from "framer-motion";
import { useQueryClient, useQuery } from "@tanstack/react-query";
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

  const { data: config } = useQuery({
    queryKey: ["brand-config"],
    queryFn: () =>
      fetch("/dashboard/api/config")
        .then((r) => r.json())
        .then((d) => ({
          googleOAuthEnabled: d.google_oauth_enabled === true,
        })),
    staleTime: 60000,
  });

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

      qc.clear();
      window.location.href = "/dashboard/apps";
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
        className="w-full max-w-[400px] mx-4 border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-8 max-md:p-6"
        style={{ boxShadow: "0 0 40px rgba(var(--brand-primary-rgb), 0.10)" }}
      >
        {/* Header */}
        <div className="flex flex-col items-center mb-8">
          <img
            src={logo}
            alt="Zeep Orbit"
            className="size-42 max-md:size-32 object-contain mb-3"
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
                ? "linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.5), rgba(var(--brand-secondary-rgb), 0.5))"
                : "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
            }}
          >
            {loading ? "Accessing..." : "Access"}
          </Button>
        </form>

        {config?.googleOAuthEnabled && (
          <>
            <div className="flex items-center gap-3 my-5">
              <div className="flex-1 h-px bg-white/[0.08]" />
              <span className="text-[12px] text-white/30 font-medium uppercase tracking-wider">
                or
              </span>
              <div className="flex-1 h-px bg-white/[0.08]" />
            </div>

            <a
              href="/dashboard/api/auth/google/login"
              className="flex items-center justify-center gap-3 h-10 rounded-lg bg-white/[0.05] border border-white/[0.10] text-sm text-[#F8FAFC] font-medium no-underline hover:bg-white/[0.10] transition-colors"
            >
              <svg width="18" height="18" viewBox="0 0 24 24">
                <path
                  fill="#4285F4"
                  d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 01-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
                />
                <path
                  fill="#34A853"
                  d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                />
                <path
                  fill="#FBBC05"
                  d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
                />
                <path
                  fill="#EA4335"
                  d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
                />
              </svg>
              Sign in with Google
            </a>
          </>
        )}
      </motion.div>

      <p className="absolute bottom-6 text-[13px] text-white/20">
        v{pkg.version}
      </p>
    </div>
  );
}
