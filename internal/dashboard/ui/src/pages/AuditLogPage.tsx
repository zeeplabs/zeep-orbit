import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useAuditLog, AuditEntry } from "../lib/api";
import { Input } from "@/components/ui/input";
import {
  PageHeader,
  DataTable,
  type Column,
  type StatusTone,
} from "@/components/patterns";
import { cn } from "@/lib/utils";

const ACTION_TONE_COLOR: Record<StatusTone, { fg: string; bg: string }> = {
  success: { fg: "var(--success)", bg: "var(--success-tint)" },
  warning: { fg: "var(--warning)", bg: "var(--warning-tint)" },
  danger: { fg: "var(--danger)", bg: "var(--danger-tint)" },
  primary: { fg: "var(--primary)", bg: "var(--primary-tint)" },
  neutral: { fg: "var(--text-secondary)", bg: "transparent" },
};

const CATEGORIES = [
  { value: "", labelKey: "audit.categoryAll" },
  { value: "create", labelKey: "audit.categoryCreate" },
  { value: "modify", labelKey: "audit.categoryModify" },
  { value: "delete", labelKey: "audit.categoryDelete" },
  { value: "auth", labelKey: "audit.categoryAuth" },
] as const;

const RESOURCE_LABELS: Record<string, string> = {
  app: "App",
  app_member: "Member",
  app_table: "Table",
  frontend_app: "App",
  frontend_app_sync_credentials: "Sync credentials",
  github_app_config: "GitHub App",
  github_template: "Template",
  deploy_provider_config: "Deploy provider",
  auth_provider: "Auth provider",
  config: "Config",
  session: "Session",
  user: "User",
};

function resourceTypeLabel(type: string): string {
  if (RESOURCE_LABELS[type]) return RESOURCE_LABELS[type];
  return type
    .split("_")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleString("en-US", {
    month: "short",
    day: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function actionTone(action: string): StatusTone {
  if (action.includes("delete") || action.includes("remove") || action.includes("revoke")) return "danger";
  if (action.includes("create") || action.includes("added")) return "success";
  if (action.includes("update") || action.includes("change") || action.includes("reset") || action.includes("regenerate")) return "warning";
  return "neutral";
}

function useDebounced(value: string, delayMs: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}

export default function AuditLogPage() {
  const { t } = useTranslation();
  const [category, setCategory] = useState("");
  const [emailFilter, setEmailFilter] = useState("");
  const debouncedEmail = useDebounced(emailFilter, 300);
  const [page, setPage] = useState(0);
  const limit = 50;
  const { data, isLoading, error } = useAuditLog(
    limit,
    page * limit,
    undefined,
    category || undefined,
    debouncedEmail || undefined,
  );

  const totalPages = data ? Math.ceil(data.total / limit) : 0;
  const rows = data?.data ?? [];

  const columns: Column<AuditEntry>[] = [
    {
      key: "date",
      header: t("audit.date"),
      width: 160,
      render: (e) => (
        <span className="text-[12.5px] tabular-nums text-[var(--text-tertiary)]">
          {formatTime(e.created_at)}
        </span>
      ),
    },
    {
      key: "action",
      header: t("audit.action"),
      width: 220,
      render: (e) => {
        const tone = actionTone(e.action);
        const { fg, bg } = ACTION_TONE_COLOR[tone];
        if (tone === "neutral") {
          return (
            <span className="font-mono text-[12px] font-semibold" style={{ color: fg }}>
              {e.action}
            </span>
          );
        }
        return (
          <span
            className="inline-block rounded-[6px] border px-2 py-1 font-mono text-[11.5px] font-bold"
            style={{ color: fg, background: bg, borderColor: fg }}
          >
            {e.action}
          </span>
        );
      },
    },
    {
      key: "user",
      header: t("audit.user"),
      width: 220,
      render: (e) => (
        <span className="text-[13px] text-[var(--text-secondary)]">{e.user_email}</span>
      ),
    },
    {
      key: "resource",
      header: t("audit.resource"),
      render: (e) => (
        <span className="text-[13px]">
          <span className="text-[var(--text-tertiary)]">{resourceTypeLabel(e.resource_type)}: </span>
          <span className="font-medium text-[var(--text-primary)]">{e.resource_name || "—"}</span>
        </span>
      ),
    },
    {
      key: "ip",
      header: t("audit.ip"),
      width: 130,
      render: (e) => (
        <span className="font-mono text-[12px] text-[var(--text-tertiary)]">{e.ip_address || "—"}</span>
      ),
    },
  ];

  return (
    <>
      <PageHeader title={t("audit.title")} subtitle={t("audit.subtitle")} />

      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-1.5 rounded-[10px] border border-[var(--border)] bg-[var(--surface)] p-1">
          {CATEGORIES.map((c) => (
            <button
              key={c.value}
              onClick={() => { setCategory(c.value); setPage(0); }}
              className={cn(
                "cursor-pointer rounded-[8px] px-3.5 py-1.5 text-[13px] font-semibold transition-colors",
                category === c.value
                  ? "bg-[var(--primary)] text-white"
                  : "text-[var(--text-secondary)] hover:bg-[var(--hover-surface)]",
              )}
            >
              {t(c.labelKey)}
            </button>
          ))}
        </div>
        <Input
          value={emailFilter}
          onChange={(e) => { setEmailFilter(e.target.value); setPage(0); }}
          placeholder={t("audit.filterUserPlaceholder")}
          className="h-9 w-[240px] text-[12.5px]"
        />
      </div>

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
