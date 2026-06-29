import { useState, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import {
  Activity,
  BarChart3,
  Clock,
  Server,
  AlertTriangle,
  XCircle,
  RefreshCw,
  Filter,
  ChevronDown,
  ChevronUp,
} from "lucide-react";
import { useLogs, useLogMetrics, useApps, LogEntry } from "../lib/api";
import { Badge } from "@/components/ui/badge";

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString("pt-BR", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function statusBadge(status: number) {
  const isError = status >= 400;
  const isServerError = status >= 500;
  return (
    <span
      className={`inline-flex min-w-[44px] items-center justify-center rounded-md px-2 py-0.5 text-[11px] font-bold tabular-nums ${
        isServerError
          ? "bg-red-500/[0.12] text-red-400 border border-red-500/[0.20]"
          : isError
            ? "bg-amber-500/[0.10] text-amber-400 border border-amber-500/[0.18]"
            : "bg-emerald-500/[0.10] text-emerald-400 border border-emerald-500/[0.18]"
      }`}
    >
      {status}
    </span>
  );
}

function methodBadge(method: string) {
  const colors: Record<string, string> = {
    GET: "text-sky-400 bg-sky-500/[0.08] border-sky-500/[0.16]",
    POST: "text-emerald-400 bg-emerald-500/[0.08] border-emerald-500/[0.16]",
    PUT: "text-amber-400 bg-amber-500/[0.08] border-amber-500/[0.16]",
    PATCH: "text-purple-400 bg-purple-500/[0.08] border-purple-500/[0.16]",
    DELETE: "text-red-400 bg-red-500/[0.08] border-red-500/[0.16]",
  };
  const c = colors[method] || "text-[#94A3B8] bg-white/[0.06] border-white/[0.10]";
  return (
    <span
      className={`inline-flex min-w-[52px] items-center justify-center rounded-md px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider border ${c}`}
    >
      {method}
    </span>
  );
}

function SkeletonRow() {
  return (
    <div className="flex items-center gap-3 border-b border-white/[0.04] px-4 py-3">
      <div className="h-3.5 w-14 rounded bg-white/[0.06]" />
      <div className="h-3.5 w-10 rounded bg-white/[0.06]" />
      <div className="h-3.5 w-32 rounded bg-white/[0.06]" />
      <div className="h-3.5 w-8 rounded bg-white/[0.06]" />
      <div className="ml-auto h-3.5 w-12 rounded bg-white/[0.06]" />
    </div>
  );
}

interface MetricCardProps {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  sub?: string;
  accent?: string;
}

function MetricCard({ icon, label, value, sub, accent }: MetricCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-5 max-md:p-3"
    >
      <div className="mb-3 max-md:mb-2 flex items-center gap-2.5">
        <div
          className="flex size-9 max-md:size-8 items-center justify-center rounded-xl border"
          style={{
            borderColor: accent ? `${accent}20` : "rgba(255,255,255,0.10)",
            backgroundColor: accent ? `${accent}10` : "rgba(255,255,255,0.06)",
            color: accent || "#94A3B8",
          }}
        >
          {icon}
        </div>
        <span className="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
          {label}
        </span>
      </div>
      <div className="text-[28px] max-md:text-[22px] font-extrabold leading-none tracking-tight text-[#F8FAFC]">
        {value}
      </div>
      {sub && (
        <div className="mt-1.5 text-[12px] text-[#64748B]">{sub}</div>
      )}
    </motion.div>
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
    if (!autoRefresh) return
    setCountdown(10)
    const interval = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          return 10
        }
        return prev - 1
      })
    }, 1000)
    return () => clearInterval(interval)
  }, [autoRefresh, logs])

  const handleManualRefresh = () => {
    refetch()
  }

  return (
    <div className="relative z-10">
      {/* Header */}
      <motion.div {...fadeUp} className="mb-8 max-md:mb-6">
        <span
          className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          <Activity size={12} strokeWidth={1.5} />
          {t("nav.logs")}
        </span>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="mb-1.5 text-[28px] max-md:text-[22px] font-extrabold leading-tight">
              {t("logs.title")}
            </h2>
            <p className="text-sm max-md:text-[13px] text-[#94A3B8]">
              {t("logs.subtitle")}
            </p>
          </div>

          <div className="flex items-center gap-3 max-md:w-full">
            {metrics && autoRefresh && (
              <div className="flex items-center gap-1.5 rounded-full border border-emerald-500/[0.20] bg-emerald-500/[0.08] px-3 py-1 shrink-0">
                <span className="relative flex size-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex size-2 rounded-full bg-emerald-500" />
                </span>
                <span className="text-[11px] font-medium text-emerald-400">
                  {countdown}s
                </span>
              </div>
            )}

            <button
              onClick={() => setAutoRefresh((v) => !v)}
              className="h-9 px-3 rounded-xl border border-white/[0.10] bg-white/[0.06] text-[12px] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors cursor-pointer whitespace-nowrap"
              title={autoRefresh ? t("logs.refreshOff") : t("logs.refreshOn")}
            >
              {autoRefresh ? t("logs.modeAuto") : t("logs.modeManual")}
            </button>

            <button
              onClick={handleManualRefresh}
              className="h-9 w-9 flex items-center justify-center rounded-xl border border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors cursor-pointer"
              title={t("logs.refresh")}
            >
              <RefreshCw size={14} />
            </button>

            <select
              value={appFilter}
              onChange={(e) => setAppFilter(e.target.value)}
              className="h-9 rounded-xl border border-white/[0.10] bg-white/[0.06] px-3 text-[12px] text-[#F8FAFC] outline-none appearance-none cursor-pointer max-md:flex-1"
            >
              <option value="" className="bg-[#0D0D14]">{t("logs.filterAll")}</option>
              {apps?.map((a) => (
                <option key={a.id} value={a.name} className="bg-[#0D0D14]">
                  {a.name}
                </option>
              ))}
            </select>
          </div>
        </div>
      </motion.div>

      {/* Metrics grid */}
      <div className="mb-8 max-md:mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MetricCard
          icon={<BarChart3 size={15} strokeWidth={1.5} />}
          label={t("logs.totalReqs")}
          value={metrics?.total_requests ?? "-"}
          sub={t("logs.totalSub")}
          accent="var(--brand-primary)"
        />
        <MetricCard
          icon={<Clock size={15} strokeWidth={1.5} />}
          label={t("logs.avgLatency")}
          value={metrics?.avg_latency_ms != null ? `${metrics.avg_latency_ms}ms` : "-"}
          sub={t("logs.avgLatencySub")}
          accent="#06B6D4"
        />
        <MetricCard
          icon={<AlertTriangle size={15} strokeWidth={1.5} />}
          label={t("logs.errors4xx")}
          value={metrics?.errors_4xx ?? "-"}
          sub={t("logs.errors4xxSub")}
          accent="#F59E0B"
        />
        <MetricCard
          icon={<XCircle size={15} strokeWidth={1.5} />}
          label={t("logs.errors5xx")}
          value={metrics?.errors_5xx ?? "-"}
          sub={t("logs.errors5xxSub")}
          accent="#EF4444"
        />
      </div>

      {/* Request breakdown badges */}
      {metrics && metrics.method_breakdown && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="mb-8 max-md:mb-5 flex flex-wrap items-center gap-2"
        >
          <span className="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
            {t("logs.methods")}
          </span>
          {Object.entries(metrics.method_breakdown).map(([method, count]) => (
            <Badge
              key={method}
              variant="outline"
              className="border-white/[0.08] bg-white/[0.04] text-[11px] text-[#94A3B8] gap-1"
            >
              {method}
              <span className="font-bold text-[#F8FAFC]">{count}</span>
            </Badge>
          ))}
          {metrics.requests_per_app && Object.keys(metrics.requests_per_app).length > 0 && (
            <>
              <span className="ml-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                {t("logs.apps")}:
              </span>
              {Object.entries(metrics.requests_per_app).map(([app, count]) => (
                <Badge
                  key={app}
                  variant="outline"
                  className="border-white/[0.08] bg-white/[0.04] text-[11px] text-[#94A3B8] gap-1"
                >
                  <Server size={10} strokeWidth={1.5} />
                  {app}
                  <span className="font-bold text-[#F8FAFC]">{count}</span>
                </Badge>
              ))}
            </>
          )}
        </motion.div>
      )}

      {/* Log table */}
      <div className="overflow-hidden rounded-2xl border border-white/[0.06] bg-white/[0.02]">
        <AnimatePresence mode="wait">
          {isLoading && (
            <motion.div
              key="loading"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
            >
              <SkeletonRow />
              <SkeletonRow />
              <SkeletonRow />
              <SkeletonRow />
              <SkeletonRow />
            </motion.div>
          )}

          {!isLoading && error && (
            <motion.div
              key="error"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="px-6 py-5 text-sm text-red-400"
            >
              {t("logs.error")}: {(error as Error).message}
            </motion.div>
          )}

          {!isLoading && !error && logs && logs.length === 0 && (
            <motion.div
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="flex items-center justify-center px-6 py-12"
            >
              <div className="text-center">
                <Activity size={32} strokeWidth={1} className="mx-auto mb-3 text-[#64748B]" />
                <p className="text-sm text-[#94A3B8]">{t("logs.empty")}</p>
                <p className="mt-1 text-[12px] text-[#64748B]">
                  {t("logs.emptyDesc")}
                </p>
              </div>
            </motion.div>
          )}

          {!isLoading && !error && logs && logs.length > 0 && (
            <>
              {/* Desktop table */}
              <motion.div
                key="table"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.4, ease }}
                className="max-md:hidden"
              >
                <table className="w-full">
                  <thead>
                    <tr className="border-b border-white/[0.06]">
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[72px]">
                        Status
                      </th>
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[60px]">
                        Método
                      </th>
                      <th className="px-2 py-3 w-8"></th>
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                        Status
                      </th>
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[64px]">
                        App
                      </th>
                      <th className="px-4 py-3 text-right text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[72px]">
                        Latência
                      </th>
                      <th className="px-4 py-3 text-right text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[80px]">
                        Hora
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {logs.map((entry: LogEntry, i: number) => (
                      <>
                      <motion.tr
                        key={`${entry.timestamp}-${i}`}
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        transition={{ delay: Math.min(i * 0.02, 0.3) }}
                        className="group border-b border-white/[0.04] last:border-0 hover:bg-white/[0.03] cursor-pointer"
                        onClick={() => setExpandedRow(expandedRow === `${entry.timestamp}-${i}` ? null : `${entry.timestamp}-${i}`)}
                      >
                        <td className="px-2 py-2.5 w-8">
                          {expandedRow === `${entry.timestamp}-${i}` ? (
                            <ChevronUp size={14} className="text-[#64748B]" />
                          ) : (
                            <ChevronDown size={14} className="text-[#64748B]" />
                          )}
                        </td>
                        <td className="px-4 py-2.5">{statusBadge(entry.status)}</td>
                        <td className="px-4 py-2.5">{methodBadge(entry.method)}</td>
                        <td className="px-4 py-2.5">
                          <div className="flex items-center gap-2">
                            <span className="text-[13px] font-medium text-[#F8FAFC] font-mono">
                              {entry.path}
                            </span>
                          </div>
                        </td>
                        <td className="px-4 py-2.5">
                          {entry.app ? (
                            <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.06] bg-white/[0.04] px-2 py-0.5 text-[11px] font-medium text-[#94A3B8]">
                              <Server size={10} strokeWidth={1.5} />
                              {entry.app}
                            </span>
                          ) : (
                            <span className="text-[11px] text-[#64748B]">—</span>
                          )}
                        </td>
                        <td className="px-4 py-2.5 text-right">
                          <span className="text-[12px] font-medium tabular-nums text-[#94A3B8]">
                            {entry.latency_ms}ms
                          </span>
                        </td>
                        <td className="px-4 py-2.5 text-right">
                          <span className="text-[12px] text-[#64748B] tabular-nums">
                            {formatTime(entry.timestamp)}
                          </span>
                        </td>
                        </motion.tr>
                      {expandedRow === `${entry.timestamp}-${i}` && (
                        <tr key={`det-${i}`}>
                          <td colSpan={7} className="px-6 py-4 bg-white/[0.02] border-b border-white/[0.04]">
                            <div className="grid grid-cols-2 gap-4 text-[12px]">
                              <div>
                                <p className="text-[10px] font-semibold uppercase tracking-wider text-[#64748B] mb-1">Query</p>
                                <code className="text-[#94A3B8] break-all">{entry.query || "—"}</code>
                              </div>
                              <div>
                                <p className="text-[10px] font-semibold uppercase tracking-wider text-[#64748B] mb-1">Content-Type</p>
                                <code className="text-[#94A3B8]">{entry.content_type || "—"}</code>
                              </div>
                              <div>
                                <p className="text-[10px] font-semibold uppercase tracking-wider text-[#64748B] mb-1">Remote Addr</p>
                                <code className="text-[#94A3B8]">{entry.remote_addr || "—"}</code>
                              </div>
                              <div>
                                <p className="text-[10px] font-semibold uppercase tracking-wider text-[#64748B] mb-1">User-Agent</p>
                                <code className="text-[#94A3B8] truncate block max-w-[300px]">{entry.user_agent || "—"}</code>
                              </div>
                              {entry.req_body && (
                                <div className="col-span-2">
                                  <p className="text-[10px] font-semibold uppercase tracking-wider text-[#64748B] mb-1">Request Body</p>
                                  <pre className="text-[#A78BFA] bg-black/20 rounded-lg p-3 overflow-x-auto max-h-[200px] text-[11px] leading-relaxed">{formatBody(entry.req_body)}</pre>
                                </div>
                              )}
                              {entry.res_body && (
                                <div className="col-span-2">
                                  <p className="text-[10px] font-semibold uppercase tracking-wider text-[#64748B] mb-1">Response Body</p>
                                  <pre className="text-[#34D399] bg-black/20 rounded-lg p-3 overflow-x-auto max-h-[200px] text-[11px] leading-relaxed">{formatBody(entry.res_body)}</pre>
                                </div>
                              )}
                            </div>
                          </td>
                        </tr>
                      )}
                      </>
                    ))}
                  </tbody>
                </table>
              </motion.div>

              {/* Mobile cards */}
              <motion.div
                key="mobile-cards"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                className="md:hidden flex flex-col gap-2"
              >
                {logs.map((entry: LogEntry, i: number) => (
                  <motion.div
                    key={`${entry.timestamp}-${i}`}
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: Math.min(i * 0.02, 0.3) }}
                    className="rounded-xl border border-white/[0.06] bg-white/[0.03] p-3.5"
                  >
                    <div className="flex items-center justify-between mb-2.5">
                      <div className="flex items-center gap-2">
                        {statusBadge(entry.status)}
                        {methodBadge(entry.method)}
                      </div>
                    </div>
                    <div className="mb-2.5">
                      <span className="text-[14px] font-medium text-[#F8FAFC] font-mono leading-snug break-all">
                        {entry.path}
                      </span>
                    </div>
                    <div className="flex items-center justify-between text-[12px] text-[#64748B]">
                      <div className="flex items-center gap-2.5">
                        {entry.app ? (
                          <span className="inline-flex items-center gap-1 rounded-md border border-white/[0.06] bg-white/[0.04] px-2 py-0.5">
                            <Server size={11} strokeWidth={1.5} />
                            {entry.app}
                          </span>
                        ) : (
                          <span>—</span>
                        )}
                        <span className="tabular-nums">{entry.latency_ms}ms</span>
                      </div>
                      <span className="tabular-nums">{formatTime(entry.timestamp)}</span>
                    </div>
                  </motion.div>
                ))}
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}
