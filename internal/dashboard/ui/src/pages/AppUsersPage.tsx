import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import { Users, Search, X, ShieldOff, RefreshCw, CheckCircle, ArrowLeft, Loader2 } from "lucide-react";
import {
  useAppUsers,
  useDeactivateAppUser,
  useActivateAppUser,
  useResetAppUserSessions,
} from "../lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

function SkeletonRow() {
  return (
    <div className="flex items-center gap-4 border-b border-white/[0.06] px-4 py-4">
      <div className="h-4 w-56 rounded bg-white/[0.07]" />
      <div className="h-4 w-16 rounded bg-white/[0.05]" />
      <div className="h-4 w-20 rounded bg-white/[0.05]" />
      <div className="ml-auto h-4 w-24 rounded bg-white/[0.05]" />
    </div>
  );
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

function formatDateTime(iso: string | null) {
  if (!iso) return "—";
  return new Date(iso).toLocaleString("pt-BR");
}

export default function AppUsersPage() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(0);
  const pageSize = 50;

  const { data, isLoading, error } = useAppUsers(id || "", debouncedSearch || undefined, pageSize, page * pageSize);
  const deactivate = useDeactivateAppUser();
  const activate = useActivateAppUser();
  const resetSessions = useResetAppUserSessions();

  const handleSearch = () => {
    setDebouncedSearch(search);
    setPage(0);
  };

  const clearSearch = () => {
    setSearch("");
    setDebouncedSearch("");
    setPage(0);
  };

  const users = data?.data || [];
  const total = data?.total || 0;
  const providerCounts = data?.providerCounts || [];
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const currentPage = page + 1;

  return (
    <motion.div {...fadeUp}>
      {/* Header */}
      <div className="mb-8">
        <Link
          to="/apps"
          className="inline-flex items-center gap-1.5 text-[12px] text-[#94A3B8] hover:text-[#F8FAFC] no-underline mb-4 transition-colors"
        >
          <ArrowLeft size={14} />
          Voltar para Apps
        </Link>
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <span
              className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
              style={{
                borderColor: 'rgba(var(--brand-primary-rgb), 0.2)',
                backgroundColor: 'rgba(var(--brand-primary-rgb), 0.12)',
                color: 'var(--brand-light)',
              }}
            >
              <Users size={12} strokeWidth={1.5} />
              Usuários do App
            </span>
            <h2 className="mb-1.5 text-[28px] font-extrabold leading-tight">
              Usuários
            </h2>
            <p className="text-sm text-[#94A3B8]">
              Gerencie os usuários registrados neste app
            </p>
          </div>
        </div>
      </div>

      {/* Provider counts */}
      {providerCounts.length > 0 && (
        <div className="flex gap-3 mb-6 flex-wrap">
          {providerCounts.map((pc) => (
            <div
              key={pc.provider}
              className="flex items-center gap-2 rounded-xl border border-white/[0.06] bg-white/[0.03] px-4 py-2.5"
            >
              <span className="text-[11px] font-semibold uppercase tracking-wider text-[#94A3B8]">
                {pc.provider}
              </span>
              <span className="text-[18px] font-bold text-[#F8FAFC]">{pc.count}</span>
            </div>
          ))}
        </div>
      )}

      {/* Search */}
      <div className="mb-6 flex gap-2">
        <div className="relative flex-1 max-w-sm">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-[#64748B]" />
          <Input
            type="text"
            placeholder={t("appUsers.search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") handleSearch(); }}
            className="h-10 rounded-md border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] pl-9 pr-9"
          />
          {search && (
            <button
              onClick={clearSearch}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-[#64748B] hover:text-[#F8FAFC] bg-none border-none cursor-pointer"
            >
              <X size={14} />
            </button>
          )}
        </div>
        <Button
          size="sm"
          onClick={handleSearch}
          className="rounded-xl border-0 text-white font-semibold"
          style={{
            background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
          }}
        >
          Buscar
        </Button>
      </div>

      {/* Table */}
      <AnimatePresence mode="wait">
        {isLoading && (
          <motion.div
            key="loading"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="overflow-hidden rounded-2xl border border-white/[0.06] bg-white/[0.02]"
          >
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
            className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400"
          >
            Erro ao carregar usuários: {(error as Error).message}
          </motion.div>
        )}

        {!isLoading && !error && users.length === 0 && (
          <motion.div
            key="empty"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex items-center justify-center rounded-2xl border border-white/[0.06] bg-white/[0.02] px-6 py-12"
          >
            <div className="text-center">
              <Users size={32} strokeWidth={1} className="mx-auto mb-3 text-[#64748B]" />
              <p className="text-sm text-[#94A3B8]">
                {debouncedSearch ? t("appUsers.emptySearch") : t("appUsers.empty")}
              </p>
            </div>
          </motion.div>
        )}

        {!isLoading && !error && users.length > 0 && (
          <>
            {/* Desktop table */}
            <motion.div
              key="table"
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.5, ease }}
              className="max-md:hidden overflow-hidden rounded-2xl border border-white/[0.06] bg-white/[0.02]"
            >
              <table className="w-full">
                <thead>
                  <tr className="border-b border-white/[0.06]">
                    <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">Email</th>
                    <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">Provider</th>
                    <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">Status</th>
                    <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">Último acesso</th>
                    <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">Criado em</th>
                    <th className="px-4 py-3 text-right text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">Ações</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u, i) => {
                    const isDeactivating = deactivate.isPending && deactivate.variables?.userId === u.id;
                    const isActivating = activate.isPending && activate.variables?.userId === u.id;
                    const isResetting = resetSessions.isPending && resetSessions.variables?.userId === u.id;
                    return (
                      <motion.tr
                        key={u.id}
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        transition={{ delay: i * 0.03 }}
                        className="group border-b border-white/[0.04] last:border-0 hover:bg-white/[0.03]"
                      >
                        <td className="px-4 py-3.5">
                          <span className="text-[13px] font-medium text-[#F8FAFC]">{u.email}</span>
                        </td>
                        <td className="px-4 py-3.5">
                          <Badge variant="outline" className="text-[11px] border-white/[0.10] bg-white/[0.04] text-[#94A3B8]">
                            {u.provider}
                          </Badge>
                        </td>
                        <td className="px-4 py-3.5">
                          {u.active ? (
                            <Badge variant="outline" className="text-[11px] border-green-500/20 bg-green-500/[0.08] text-green-400">
                              {t("appUsers.active")}
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="text-[11px] border-red-500/20 bg-red-500/[0.08] text-red-400">
                              {t("appUsers.inactive")}
                            </Badge>
                          )}
                        </td>
                        <td className="px-4 py-3.5 text-[12px] text-[#64748B]">
                          {formatDateTime(u.last_sign_in_at)}
                        </td>
                        <td className="px-4 py-3.5 text-[12px] text-[#64748B]">
                          {formatDate(u.created_at)}
                        </td>
                        <td className="px-4 py-3.5 text-right">
                          <div className="flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                            {u.active ? (
                              <Button
                                variant="outline"
                                size="icon"
                                onClick={() => deactivate.mutate({ appId: id!, userId: u.id })}
                                disabled={deactivate.isPending}
                                title={t("appUsers.deactivateTitle")}
                                className="size-7 rounded-lg border-amber-500/20 bg-amber-500/[0.08] text-amber-400 hover:bg-amber-500/[0.14] transition-colors"
                              >
                                <ShieldOff size={12} strokeWidth={1.5} />
                              </Button>
                            ) : null}
                            {!u.active && (
                              <Button
                                variant="outline"
                                size="icon"
                                onClick={() => activate.mutate({ appId: id!, userId: u.id })}
                                title={t("appUsers.activateTitle")}
                                className="size-7 rounded-lg border-green-500/20 bg-green-500/[0.06] text-green-400 hover:bg-green-500/10 hover:text-green-400"
                              >
                                {isActivating ? <Loader2 size={12} className="animate-spin" /> : <CheckCircle size={12} />}
                              </Button>
                            )}
                            <Button
                              variant="outline"
                              size="icon"
                              onClick={() => resetSessions.mutate({ appId: id!, userId: u.id })}
                              disabled={resetSessions.isPending}
                              title={t("appUsers.resetTitle")}
                              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-[#F8FAFC]"
                            >
                              {isResetting ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
                            </Button>
                          </div>
                        </td>
                      </motion.tr>
                    );
                  })}
                </tbody>
              </table>
            </motion.div>

            {/* Mobile cards */}
            <motion.div
              key="mobile-cards"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="md:hidden flex flex-col gap-3"
            >
              {users.map((u, i) => {
                const isDeactivating = deactivate.isPending && deactivate.variables?.userId === u.id;
                const isActivating = activate.isPending && activate.variables?.userId === u.id;
                const isResetting = resetSessions.isPending && resetSessions.variables?.userId === u.id;
                return (
                  <motion.div
                    key={u.id}
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: i * 0.03 }}
                    className="rounded-xl border border-white/[0.06] bg-white/[0.03] p-4"
                  >
                    <div className="flex items-center justify-between mb-3">
                      <div>
                        <p className="text-[13px] font-medium text-[#F8FAFC]">{u.email}</p>
                        <div className="flex items-center gap-2 mt-1">
                          <Badge variant="outline" className="text-[10px] border-white/[0.10] bg-white/[0.04] text-[#94A3B8]">
                            {u.provider}
                          </Badge>
                          {u.active ? (
                            <Badge variant="outline" className="text-[10px] border-green-500/20 bg-green-500/[0.08] text-green-400">Ativo</Badge>
                          ) : (
                            <Badge variant="outline" className="text-[10px] border-red-500/20 bg-red-500/[0.08] text-red-400">Inativo</Badge>
                          )}
                        </div>
                      </div>
                    </div>
                    <p className="text-[11px] text-[#64748B] mb-3">
                      Criado: {formatDate(u.created_at)} · Último acesso: {formatDateTime(u.last_sign_in_at)}
                    </p>
                    <div className="flex gap-2">
                      {u.active ? (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => deactivate.mutate({ appId: id!, userId: u.id })}
                          disabled={deactivate.isPending}
                          className="flex-1 rounded-xl border-orange-500/20 bg-orange-500/[0.06] text-orange-400 text-[11px]"
                        >
                          {isDeactivating ? <Loader2 size={12} className="animate-spin mr-1" /> : <ShieldOff size={12} className="mr-1" />}
                          Desativar
                        </Button>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => activate.mutate({ appId: id!, userId: u.id })}
                          disabled={activate.isPending}
                          className="flex-1 rounded-xl border-green-500/20 bg-green-500/[0.06] text-green-400 text-[11px]"
                        >
                          {isActivating ? <Loader2 size={12} className="animate-spin mr-1" /> : <CheckCircle size={12} className="mr-1" />}
                          Reativar
                        </Button>
                      )}
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => resetSessions.mutate({ appId: id!, userId: u.id })}
                        disabled={resetSessions.isPending}
                        className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.04] text-[#94A3B8] text-[11px]"
                      >
                        {isResetting ? <Loader2 size={12} className="animate-spin mr-1" /> : <RefreshCw size={12} className="mr-1" />}
                        Reset Sessões
                      </Button>
                    </div>
                  </motion.div>
                );
              })}
            </motion.div>

            {/* Pagination */}
            {total > pageSize && (
              <div className="flex items-center justify-between px-4 py-3 border-t border-white/[0.06] text-[12px] text-[#64748B]">
                <span>{page * pageSize + 1}–{Math.min((page + 1) * pageSize, total)} de {total}</span>
                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={page === 0}
                    onClick={() => setPage(Math.max(0, page - 1))}
                    className="text-[12px]"
                  >
                    Anterior
                  </Button>
                  <span>{currentPage}/{totalPages}</span>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={(page + 1) * pageSize >= total}
                    onClick={() => setPage(page + 1)}
                    className="text-[12px]"
                  >
                    Próximo
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
