import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation, Trans } from "react-i18next";
import { toast } from "sonner";
import ChangePasswordModal from "./ChangePasswordModal";
import { useUsers, useCreateUser, useDeleteUser, UserDef } from "../lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Icon } from "@/components/ui/icon";
import {
  PageHeader,
  DataTable,
  StatusPill,
  FormDrawer,
  ConfirmDialog,
  type Column,
  type StatusTone,
} from "@/components/patterns";

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

const ROLE_TONE: Record<string, StatusTone> = {
  superadmin: "primary",
  admin: "success",
  auditor: "warning",
  member: "neutral",
};

const ROLE_LABEL_KEY: Record<string, string> = {
  superadmin: "users.roleSuperadmin",
  admin: "users.roleAdmin",
  auditor: "users.roleAuditor",
  member: "users.roleMember",
};

function RoleBadge({ role }: { role: string }) {
  const { t } = useTranslation();
  return (
    <StatusPill
      label={t(ROLE_LABEL_KEY[role] || role)}
      tone={ROLE_TONE[role] || "neutral"}
      dot={false}
    />
  );
}

const ROLE_OPTIONS = [
  { value: "superadmin", icon: "shield_person", labelKey: "users.roleSuperadmin", descKey: "users.roleSuperadminDesc" },
  { value: "admin", icon: "admin_panel_settings", labelKey: "users.roleAdmin", descKey: "users.roleAdminDesc" },
  { value: "auditor", icon: "visibility", labelKey: "users.roleAuditor", descKey: "users.roleAuditorDesc" },
  { value: "member", icon: "person", labelKey: "users.roleMember", descKey: "users.roleMemberDesc" },
] as const;

function RoleOption({
  value,
  icon,
  label,
  description,
  selected,
  onSelect,
}: {
  value: string;
  icon: string;
  label: string;
  description: string;
  selected: boolean;
  onSelect: (value: string) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onSelect(value)}
      className="flex w-full items-start gap-3 rounded-[10px] border p-3 text-left transition-colors"
      style={{
        borderColor: selected ? "var(--primary)" : "var(--border-strong)",
        background: selected ? "var(--primary-tint)" : "var(--bg-page)",
      }}
    >
      <Icon name={icon} size={18} style={{ color: selected ? "var(--primary)" : "var(--text-secondary)" }} className="mt-0.5 shrink-0" />
      <div className="min-w-0">
        <div
          className="text-[13px] font-bold"
          style={{ color: selected ? "var(--primary)" : "var(--text-secondary)" }}
        >
          {label}
        </div>
        <div className="mt-0.5 text-[12px] leading-relaxed text-[var(--text-tertiary)]">{description}</div>
      </div>
    </button>
  );
}

interface CreateUserDrawerProps {
  open: boolean;
  onClose: () => void;
  currentUserRole?: string;
}

