import { useState } from "react";
import { Navigate, NavLink, Outlet } from "react-router-dom";
import { motion } from "framer-motion";
import { LogOut, Grid, Database, Users, Activity, Settings, User } from "lucide-react";
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
  ];
  if (user?.role === "superadmin") {
    items.splice(2, 0, { icon: Users, label: "Usuários", path: "/usuarios" });
    items.push({ icon: Settings, label: "Aparência", path: "/configuracoes" });
  }
  return items;
}

function BottomBar({ items, user, onUserClick }: { items: NavItem[]; user: User; onUserClick: () => void }) {
  return (
    <nav
      className="md:hidden fixed bottom-0 left-0 right-0 z-50 flex items-center justify-around"
      style={{
        height: 60,
        borderTop: "1px solid rgba(255,255,255,0.06)",
        background: "rgba(10,10,15,0.88)",
        backdropFilter: "blur(24px)",
        WebkitBackdropFilter: "blur(24px)",
        paddingBottom: "env(safe-area-inset-bottom, 0px)",
      }}
    >
      {items.map(({ icon: Icon, label, path }) => (
        <NavLink
          key={path}
          to={path}
          end={path === "/apps"}
          className="flex flex-col items-center justify-center flex-1 no-underline"
          style={({ isActive }) => ({
            gap: 2,
            padding: "4px 8px",
            color: isActive ? "var(--brand-primary)" : "var(--text-muted)",
            fontSize: 10,
            fontWeight: isActive ? 600 : 400,
            transition: "color 0.15s",
          })}
        >
          {({ isActive }) => (
            <>
              <Icon size={21} strokeWidth={isActive ? 2 : 1.5} />
              <span style={{ fontSize: 10, lineHeight: 1, whiteSpace: "nowrap" }}>
                {label}
              </span>
            </>
          )}
        </NavLink>
      ))}
      <button
        onClick={onUserClick}
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 2,
          padding: "4px 8px",
          color: "var(--text-muted)",
          background: "none",
          border: "none",
          cursor: "pointer",
          fontSize: 10,
          fontFamily: "inherit",
          flex: 1,
        }}
      >
        <User size={21} strokeWidth={1.5} />
        <span style={{ fontSize: 10, lineHeight: 1 }}>Conta</span>
      </button>
    </nav>
  );
}

export default function DashboardShell({ user }: { user: User | null }) {
  const qc = useQueryClient();
  const [showLogoutDialog, setShowLogoutDialog] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [showUserMenu, setShowUserMenu] = useState(false);

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

  const items = navItems(user);

  return (
    <div
      className="grid grid-cols-[240px_1fr] max-md:grid-cols-1"
      style={{
        minHeight: "100vh",
        background:
          "radial-gradient(ellipse at 20% 50%, rgba(var(--brand-primary-rgb),0.15) 0%, transparent 50%), radial-gradient(ellipse at 80% 20%, rgba(var(--brand-secondary-rgb),0.15) 0%, transparent 50%), var(--bg)",
      }}
    >
      {/* Sidebar — hidden on mobile */}
      <motion.aside
        initial={{ x: -20, opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
        className="max-md:hidden flex flex-col"
        style={{
          position: "sticky",
          top: 0,
          height: "100vh",
          borderRight: "1px solid rgba(255,255,255,0.06)",
          padding: "24px 12px",
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
          {items.map(({ icon: Icon, label, path }) => (
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

      {/* Main content */}
      <main
        className="max-md:pb-[65px]"
        style={{
          display: "flex",
          justifyContent: "center",
          minHeight: "100vh",
        }}
      >
        <div
          className="max-md:px-4 max-md:py-4"
          style={{ width: "100%", padding: 40, minWidth: 0 }}
        >
          <Outlet />
        </div>
      </main>

      {/* Mobile bottom bar */}
      <BottomBar items={items} user={user} onUserClick={() => setShowUserMenu(prev => !prev)} />

      {/* Mobile user menu popover */}
      {showUserMenu && (
        <>
          <div
            onClick={() => setShowUserMenu(false)}
            style={{ position: "fixed", inset: 0, zIndex: 51 }}
          />
          <div
            style={{
              position: "fixed",
              bottom: 72,
              right: 16,
              zIndex: 52,
              background: "rgba(20,20,28,0.95)",
              backdropFilter: "blur(20px)",
              WebkitBackdropFilter: "blur(20px)",
              border: "1px solid rgba(255,255,255,0.10)",
              borderRadius: 16,
              padding: "16px 0",
              minWidth: 200,
              boxShadow: "0 8px 32px rgba(0,0,0,0.4)",
            }}
          >
            <div style={{ padding: "0 16px", marginBottom: 12 }}>
              <p style={{ fontSize: 14, fontWeight: 600 }}>{user.email}</p>
              <p style={{ fontSize: 12, color: "var(--text-muted)", marginTop: 2 }}>{user.role}</p>
            </div>
            <div style={{ borderTop: "1px solid rgba(255,255,255,0.06)", paddingTop: 8 }}>
              <button
                onClick={() => {
                  setShowUserMenu(false);
                  setShowLogoutDialog(true);
                }}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  padding: "10px 16px",
                  width: "100%",
                  border: "none",
                  background: "transparent",
                  color: "var(--text-muted)",
                  fontSize: 14,
                  cursor: "pointer",
                  fontFamily: "inherit",
                }}
              >
                <LogOut size={16} strokeWidth={1.5} /> Sair
              </button>
            </div>
          </div>
        </>
      )}

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
