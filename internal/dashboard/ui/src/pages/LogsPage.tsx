import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
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
      className="rounded-2xl border border-white/[0.06] bg-white/[0.02] p-5"
    >
      <div className="mb-3 flex items-center gap-2.5">
        <div
          className="flex size-9 items-center justify-center rounded-xl border"
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
      <div className="text-[28px] font-extrabold leading-none tracking-tight text-[#F8FAFC]">
        {value}
      </div>
      {sub && (
        <div className="mt-1.5 text-[12px] text-[#64748B]">{sub}</div>
      )}
    </motion.div>
  );
}

export default function LogsPage() {
  const [appFilter, setAppFilter] = useState("");
  const { data: logs, isLoading, error } = useLogs(appFilter || undefined);
  const { data: metrics } = useLogMetrics();
  const { data: apps } = useApps();

  return (
    <div className="relative z-10">
      {/* Header */}
      <motion.div {...fadeUp} className="mb-8">
        <span
          className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          <Activity size={12} strokeWidth={1.5} />
          Logs
        </span>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="mb-1.5 text-[28px] font-extrabold leading-tight">
              Monitoramento
            </h2>
            <p className="text-sm text-[#94A3B8]">
              Requisições em tempo real e métricas agregadas da plataforma
            </p>
          </div>

          <div className="flex items-center gap-3">
            {metrics && (
              <div className="flex items-center gap-1.5 rounded-full border border-emerald-500/[0.20] bg-emerald-500/[0.08] px-3 py-1">
                <span className="relative flex size-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex size-2 rounded-full bg-emerald-500" />
                </span>
                <span className="text-[11px] font-medium text-emerald-400">
                  Ao vivo
                </span>
              </div>
            )}

            <select
              value={appFilter}
              onChange={(e) => setAppFilter(e.target.value)}
              className="h-9 rounded-xl border border-white/[0.10] bg-white/[0.06] px-3 text-[12px] text-[#F8FAFC] outline-none appearance-none cursor-pointer"
            >
              <option value="" className="bg-[#0D0D14]">Todos os apps</option>
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
      <div className="mb-8 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MetricCard
          icon={<BarChart3 size={15} strokeWidth={1.5} />}
          label="Requisições (1min)"
          value={metrics?.total_requests ?? "-"}
          sub="Total no último minuto"
          accent="var(--brand-primary)"
        />
        <MetricCard
          icon={<Clock size={15} strokeWidth={1.5} />}
          label="Latência Média"
          value={metrics?.avg_latency_ms != null ? `${metrics.avg_latency_ms}ms` : "-"}
          sub="Média do último minuto"
          accent="#06B6D4"
        />
        <MetricCard
          icon={<AlertTriangle size={15} strokeWidth={1.5} />}
          label="Erros 4xx"
          value={metrics?.errors_4xx ?? "-"}
          sub="No último minuto"
          accent="#F59E0B"
        />
        <MetricCard
          icon={<XCircle size={15} strokeWidth={1.5} />}
          label="Erros 5xx"
          value={metrics?.errors_5xx ?? "-"}
          sub="No último minuto"
          accent="#EF4444"
        />
      </div>

      {/* Request breakdown badges */}
      {metrics && metrics.method_breakdown && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="mb-8 flex flex-wrap items-center gap-2"
        >
          <span className="text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
            Métodos:
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
                Apps:
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
              Erro ao carregar logs: {(error as Error).message}
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
                <p className="text-sm text-[#94A3B8]">Nenhuma requisição registrada</p>
                <p className="mt-1 text-[12px] text-[#64748B]">
                  As requisições aparecerão aqui em tempo real
                </p>
              </div>
            </motion.div>
          )}

          {!isLoading && !error && logs && logs.length > 0 && (
            <motion.div
              key="table"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.4, ease }}
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
                    <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                      Path
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
                    <motion.tr
                      key={`${entry.timestamp}-${i}`}
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      transition={{ delay: Math.min(i * 0.02, 0.3) }}
                      className="group border-b border-white/[0.04] last:border-0 hover:bg-white/[0.03]"
                    >
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
                  ))}
                </tbody>
              </table>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}
