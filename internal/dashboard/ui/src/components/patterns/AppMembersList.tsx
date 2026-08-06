import { useMemo, useState } from "react";
import { useTranslation, Trans } from "react-i18next";
import { toast } from "sonner";
import {
  AppAxis,
  AppMember,
  AppRole,
  useAddAppMember,
  useAppMembers,
  useRemoveAppMember,
  useUpdateAppMember,
  useUsers,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  ConfirmDialog,
  DataTable,
  FormDrawer,
  PageHeader,
  StatusPill,
  type Column,
} from "@/components/patterns";
import { cn } from "@/lib/utils";

interface AppMembersListProps {
  appId: string;
  axis: AppAxis;
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

/**
 * Tinted badge for a per-app role (admin/editor/viewer). Distinct
 * tones so a list of mixed-role members is scannable at a glance.
 */
function AppRoleBadge({ role }: { role: AppRole }) {
  const { t } = useTranslation();
  const tone =
    role === "admin"
      ? "primary"
      : role === "editor"
      ? "warning"
      : "neutral";
  const labelKey =
    role === "admin"
      ? "appMembers.roleAdmin"
      : role === "editor"
      ? "appMembers.roleEditor"
      : "appMembers.roleViewer";
  return <StatusPill label={t(labelKey)} tone={tone} dot={false} />;
}

/**
 * Derives a sensible display name for a user (email always, plus name
 * if available). Used in tables and dialogs to make the "who is this?"
 * question cheap.
 */
function useUserMap() {
  const { data: users } = useUsers();
  return useMemo(() => {
    const m = new Map<string, { email: string; name: string }>();
    (users ?? []).forEach((u) => m.set(u.id, { email: u.email, name: u.name || u.email }));
    return m;
  }, [users]);
}

interface AddMemberDrawerProps {
  open: boolean;
  onClose: () => void;
  appId: string;
  axis: AppAxis;
  memberIds: Set<string>;
}

function AddMemberDrawer({ open, onClose, appId, axis, memberIds }: AddMemberDrawerProps) {
  const { t } = useTranslation();
  const { data: users } = useUsers();
  const addMember = useAddAppMember(appId, axis);
  const [selectedUserId, setSelectedUserId] = useState("");
  const [role, setRole] = useState<AppRole>("editor");
  const [error, setError] = useState("");

  const candidates = (users ?? []).filter((u) => !memberIds.has(u.id));

  function reset() {
    setSelectedUserId("");
    setRole("editor");
    setError("");
  }

  async function handleSubmit() {
    setError("");
    if (!selectedUserId) {
      setError(t("appMembers.pickUser"));
      return;
    }
    try {
      await addMember.mutateAsync({ user_id: selectedUserId, role });
      reset();
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <FormDrawer
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) {
          reset();
          onClose();
        }
      }}
      title={t("appMembers.addTitle")}
      description={t("appMembers.addDesc")}
      footer={
        <div className="flex w-full gap-2.5">
          <Button
            variant="outline"
            className="flex-1"
            onClick={() => {
              reset();
              onClose();
            }}
            disabled={addMember.isPending}
          >
            {t("appMembers.cancel")}
          </Button>
          <Button
            className="flex-1"
            onClick={handleSubmit}
            disabled={addMember.isPending || candidates.length === 0}
          >
            {addMember.isPending ? t("appMembers.adding") : t("appMembers.add")}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="add-member-user">{t("appMembers.addUser")}</Label>
          <Select value={selectedUserId} onValueChange={setSelectedUserId}>
            <SelectTrigger id="add-member-user">
              <SelectValue placeholder={t("appMembers.pickUser")} />
            </SelectTrigger>
            <SelectContent>
              {candidates.length === 0 ? (
                <SelectItem value="__none__" disabled>
                  {t("appMembers.noCandidates")}
                </SelectItem>
              ) : (
                candidates.map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.name ? `${u.name} (${u.email})` : u.email}
                  </SelectItem>
                ))
              )}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="add-member-role">{t("appMembers.addRole")}</Label>
          <Select value={role} onValueChange={(v) => setRole(v as AppRole)}>
            <SelectTrigger id="add-member-role">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="admin">{t("appMembers.roleAdmin")}</SelectItem>
              <SelectItem value="editor">{t("appMembers.roleEditor")}</SelectItem>
              <SelectItem value="viewer">{t("appMembers.roleViewer")}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {error && <p className="text-[12px] text-[var(--danger)]">{error}</p>}
      </div>
    </FormDrawer>
  );
}

