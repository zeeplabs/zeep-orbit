import { useState } from "react";
import { Navigate, NavLink, Outlet } from "react-router-dom";
import { motion } from "framer-motion";
import { LogOut, Grid, Database, Users, Activity, Settings } from "lucide-react";
import { useQueryClient, useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import logoType from "@/assets/images/logo/logotype.svg";

interface User {
  id: string;
  email: string;
  role: string;
}

type NavItem = { icon: typeof Grid; label: string; path: string };

function navItems(user: User | null): NavItem[] {
  const items: NavItem[] = [
    { icon: Grid, label: "Apps", path: "/apps" },
    { icon: Database, label: "Data Browser", path: "/data-browser" },
    { icon: Activity, label: "Logs", path: "/logs" },
    { icon: Settings, label: "Aparência", path: "/configuracoes" },
  ];
  if (user?.role === "superadmin") {
    items.splice(2, 0, { icon: Users, label: "Usuários", path: "/usuarios" });
  }
  return items;
}

export default function DashboardShell({ user }: { user: User | null }) {
  const qc = useQueryClient();
  const [showLogoutDialog, setShowLogoutDialog] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);

  const { data: brandConfig } = useQuery({
    queryKey: ["brand-config"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/config");
      return res.json() as Promise<{ theme: string; company_name: string }>;
    },
    staleTime: 30000,
  });

  const companyName = brandConfig?.company_name || "Orbit";

  if (!user) return <Navigate to="/login" replace />;

  const handleLogout = async () => {
    setLoggingOut(true);
    try {
      await fetch("/dashboard/api/logout", {
        method: "POST",
        credentials: "include",
      });
      qc.invalidateQueries({ queryKey: ["me"] });
    } finally {
      setLoggingOut(false);
      setShowLogoutDialog(false);
    }
  };

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "240px 1fr",
        minHeight: "100vh",
        background:
          "radial-gradient(ellipse at 20% 50%, rgba(var(--brand-primary-rgb),0.15) 0%, transparent 50%), radial-gradient(ellipse at 80% 20%, rgba(var(--brand-secondary-rgb),0.15) 0%, transparent 50%), var(--bg)",
      }}
    >
      {/* Sidebar */}
      <motion.aside
        initial={{ x: -20, opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
        style={{
          position: "sticky",
          top: 0,
          height: "100vh",
          borderRight: "1px solid rgba(255,255,255,0.06)",
          padding: "24px 12px",
          display: "flex",
          flexDirection: "column",
        }}
      >
        {/* Logo */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "0 8px",
            marginBottom: 32,
          }}
        >
          <img
            src={logoType}
            alt="Zeep Orbit"
            style={{
              width: 42,
              height: 42,
              borderRadius: 8,
              border: "1px solid rgba(255,255,255,0.10)",
              objectFit: "cover",
            }}
          />
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <span
              style={{
              fontSize: 16,
              fontWeight: 700,
              lineHeight: 1.3,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {companyName}
            </span>
            <p
              style={{
                fontSize: 12,
                fontWeight: 400,
                lineHeight: 1,
                color: "var(--text-muted)",
              }}
            >
              BaaS Platform Manager
            </p>
          </div>
        </div>

        {/* Nav */}
        <nav
          style={{ flex: 1, display: "flex", flexDirection: "column", gap: 2 }}
        >
          {navItems(user).map(({ icon: Icon, label, path }) => (
            <NavLink
              key={path}
              to={path}
              end={path === "/apps"}
              style={({ isActive }) => ({
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: "9px 12px",
                borderRadius: 10,
                border: "none",
                background: isActive ? "rgba(var(--brand-primary-rgb), 0.12)" : "transparent",
                color: isActive ? "var(--text)" : "var(--text-muted)",
                cursor: "pointer",
                fontSize: 14,
                textAlign: "left",
                width: "100%",
                fontFamily: "inherit",
                fontWeight: isActive ? 600 : 400,
                position: "relative",
                textDecoration: "none",
                transition: "background 0.15s, color 0.15s",
              })}
            >
              {({ isActive }) => (
                <>
                  {isActive && (
                    <motion.div
                      layoutId="nav-active-indicator"
                      style={{
                        position: "absolute",
                        left: 0,
                        top: "20%",
                        bottom: "20%",
                        width: 3,
                        borderRadius: 2,
                        background: "var(--accent)",
                      }}
                      transition={{ duration: 0.3, ease: [0.32, 0.72, 0, 1] }}
                    />
                  )}
                  <Icon size={15} strokeWidth={1.5} />
                  {label}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        {/* User */}
        <div
          style={{
            borderTop: "1px solid rgba(255,255,255,0.06)",
            paddingTop: 14,
          }}
        >
          <div style={{ padding: "0 8px", marginBottom: 10 }}>
            <p
              style={{
                fontSize: 13,
                fontWeight: 600,
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
            >
              {user.email}
            </p>
            <p
              style={{ fontSize: 11, color: "var(--text-muted)", marginTop: 2 }}
            >
              {user.role}
            </p>
          </div>
          <button
            onClick={() => setShowLogoutDialog(true)}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              padding: "8px 12px",
              borderRadius: 10,
              border: "none",
              background: "transparent",
              color: "var(--text-muted)",
              cursor: "pointer",
              fontSize: 13,
              width: "100%",
              fontFamily: "inherit",
              transition: "color 0.15s",
            }}
          >
            <LogOut size={14} strokeWidth={1.5} /> Sair
          </button>
        </div>
      </motion.aside>

      {/* Main content — centered independently */}
      <main
        style={{
          display: "flex",
          justifyContent: "center",
          minHeight: "100vh",
        }}
      >
        <div style={{ width: "100%", maxWidth: 1100, padding: 40 }}>
          <Outlet />
        </div>
      </main>

      {/* Logout confirmation dialog */}
      <Dialog
        open={showLogoutDialog}
        onOpenChange={(open) => {
          if (!open) setShowLogoutDialog(false);
        }}
      >
        <DialogContent className="max-w-[380px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0" style={{ boxShadow: '0 0 40px rgba(var(--brand-primary-rgb), 0.10)' }}>
          <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
            <DialogHeader className="mb-0">
              <div className="w-11 h-11 rounded-xl bg-white/[0.08] border border-white/[0.10] flex items-center justify-center mb-[18px]">
                <LogOut
                  size={18}
                  strokeWidth={1.5}
                  className="text-[#94A3B8]"
                />
              </div>
              <DialogTitle className="text-base font-bold text-[#F8FAFC] mb-2">
                Sair do dashboard?
              </DialogTitle>
              <DialogDescription className="text-[13px] text-[#94A3B8] leading-relaxed mb-6">
                Você será desconectado e precisará fazer login novamente.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter className="flex flex-row gap-2.5 sm:flex-row sm:justify-start sm:space-x-0">
              <Button
                variant="outline"
                onClick={() => setShowLogoutDialog(false)}
                disabled={loggingOut}
                className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
              >
                Cancelar
              </Button>
              <Button
                onClick={handleLogout}
                disabled={loggingOut}
                className="flex-1 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
                style={{
                  background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
                }}
              >
                {loggingOut ? "Saindo..." : "Sair"}
              </Button>
            </DialogFooter>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
