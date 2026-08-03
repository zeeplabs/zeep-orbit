import { useState, useEffect, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useLogs, useLogMetrics, useApps, LogEntry } from "../lib/api";
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
  type Column,
  type StatusTone,
} from "@/components/patterns";

const ALL = "__all__";

type LogRow = LogEntry & { _id: string };

function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString("pt-BR", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return body;
  }
}

function statusTone(status: number): StatusTone {
  if (status >= 500) return "danger";
  if (status >= 400) return "warning";
  return "success";
}

function methodTone(method: string): StatusTone {
  switch (method) {
    case "POST": return "success";
    case "PUT": return "warning";
    case "DELETE": return "danger";
    case "GET": return "primary";
    default: return "neutral";
  }
}

interface StatCardProps {
  icon: string;
  label: string;
  value: string | number;
  sub?: string;
  fg?: string;
  bg?: string;
}

function StatCard({ icon, label, value, sub, fg, bg }: StatCardProps) {
  return (
    <div className="rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5 max-md:p-3">
      <div className="mb-3 flex items-center gap-2.5 max-md:mb-2">
        <div
          className="flex size-9 items-center justify-center rounded-[10px] max-md:size-8"
          style={{ background: bg ?? "var(--hover-surface)", color: fg ?? "var(--text-tertiary)" }}
        >
          <Icon name={icon} size={16} />
        </div>
        <span className="text-[11px] font-semibold uppercase tracking-[0.08em] text-[var(--text-tertiary)]">
          {label}
        </span>
      </div>
      <div className="text-[28px] font-extrabold leading-none tracking-tight text-[var(--text-primary)] max-md:text-[22px]">
        {value}
      </div>
      {sub && <div className="mt-1.5 text-[12px] text-[var(--text-tertiary)]">{sub}</div>}
    </div>
  );
}

function DetailField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
        {label}
      </p>
      {children}
    </div>
  );
}