interface ChangeRoleDrawerProps {
  open: boolean;
  onClose: () => void;
  appId: string;
  axis: AppAxis;
  member: AppMember;
  displayName: string;
}

function ChangeRoleDrawer({ open, onClose, appId, axis, member, displayName }: ChangeRoleDrawerProps) {
  const { t } = useTranslation();
  const updateMember = useUpdateAppMember(appId, axis);
  const [role, setRole] = useState<AppRole>(member.role);
  const [error, setError] = useState("");

  function reset() {
    setRole(member.role);
    setError("");
  }

  async function handleSubmit() {
    setError("");
    if (role === member.role) {
      onClose();
      return;
    }
    try {
      await updateMember.mutateAsync({ user_id: member.user_id, role });
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <FormDrawer
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) {
          reset();
          onClose();
        }
      }}
      title={t("appMembers.changeRoleTitle")}
      description={t("appMembers.changeRoleDesc", { name: displayName })}
      footer={
        <div className="flex w-full gap-2.5">
          <Button
            variant="outline"
            className="flex-1"
            onClick={() => {
              reset();
              onClose();
            }}
            disabled={updateMember.isPending}
          >
            {t("appMembers.cancel")}
          </Button>
          <Button
            className="flex-1"
            onClick={handleSubmit}
            disabled={updateMember.isPending}
          >
            {updateMember.isPending ? t("appMembers.saving") : t("appMembers.save")}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="change-member-role">{t("appMembers.addRole")}</Label>
          <Select value={role} onValueChange={(v) => setRole(v as AppRole)}>
            <SelectTrigger id="change-member-role">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="admin">{t("appMembers.roleAdmin")}</SelectItem>
              <SelectItem value="editor">{t("appMembers.roleEditor")}</SelectItem>
              <SelectItem value="viewer">{t("appMembers.roleViewer")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        {error && <p className="text-[12px] text-[var(--danger)]">{error}</p>}
      </div>
    </FormDrawer>
  );
}

/**
 * Members management for an app (rbac-per-app T-09). Renders the list of
 * members with their per-app role, plus add/change-role/remove actions
 * gated on CanManage (the backend returns 403 for editor/viewer/non-member
 * on every /members endpoint, including GET — so non-admins see a
 * "no access" state instead of the list).
 */
