import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuditLog, auditActionLabel, AuditEntry, useUsers } from "../lib/api";
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
  type Column,
  type StatusTone,
} from "@/components/patterns";

const ALL = "__all__";

function formatTime(iso: string) {
  return new Date(iso).toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function actionTone(action: string): StatusTone {
  if (action.includes("delete") || action.includes("deactivate")) return "danger";
  if (action.includes("create") || action.includes("activate")) return "success";
  if (action.includes("update") || action.includes("change") || action.includes("reset")) return "warning";
  return "neutral";
}

const ACTIONS = [
  "app.create", "app.update", "app.delete",
  "user.create", "user.delete",
  "user.login", "user.logout",
  "user.password.change", "config.update",
  "auth.provider.update",
  "app.user.deactivate", "app.user.activate", "app.user.sessions.reset",
  "data.create", "data.update", "data.delete",
  "bootstrap.complete",
];

export default function AuditLogPage() {
  const { t } = useTranslation();
  const [actionFilter, setActionFilter] = useState("");
  const [userFilter, setUserFilter] = useState("");
  const [page, setPage] = useState(0);
  const limit = 50;
  const { data, isLoading, error } = useAuditLog(
    limit,
    page * limit,
    actionFilter || undefined,
    userFilter || undefined,
  );
  const { data: users } = useUsers();

  const totalPages = data ? Math.ceil(data.total / limit) : 0;
  const userOptions = users?.filter((u) => u.role === "superadmin") || [];
  const rows = data?.data ?? [];

  const columns: Column<AuditEntry>[] = [
    {
      key: "date",
      header: t("audit.date"),
      width: 150,
      render: (e) => (
        <span className="text-[12px] tabular-nums text-[var(--text-tertiary)]">
          {formatTime(e.created_at)}
        </span>
      ),
    },
    {
      key: "action",
      header: t("audit.action"),
      width: 150,
      render: (e) => <StatusPill label={auditActionLabel(e.action)} tone={actionTone(e.action)} dot={false} />,
    },
    {
      key: "user",
      header: t("audit.user"),
      width: 200,
      render: (e) => (
        <span className="text-[13px] font-medium text-[var(--text-primary)]">{e.user_email}</span>
      ),
    },
    {
      key: "resource",
      header: t("audit.resource"),
      width: 180,
      render: (e) => (
        <div className="flex flex-col">
          <span className="font-mono text-[12px] text-[var(--text-secondary)]">{e.resource_type}</span>
          {e.resource_name && (
            <span className="max-w-[180px] truncate text-[11px] text-[var(--text-tertiary)]">
              {e.resource_name}
            </span>
          )}
        </div>
      ),
    },
    {
      key: "ip",
      header: t("audit.ip"),
      width: 120,
      render: (e) => (
        <span className="font-mono text-[11px] text-[var(--text-tertiary)]">{e.ip_address || "—"}</span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={t("audit.title")}
        subtitle={t("audit.subtitle")}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Select
              value={actionFilter || ALL}
              onValueChange={(v) => { setActionFilter(v === ALL ? "" : v); setPage(0); }}
            >
              <SelectTrigger className="h-9 w-[180px] text-[12px]">
                <SelectValue placeholder={t("audit.filterAction")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t("audit.filterAction")}</SelectItem>
                {ACTIONS.map((a) => (
                  <SelectItem key={a} value={a}>{auditActionLabel(a)}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select
              value={userFilter || ALL}
              onValueChange={(v) => { setUserFilter(v === ALL ? "" : v); setPage(0); }}
            >
              <SelectTrigger className="h-9 w-[200px] text-[12px]">
                <SelectValue placeholder={t("audit.filterUser")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t("audit.filterUser")}</SelectItem>
                {userOptions.map((u) => (
                  <SelectItem key={u.id} value={u.id}>{u.email}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <DataTable
        columns={columns}
        rows={rows}
        getRowId={(e) => e.id}
        loading={isLoading}
        error={Boolean(error)}
        errorState={{
          title: t("audit.error"),
          description: error ? (error as Error).message : undefined,
        }}
        empty={{
          icon: "shield",
          title: t("audit.empty"),
          description: t("audit.emptyDesc"),
        }}
        pagination={
          totalPages > 1
            ? {
                page: page + 1,
                pageCount: totalPages,
                onPageChange: (p) => setPage(p - 1),
                prevLabel: t("common.previous"),
                nextLabel: t("common.next"),
                label: data ? t("audit.total", { count: data.total }) : undefined,
              }
            : undefined
        }
      />
    </>
  );
}