export default function LogsPage() {
  const { t } = useTranslation();
  const [appFilter, setAppFilter] = useState("");
  const [expandedRow, setExpandedRow] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [countdown, setCountdown] = useState(10);
  const { data: logs, isLoading, error, refetch } = useLogs(appFilter || undefined, autoRefresh);
  const { data: metrics } = useLogMetrics(autoRefresh);
  const { data: apps } = useApps();

  useEffect(() => {
    if (!autoRefresh) return;
    setCountdown(10);
    const interval = setInterval(() => {
      setCountdown((prev) => (prev <= 1 ? 10 : prev - 1));
    }, 1000);
    return () => clearInterval(interval);
  }, [autoRefresh, logs]);

  const rows: LogRow[] = (logs ?? []).map((e, i) => ({ ...e, _id: `${e.timestamp}-${i}` }));

  const columns: Column<LogRow>[] = [
    {
      key: "status",
      header: t("logs.colStatus"),
      width: 90,
      render: (e) => <StatusPill label={String(e.status)} tone={statusTone(e.status)} dot={false} />,
    },
    {
      key: "method",
      header: t("logs.colMethod"),
      width: 90,
      render: (e) => <StatusPill label={e.method} tone={methodTone(e.method)} dot={false} />,
    },
    {
      key: "path",
      header: t("logs.colPath"),
      render: (e) => (
        <span className="font-mono text-[13px] font-medium text-[var(--text-primary)]">{e.path}</span>
      ),
    },
    {
      key: "app",
      header: t("logs.colApp"),
      width: 120,
      render: (e) =>
        e.app ? (
          <span className="inline-flex items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--hover-surface)] px-2 py-0.5 text-[11px] font-medium text-[var(--text-secondary)]">
            <Icon name="dns" size={11} />
            {e.app}
          </span>
        ) : (
          <span className="text-[11px] text-[var(--text-tertiary)]">—</span>
        ),
    },
    {
      key: "latency",
      header: t("logs.colLatency"),
      width: 90,
      align: "right",
      render: (e) => (
        <span className="text-[12px] font-medium tabular-nums text-[var(--text-secondary)]">
          {e.latency_ms}ms
        </span>
      ),
    },
    {
      key: "time",
      header: t("logs.colTime"),
      width: 100,
      align: "right",
      render: (e) => (
        <span className="text-[12px] tabular-nums text-[var(--text-tertiary)]">
          {formatTime(e.timestamp)}
        </span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={t("logs.title")}
        subtitle={t("logs.subtitle")}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {metrics && autoRefresh && (
              <span className="inline-flex items-center gap-1.5 rounded-full border border-[var(--success)]/25 bg-[var(--success-tint)] px-3 py-1">
                <span className="relative flex size-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[var(--success)] opacity-75" />
                  <span className="relative inline-flex size-2 rounded-full bg-[var(--success)]" />
                </span>
                <span className="text-[11px] font-medium text-[var(--success)] tabular-nums">{countdown}s</span>
              </span>
            )}
            <button
              onClick={() => setAutoRefresh((v) => !v)}
              className="h-9 rounded-[8px] border border-[var(--border)] bg-[var(--surface)] px-3 text-[12px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)] cursor-pointer"
              title={autoRefresh ? t("logs.refreshOff") : t("logs.refreshOn")}
            >
              {autoRefresh ? t("logs.modeAuto") : t("logs.modeManual")}
            </button>
            <button
              onClick={() => refetch()}
              className="flex h-9 w-9 items-center justify-center rounded-[8px] border border-[var(--border)] bg-[var(--surface)] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)] cursor-pointer"
              title={t("logs.refresh")}
            >
              <Icon name="refresh" size={16} />
            </button>
            <Select
              value={appFilter || ALL}
              onValueChange={(v) => setAppFilter(v === ALL ? "" : v)}
            >
              <SelectTrigger className="h-9 w-[160px] text-[12px]">
                <SelectValue placeholder={t("logs.filterAll")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t("logs.filterAll")}</SelectItem>
                {apps?.map((a) => (
                  <SelectItem key={a.id} value={a.name}>{a.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          icon="bar_chart"
          label={t("logs.totalReqs")}
          value={metrics?.total_requests ?? "-"}
          sub={t("logs.totalSub")}
          fg="var(--primary)"
          bg="var(--primary-tint)"
        />
        <StatCard
          icon="schedule"
          label={t("logs.avgLatency")}
          value={metrics?.avg_latency_ms != null ? `${metrics.avg_latency_ms}ms` : "-"}
          sub={t("logs.avgLatencySub")}
        />
        <StatCard
          icon="warning"
          label={t("logs.errors4xx")}
          value={metrics?.errors_4xx ?? "-"}
          sub={t("logs.errors4xxSub")}
          fg="var(--warning)"
          bg="var(--warning-tint)"
        />
        <StatCard
          icon="error"
          label={t("logs.errors5xx")}
          value={metrics?.errors_5xx ?? "-"}
          sub={t("logs.errors5xxSub")}
          fg="var(--danger)"
          bg="var(--danger-tint)"
        />
      </div>

      {metrics?.method_breakdown && (
        <div className="mb-6 flex flex-wrap items-center gap-2">
          <span className="text-[11px] font-semibold uppercase tracking-[0.08em] text-[var(--text-tertiary)]">
            {t("logs.methods")}
          </span>
          {Object.entries(metrics.method_breakdown).map(([method, count]) => (
            <StatusPill key={method} label={`${method} ${count}`} tone={methodTone(method)} dot={false} />
          ))}
          {metrics.requests_per_app && Object.keys(metrics.requests_per_app).length > 0 && (
            <>
              <span className="ml-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-[var(--text-tertiary)]">
                {t("logs.apps")}:
              </span>
              {Object.entries(metrics.requests_per_app).map(([app, count]) => (
                <StatusPill key={app} label={`${app} ${count}`} tone="neutral" dot={false} />
              ))}
            </>
          )}
        </div>
      )}

      <DataTable
        columns={columns}
        rows={rows}
        getRowId={(e) => e._id}
        loading={isLoading}
        error={Boolean(error)}
        errorState={{
          title: t("logs.error"),
          description: error ? (error as Error).message : undefined,
        }}
        empty={{ icon: "monitoring", title: t("logs.empty"), description: t("logs.emptyDesc") }}
        expandedRowId={expandedRow}
        onToggleExpand={(id) => setExpandedRow((cur) => (cur === id ? null : id))}
        renderExpanded={(e) => (
          <div className="grid grid-cols-2 gap-4 text-[12px]">
            <DetailField label="Query">
              <code className="break-all text-[var(--text-secondary)]">{e.query || "—"}</code>
            </DetailField>
            <DetailField label="Content-Type">
              <code className="text-[var(--text-secondary)]">{e.content_type || "—"}</code>
            </DetailField>
            <DetailField label="Remote Addr">
              <code className="text-[var(--text-secondary)]">{e.remote_addr || "—"}</code>
            </DetailField>
            <DetailField label="User-Agent">
              <code className="block max-w-[300px] truncate text-[var(--text-secondary)]">{e.user_agent || "—"}</code>
            </DetailField>
            {e.req_body && (
              <div className="col-span-2">
                <DetailField label="Request Body">
                  <pre className="max-h-[200px] overflow-x-auto rounded-[10px] bg-[var(--bg-page)] p-3 text-[11px] leading-relaxed text-[var(--text-secondary)]">
                    {formatBody(e.req_body)}
                  </pre>
                </DetailField>
              </div>
            )}
            {e.res_body && (
              <div className="col-span-2">
                <DetailField label="Response Body">
                  <pre className="max-h-[200px] overflow-x-auto rounded-[10px] bg-[var(--bg-page)] p-3 text-[11px] leading-relaxed text-[var(--text-secondary)]">
                    {formatBody(e.res_body)}
                  </pre>
                </DetailField>
              </div>
            )}
          </div>
        )}
      />
    </>
  );
}
