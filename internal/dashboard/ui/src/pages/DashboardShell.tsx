import { useState, useEffect } from "react";
import { Navigate, Outlet } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQueryClient, useQuery } from "@tanstack/react-query";
import ChangePasswordModal from "./ChangePasswordModal";
import { setLanguage } from "../lib/i18n";
import { Icon } from "@/components/ui/icon";
import { ConfirmDialog } from "@/components/patterns/ConfirmDialog";
import { Sidebar } from "@/components/layout/Sidebar";
import { SidebarFooter } from "@/components/layout/SidebarFooter";
import { MobileNav } from "@/components/layout/MobileNav";
import pkg from "../../package.json";

interface User {
  id: string;
  email: string;
  name?: string;
  role: string;
  language?: string;
}

function UpdateAvailableBanner() {
  const { t } = useTranslation();
  const { data } = useQuery({
    queryKey: ["version-check"],
    queryFn: () =>
      fetch("/dashboard/api/version-check", { credentials: "include" }).then(
        (r) => r.json(),
      ) as Promise<{ tag_name: string; html_url: string }>,
    staleTime: 60 * 60 * 1000,
    retry: false,
  });

  if (!data?.tag_name || !data?.html_url) return null;
  if (data.tag_name.replace(/^v/, "") === pkg.version) return null;

  return (
    <a
      href={data.html_url}
      target="_blank"
      rel="noopener noreferrer"
      title={t("nav.updateAvailable", { version: data.tag_name })}
      className="mb-2 flex items-center gap-2 rounded-[8px] px-2.5 py-2 text-[12px] font-semibold no-underline transition-colors"
      style={{ background: "var(--primary-tint)", color: "var(--primary)" }}
    >
      <Icon name="arrow_circle_up" size={16} />
      <span className="truncate">
        {t("nav.updateShort", { version: data.tag_name })}
      </span>
    </a>
  );
}

export default function DashboardShell({ user }: { user: User | null }) {
  const qc = useQueryClient();
  const { t, i18n } = useTranslation();
  const [showLogoutDialog, setShowLogoutDialog] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [showChangePassword, setShowChangePassword] = useState(false);

  const { data: brandConfig } = useQuery({
    queryKey: ["brand-config"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/config");
      return res.json() as Promise<{ company_name: string }>;
    },
    staleTime: 30000,
  });

  const { data: brandAssets } = useQuery({
    queryKey: ["brand-assets"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/brand/config");
      if (!res.ok) return { icon_url: "", logo_url: "", company_name: "" };
      return res.json() as Promise<{ icon_url: string; logo_url: string; company_name: string }>;
    },
    staleTime: 60000,
  });

  const companyName =
    brandConfig?.company_name || brandAssets?.company_name || t("app.title");

  useEffect(() => {
    if (user?.language && user.language !== i18n.language) {
      setLanguage(user.language);
    }
  }, [user?.language]);

  async function saveLanguage(lang: string) {
    setLanguage(lang);
    try {
      await fetch("/dashboard/api/me/language", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ language: lang }),
      });
    } catch {
      // persistência é best-effort; idioma já trocou localmente
    }
  }

  if (!user) return <Navigate to="/login" replace />;

  const handleLogout = async () => {
    setLoggingOut(true);
    try {
      await fetch("/dashboard/api/logout", {
        method: "POST",
        credentials: "include",
      });
      qc.clear();
      window.location.href = "/dashboard/login";
    } finally {
      setLoggingOut(false);
    }
  };

  const footer = (
    <SidebarFooter
      user={user}
      currentLang={i18n.language}
      onChangePassword={() => setShowChangePassword(true)}
      onSaveLanguage={saveLanguage}
      onLogout={() => setShowLogoutDialog(true)}
      version={pkg.version}
    />
  );

  return (
    <div
      // SPEC_DEVIATION: design.md described only Sidebar.tsx's own width
      // classes; the grid track itself was still hardcoded to 264px and
      // needed the matching responsive columns for the tablet 72px rail
      // to actually get 72px of track instead of leaving a 192px gap.
      className="grid min-h-screen max-md:grid-cols-1 md:grid-cols-[72px_1fr] lg:grid-cols-[264px_1fr]"
      style={{ background: "var(--bg-page)" }}
    >
      <Sidebar companyName={companyName} banner={<UpdateAvailableBanner />} footer={footer} />

      <main className="flex min-h-screen min-w-0 justify-center max-md:pb-[65px]">
        <div className="mx-auto w-full min-w-0 max-w-[1920px] p-10 max-md:px-4 max-md:py-4">
          <Outlet />
        </div>
      </main>

      <MobileNav
        user={user}
        currentLang={i18n.language}
        onChangePassword={() => setShowChangePassword(true)}
        onSaveLanguage={saveLanguage}
        onLogout={() => setShowLogoutDialog(true)}
        version={pkg.version}
      />

      <ConfirmDialog
        open={showLogoutDialog}
        title={t("nav.logoutConfirm")}
        message={t("nav.logoutDesc")}
        confirmLabel={loggingOut ? t("nav.loggingOut") : t("nav.logoutConfirmBtn")}
        cancelLabel={t("nav.logoutCancel")}
        icon="logout"
        loading={loggingOut}
        onConfirm={handleLogout}
        onCancel={() => setShowLogoutDialog(false)}
      />

      <ChangePasswordModal
        open={showChangePassword}
        onClose={() => setShowChangePassword(false)}
      />
    </div>
  );
}