function CreateUserDrawer({ open, onClose, currentUserRole }: CreateUserDrawerProps) {
  const { t } = useTranslation();
  const createUser = useCreateUser();
  const roleOptions = ROLE_OPTIONS.filter((opt) => opt.value !== "superadmin" || currentUserRole === "superadmin");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("admin");
  const [error, setError] = useState("");

  function reset() {
    setEmail("");
    setName("");
    setPassword("");
    setRole("admin");
    setError("");
  }

  async function handleSubmit() {
    setError("");
    try {
      await createUser.mutateAsync({ email, name, password, role });
      reset();
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <FormDrawer
      open={open}
      onOpenChange={(isOpen) => { if (!isOpen) { reset(); onClose(); } }}
      title={t("users.createTitle")}
      description={t("users.createDesc")}
      footer={
        <div className="flex w-full gap-2.5">
          <Button
            variant="outline"
            className="flex-1"
            onClick={() => { reset(); onClose(); }}
            disabled={createUser.isPending}
          >
            {t("users.cancel")}
          </Button>
          <Button
            className="flex-1"
            onClick={handleSubmit}
            disabled={createUser.isPending}
          >
            {createUser.isPending ? t("users.creating") : t("users.createBtn")}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-user-name">{t("users.name")}</Label>
          <Input
            id="new-user-name"
            type="text"
            placeholder={t("users.namePlaceholder")}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-user-email">{t("users.email")}</Label>
          <Input
            id="new-user-email"
            type="email"
            placeholder={t("users.emailPlaceholder")}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-user-password">{t("users.password")}</Label>
          <Input
            id="new-user-password"
            type="password"
            placeholder={t("users.passwordHint")}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label>{t("users.role")}</Label>
          <div className="flex flex-col gap-2">
            {roleOptions.map((opt) => (
              <RoleOption
                key={opt.value}
                value={opt.value}
                icon={opt.icon}
                label={t(opt.labelKey)}
                description={t(opt.descKey)}
                selected={role === opt.value}
                onSelect={setRole}
              />
            ))}
          </div>
          <div
            className="mt-1 flex items-start gap-2 rounded-[10px] border p-3 text-[11.5px] leading-relaxed text-[var(--text-tertiary)]"
            style={{ borderColor: "var(--border)", background: "var(--bg-sunken)" }}
          >
            <Icon name="info" size={14} className="mt-0.5 shrink-0" />
            {t("users.roleFootnote")}
          </div>
        </div>

        {error && <p className="text-[12px] text-[var(--danger)]">{error}</p>}
      </div>
    </FormDrawer>
  );
}

function DashboardRoleInfoBanner() {
  return (
    <div
      className="mb-4 flex items-start gap-2.5 rounded-[10px] border p-4 text-[13px] leading-relaxed"
      style={{ borderColor: "var(--primary)", background: "var(--primary-tint)", color: "var(--text-secondary)" }}
    >
      <Icon name="info" size={16} style={{ color: "var(--primary)" }} className="mt-0.5 shrink-0" />
      <Trans
        i18nKey="users.dashboardRoleInfo"
        components={{ 1: <span className="font-semibold text-[var(--primary)]" /> }}
      />
    </div>
  );
}

export default function UsersPage() {
  const { t } = useTranslation();
  const { data: users, isLoading, error } = useUsers();
  const deleteUser = useDeleteUser();

  const { data: currentUser } = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/me", { credentials: "include" });
      if (!res.ok) return null;
      return res.json() as Promise<{ id: string; email: string; name: string; role: string; language: string }>;
    },
    retry: false,
    staleTime: 30000,
  });

  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<UserDef | null>(null);
  const [passwordTarget, setPasswordTarget] = useState<UserDef | null>(null);

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    try {
      await deleteUser.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  const columns: Column<UserDef>[] = [
    {
      key: "user",
      header: t("users.colUser"),
      render: (u) => (
        <div className="flex items-center gap-2.5">
          <div
            className="flex size-8 shrink-0 items-center justify-center rounded-full text-[12px] font-bold text-white"
            style={{ background: "linear-gradient(135deg, var(--primary), var(--accent))" }}
          >
            {(u.name || u.email).charAt(0).toUpperCase()}
          </div>
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-[13.5px] font-semibold text-[var(--text-primary)]">
              {u.name || u.email}
            </span>
            {u.name && (
              <span className="truncate text-[12px] text-[var(--text-tertiary)]">{u.email}</span>
            )}
          </div>
        </div>
      ),
    },
    {
      key: "role",
      header: t("users.colRole"),
      render: (u) => <RoleBadge role={u.role} />,
    },
    {
      key: "signIn",
      header: t("users.colSignIn"),
      render: (u) => {
        const isGoogle = u.sign_in === "google";
        return (
          <span className="inline-flex items-center gap-1.5 text-[12.5px] text-[var(--text-secondary)]">
            {isGoogle ? (
              <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
                <path fill="#4285F4" d="M15.68 8.18c0-.58-.05-1.14-.15-1.68H8v3.18h4.3a3.68 3.68 0 0 1-1.6 2.42v2h2.6c1.52-1.4 2.38-3.46 2.38-5.92z" />
                <path fill="#34A853" d="M8 16c2.16 0 3.97-.72 5.3-1.9l-2.6-2c-.72.48-1.64.77-2.7.77-2.08 0-3.84-1.4-4.47-3.3H.85v2.07A8 8 0 0 0 8 16z" />
                <path fill="#FBBC05" d="M3.53 9.57A4.8 4.8 0 0 1 3.28 8c0-.55.1-1.08.25-1.57V4.36H.85A8 8 0 0 0 0 8c0 1.29.31 2.5.85 3.64l2.68-2.07z" />
                <path fill="#EA4335" d="M8 3.18c1.18 0 2.23.4 3.06 1.2l2.3-2.3C11.96.9 10.15.14 8 .14A8 8 0 0 0 .85 4.36l2.68 2.07C4.16 4.53 5.92 3.18 8 3.18z" />
              </svg>
            ) : (
              <Icon name="mail" size={14} />
            )}
            {isGoogle ? t("users.signInGoogle") : t("users.signInEmail")}
          </span>
        );
      },
    },
    {
      key: "created",
      header: t("users.colCreated"),
      render: (u) => (
        <span className="text-[12.5px] text-[var(--text-tertiary)]">{formatDate(u.created_at)}</span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={t("users.title")}
        subtitle={t("users.subtitle")}
        actions={
          <Button className="gap-2" onClick={() => setShowCreate(true)}>
            <Icon name="add" size={16} />
            {t("users.create")}
          </Button>
        }
      />

      <DashboardRoleInfoBanner />

      <DataTable
        columns={columns}
        rows={users ?? []}
        getRowId={(u) => u.id}
        loading={isLoading}
        error={Boolean(error)}
        errorState={{
          title: t("users.error"),
          description: error ? (error as Error).message : undefined,
        }}
        empty={{
          icon: "group",
          title: t("users.empty"),
          action: { label: t("users.create"), onClick: () => setShowCreate(true) },
        }}
        rowActions={(u) =>
          currentUser && u.id !== currentUser.id ? (
            <>
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-[var(--text-tertiary)] hover:bg-transparent hover:text-[var(--text-primary)]"
                onClick={() => setPasswordTarget(u)}
                title={t("users.changePassword")}
              >
                <Icon name="lock_reset" size={15} />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-8 cursor-not-allowed text-[var(--text-tertiary)] opacity-50 hover:bg-transparent"
                disabled
                title={t("users.reset2faSoon")}
              >
                <Icon name="phonelink_lock" size={15} />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-[var(--danger)] hover:bg-transparent hover:text-[var(--danger)]"
                onClick={() => setDeleteTarget(u)}
                title={t("apps.delete")}
              >
                <Icon name="delete" size={15} />
              </Button>
            </>
          ) : null
        }
      />

      <CreateUserDrawer open={showCreate} onClose={() => setShowCreate(false)} currentUserRole={currentUser?.role} />

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        title={t("users.deleteTitle")}
        message={<Trans i18nKey="users.deleteDesc" values={{ email: deleteTarget?.email }} />}
        confirmLabel={deleteUser.isPending ? t("users.deleting") : t("users.deleteConfirm")}
        cancelLabel={t("users.deleteCancel")}
        destructive
        icon="delete"
        loading={deleteUser.isPending}
        onConfirm={handleConfirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      <ChangePasswordModal
        open={Boolean(passwordTarget)}
        onClose={() => setPasswordTarget(null)}
        targetUserId={passwordTarget?.id}
        targetEmail={passwordTarget?.email}
      />
    </>
  );
}
