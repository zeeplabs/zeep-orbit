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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  PageHeader,
  DataTable,
  StatusPill,
  FormDrawer,
  ConfirmDialog,
  type Column,
} from "@/components/patterns";

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

function RoleBadge({ role }: { role: string }) {
  const { t } = useTranslation();
  const isSuper = role === "superadmin";
  return (
    <StatusPill
      label={isSuper ? t("users.roleSuperadmin") : t("users.roleAdmin")}
      tone={isSuper ? "primary" : "neutral"}
      dot={false}
    />
  );
}

interface CreateUserDrawerProps {
  open: boolean;
  onClose: () => void;
}

function CreateUserDrawer({ open, onClose }: CreateUserDrawerProps) {
  const { t } = useTranslation();
  const createUser = useCreateUser();
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
          <Label htmlFor="new-user-email">{t("users.email")}</Label>
          <Input
            id="new-user-email"
            type="email"
            placeholder={t("users.email")}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>

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
          <Label htmlFor="new-user-role">{t("users.role")}</Label>
          <Select value={role} onValueChange={setRole}>
            <SelectTrigger id="new-user-role">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="admin">{t("users.roleAdmin")}</SelectItem>
              <SelectItem value="superadmin">{t("users.roleSuperadmin")}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {error && <p className="text-[12px] text-[var(--danger)]">{error}</p>}
      </div>
    </FormDrawer>
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
            className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-[var(--border)] text-[12px] font-bold"
            style={{ background: "var(--primary-tint)", color: "var(--primary)" }}
          >
            {(u.name || u.email).charAt(0).toUpperCase()}
          </div>
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-[13px] font-medium text-[var(--text-primary)]">
              {u.name || u.email}
            </span>
            {u.name && (
              <span className="flex items-center gap-1 truncate text-[11px] text-[var(--text-tertiary)]">
                <Icon name="mail" size={11} />
                {u.email}
              </span>
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
      key: "created",
      header: t("users.colCreated"),
      render: (u) => (
        <span className="text-[12px] text-[var(--text-tertiary)]">{formatDate(u.created_at)}</span>
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
                variant="outline"
                size="icon"
                className="size-8"
                onClick={() => setPasswordTarget(u)}
                title={t("users.changePassword")}
              >
                <Icon name="lock_reset" size={15} />
              </Button>
              <Button
                variant="outline"
                size="icon"
                className="size-8 border-[var(--danger)]/30 text-[var(--danger)] hover:bg-[var(--danger-tint)]"
                onClick={() => setDeleteTarget(u)}
                title={t("apps.delete")}
              >
                <Icon name="delete" size={15} />
              </Button>
            </>
          ) : null
        }
      />

      <CreateUserDrawer open={showCreate} onClose={() => setShowCreate(false)} />

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
