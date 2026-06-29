import { useState } from "react";
import { motion } from "framer-motion";
import {
  Shield,
  Search,
  ChevronLeft,
  ChevronRight,
} from "lucide-react";
import { useAuditLog, auditActionLabel, AuditEntry, useUsers } from "../lib/api";

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

function formatTime(iso: string) {
  return new Date(iso).toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function actionBadge(action: string) {
  const isDestructive = action.includes("delete") || action.includes("deactivate");
  const isCreate = action.includes("create") || action.includes("activate");
  const isModify = action.includes("update") || action.includes("change") || action.includes("reset");
  let cls = "bg-white/[0.04] text-[#94A3B8] border-white/[0.08]";
  if (isDestructive) cls = "bg-red-500/[0.10] text-red-400 border-red-500/[0.20]";
  else if (isCreate) cls = "bg-emerald-500/[0.10] text-emerald-400 border-emerald-500/[0.20]";
  else if (isModify) cls = "bg-amber-500/[0.10] text-amber-400 border-amber-500/[0.18]";
  return (
    <span
      className={`inline-flex items-center rounded-md border px-2 py-0.5 text-[11px] font-bold whitespace-nowrap ${cls}`}
    >
      {auditActionLabel(action)}
    </span>
  );
}

function SkeletonRow() {
  return (
    <div className="flex items-center gap-3 border-b border-white/[0.04] px-4 py-3">
      <div className="h-3.5 w-28 rounded bg-white/[0.06]" />
      <div className="h-3.5 w-20 rounded bg-white/[0.06]" />
      <div className="h-3.5 w-32 rounded bg-white/[0.06]" />
      <div className="h-3.5 w-24 rounded bg-white/[0.06]" />
      <div className="ml-auto h-3.5 w-24 rounded bg-white/[0.06]" />
    </div>
  );
}

export default function AuditLogPage() {
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

  const actions = [
    "",
    "app.create", "app.update", "app.delete",
    "user.create", "user.delete",
    "user.login", "user.logout",
    "user.password.change", "config.update",
    "auth.provider.update",
    "app.user.deactivate", "app.user.activate", "app.user.sessions.reset",
    "data.create", "data.update", "data.delete",
    "bootstrap.complete",
  ];

  return (
    <div className="relative z-10">
      <motion.div {...fadeUp} className="mb-8 max-md:mb-6">
        <span
          className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          <Shield size={12} strokeWidth={1.5} />
          Auditoria
        </span>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="mb-1.5 text-[28px] max-md:text-[22px] font-extrabold leading-tight">
              Log de Auditoria
            </h2>
            <p className="text-sm max-md:text-[13px] text-[#94A3B8]">
              Histórico de todas as ações realizadas no dashboard
            </p>
          </div>

          <div className="flex items-center gap-3 max-md:w-full max-md:flex-wrap">
            <select
              value={actionFilter}
              onChange={(e) => { setActionFilter(e.target.value); setPage(0); }}
              className="h-9 rounded-xl border border-white/[0.10] bg-white/[0.06] px-3 text-[12px] text-[#F8FAFC] outline-none appearance-none cursor-pointer"
            >
              <option value="" className="bg-[#0D0D14]">Todas ações</option>
              {actions.filter(Boolean).map((a) => (
                <option key={a} value={a} className="bg-[#0D0D14]">
                  {auditActionLabel(a)}
                </option>
              ))}
            </select>

            <div className="relative">
              <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-[#64748B]" strokeWidth={1.5} />
              <select
                value={userFilter}
                onChange={(e) => { setUserFilter(e.target.value); setPage(0); }}
                className="h-9 rounded-xl border border-white/[0.10] bg-white/[0.06] pl-9 pr-3 text-[12px] text-[#F8FAFC] outline-none appearance-none cursor-pointer"
              >
                <option value="" className="bg-[#0D0D14]">Todos usuários</option>
                {userOptions.map((u) => (
                  <option key={u.id} value={u.id} className="bg-[#0D0D14]">
                    {u.email}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </div>
      </motion.div>

      <div className="overflow-hidden rounded-2xl border border-white/[0.06] bg-white/[0.02]">
        {isLoading && (
          <div>
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </div>
        )}

        {!isLoading && error && (
          <div className="px-6 py-5 text-sm text-red-400">
            Erro ao carregar auditoria: {(error as Error).message}
          </div>
        )}

        {!isLoading && !error && data && data.data.length === 0 && (
          <div className="flex items-center justify-center px-6 py-16">
            <div className="text-center">
              <Shield size={40} strokeWidth={1} className="mx-auto mb-3 text-[#64748B]" />
              <p className="text-sm text-[#94A3B8]">Nenhum registro de auditoria</p>
              <p className="mt-1 text-[12px] text-[#64748B]">
                As ações do dashboard aparecerão aqui
              </p>
            </div>
          </div>
        )}

        {!isLoading && !error && data && data.data.length > 0 && (
          <>
            <table className="w-full max-md:hidden">
              <thead>
                <tr className="border-b border-white/[0.06]">
                  <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[140px]">
                    Data/Hora
                  </th>
                  <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[140px]">
                    Ação
                  </th>
                  <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[200px]">
                    Usuário
                  </th>
                  <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[160px]">
                    Recurso
                  </th>
                  <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B] w-[120px]">
                    IP
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.data.map((entry: AuditEntry, i: number) => (
                  <tr
                    key={entry.id}
                    className="group border-b border-white/[0.04] last:border-0 hover:bg-white/[0.03]"
                  >
                    <td className="px-4 py-3">
                      <span className="text-[12px] text-[#64748B] tabular-nums">
                        {formatTime(entry.created_at)}
                      </span>
                    </td>
                    <td className="px-4 py-3">{actionBadge(entry.action)}</td>
                    <td className="px-4 py-3">
                      <span className="text-[13px] text-[#F8FAFC] font-medium">
                        {entry.user_email}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-col">
                        <span className="text-[12px] text-[#94A3B8] font-mono">
                          {entry.resource_type}
                        </span>
                        {entry.resource_name && (
                          <span className="text-[11px] text-[#64748B] truncate max-w-[160px]">
                            {entry.resource_name}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-[11px] text-[#64748B] font-mono">
                        {entry.ip_address || "—"}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            <div className="md:hidden flex flex-col gap-2 p-3">
              {data.data.map((entry: AuditEntry, i: number) => (
                <div
                  key={entry.id}
                  className="rounded-xl border border-white/[0.06] bg-white/[0.03] p-3.5"
                >
                  <div className="flex items-center justify-between mb-2">
                    {actionBadge(entry.action)}
                    <span className="text-[11px] text-[#64748B] tabular-nums">
                      {formatTime(entry.created_at)}
                    </span>
                  </div>
                  <div className="text-[13px] text-[#F8FAFC] mb-1">
                    {entry.user_email}
                  </div>
                  <div className="flex items-center gap-2 text-[11px] text-[#64748B]">
                    <span className="font-mono">{entry.resource_type}</span>
                    {entry.resource_name && <span>· {entry.resource_name}</span>}
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>

      {data && totalPages > 1 && (
        <div className="mt-4 flex items-center justify-between">
          <span className="text-[12px] text-[#64748B]">
            {data.total} registro{data.total !== 1 ? "s" : ""}
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setPage(Math.max(0, page - 1))}
              disabled={page === 0}
              className="h-8 w-8 flex items-center justify-center rounded-lg border border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:text-[#F8FAFC] disabled:opacity-30 disabled:cursor-not-allowed transition-colors cursor-pointer"
            >
              <ChevronLeft size={14} />
            </button>
            <span className="text-[12px] text-[#64748B] tabular-nums">
              {page + 1} / {totalPages}
            </span>
            <button
              onClick={() => setPage(Math.min(totalPages - 1, page + 1))}
              disabled={page >= totalPages - 1}
              className="h-8 w-8 flex items-center justify-center rounded-lg border border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:text-[#F8FAFC] disabled:opacity-30 disabled:cursor-not-allowed transition-colors cursor-pointer"
            >
              <ChevronRight size={14} />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