export function AppMembersList({ appId, axis }: AppMembersListProps) {
  const { t } = useTranslation();
  const { data: membersResp, isLoading, error } = useAppMembers(appId, axis);
  const removeMember = useRemoveAppMember(appId, axis);
  const userMap = useUserMap();

  const [showAdd, setShowAdd] = useState(false);
  const [changeTarget, setChangeTarget] = useState<AppMember | null>(null);
  const [removeTarget, setRemoveTarget] = useState<AppMember | null>(null);

  const members = membersResp?.members ?? [];
  const memberIds = useMemo(() => new Set(members.map((m) => m.user_id)), [members]);

  // T-06 AC-6: editor/viewer/non-member get 403 on every /members endpoint.
  // We surface this as a "no access" state instead of a raw error, so the
  // tab itself stays present (consistent with the design system: omit
  // controls, don't disable them, but a hard 403 from the server can't be
  // hidden — show it).
  const accessDenied = error && (error as Error).message.toLowerCase().includes("forbidden");

  async function handleConfirmRemove() {
    if (!removeTarget) return;
    try {
      await removeMember.mutateAsync(removeTarget.user_id);
      setRemoveTarget(null);
    } catch (err) {
      // toast already shown by the mutation hook
    }
  }

  if (accessDenied) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 rounded-[14px] border border-white/[0.08] bg-white/[0.03] py-12 text-center">
        <div
          className="flex h-12 w-12 items-center justify-center rounded-[12px]"
          style={{ background: "var(--warning-tint)", color: "var(--warning)" }}
        >
          <Icon name="lock" size={20} />
        </div>
        <p className="text-[14px] font-semibold text-[var(--text-primary)]">
          {t("appMembers.noAccess")}
        </p>
        <p className="max-w-[360px] text-[12px] text-[var(--text-tertiary)]">
          {t("appMembers.noAccessDesc")}
        </p>
      </div>
    );
  }

  const columns: Column<AppMember>[] = [
    {
      key: "user",
      header: t("appMembers.colUser"),
      render: (m) => {
        const u = userMap.get(m.user_id);
        const name = u?.name ?? m.user_id;
        const email = u?.email ?? "";
        return (
          <div className="flex items-center gap-2.5">
            <div
              className="flex size-8 shrink-0 items-center justify-center rounded-[8px] border border-[var(--border)] text-[12px] font-bold"
              style={{ background: "var(--primary-tint)", color: "var(--primary)" }}
            >
              {name.charAt(0).toUpperCase()}
            </div>
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-[13px] font-medium text-[var(--text-primary)]">{name}</span>
              {email && (
                <span className="flex items-center gap-1 truncate text-[11px] text-[var(--text-tertiary)]">
                  <Icon name="mail" size={11} />
                  {email}
                </span>
              )}
            </div>
          </div>
        );
      },
    },
    {
      key: "role",
      header: t("appMembers.colRole"),
      render: (m) => <AppRoleBadge role={m.role} />,
    },
    {
      key: "added",
      header: t("appMembers.colAdded"),
      render: (m) => (
        <span className="text-[12px] text-[var(--text-tertiary)]">{formatDate(m.created_at)}</span>
      ),
    },
  ];

  return (
    <>
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div
            className="h-6 w-1 rounded-full"
            style={{ background: "linear-gradient(to bottom, var(--brand-primary), var(--brand-secondary))" }}
          />
          <p className="text-[15px] font-extrabold text-[var(--text-primary)]">{t("appMembers.title")}</p>
        </div>
        <Button onClick={() => setShowAdd(true)} className="gap-1.5">
          <Icon name="add" size={16} />
          {t("appMembers.add")}
        </Button>
      </div>

      <DataTable
        columns={columns}
        rows={members}
        getRowId={(m) => m.user_id}
        loading={isLoading}
        error={Boolean(error)}
        errorState={{
          title: t("appMembers.error"),
          description: error ? (error as Error).message : undefined,
        }}
        empty={{
          icon: "group",
          title: t("appMembers.empty"),
          description: t("appMembers.emptyDesc"),
          action: { label: t("appMembers.add"), onClick: () => setShowAdd(true) },
        }}
        rowActions={(m) => (
          <>
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              onClick={() => setChangeTarget(m)}
              title={t("appMembers.changeRole")}
              data-testid={`member-change-${m.user_id}`}
            >
              <Icon name="edit" size={15} />
            </Button>
            <Button
              variant="outline"
              size="icon"
              className={cn(
                "size-8 border-[var(--danger)]/30 text-[var(--danger)] hover:bg-[var(--danger-tint)]",
              )}
              onClick={() => setRemoveTarget(m)}
              title={t("appMembers.remove")}
              data-testid={`member-remove-${m.user_id}`}
            >
              <Icon name="delete" size={15} />
            </Button>
          </>
        )}
      />

      <AddMemberDrawer
        open={showAdd}
        onClose={() => setShowAdd(false)}
        appId={appId}
        axis={axis}
        memberIds={memberIds}
      />

      {changeTarget && (
        <ChangeRoleDrawer
          open={Boolean(changeTarget)}
          onClose={() => setChangeTarget(null)}
          appId={appId}
          axis={axis}
          member={changeTarget}
          displayName={userMap.get(changeTarget.user_id)?.name ?? userMap.get(changeTarget.user_id)?.email ?? changeTarget.user_id}
        />
      )}

      <ConfirmDialog
        open={Boolean(removeTarget)}
        title={t("appMembers.removeTitle")}
        message={
          <Trans
            i18nKey="appMembers.removeDesc"
            values={{ name: removeTarget ? (userMap.get(removeTarget.user_id)?.name ?? userMap.get(removeTarget.user_id)?.email ?? removeTarget.user_id) : "" }}
          />
        }
        confirmLabel={removeMember.isPending ? t("appMembers.removing") : t("appMembers.removeConfirm")}
        cancelLabel={t("appMembers.cancel")}
        destructive
        icon="delete"
        loading={removeMember.isPending}
        onConfirm={handleConfirmRemove}
        onCancel={() => setRemoveTarget(null)}
      />
    </>
  );
}
