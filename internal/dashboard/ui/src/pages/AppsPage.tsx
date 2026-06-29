import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import {
  Plus,
  Pencil,
  Trash2,
  Table2,
  Mail,
  MailX,
  LayoutGrid,
  Users,
  User,
  BookOpen,
} from "lucide-react";
import { useApps, useDeleteApp, AppDef } from "../lib/api";
import DeleteConfirmDialog from "../components/DeleteConfirmDialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";


const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};


function SkeletonCard() {
  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.03] p-5">
      <div className="relative overflow-hidden rounded-xl bg-white/[0.03]">
        <div className="absolute inset-0 animate-[shimmer_1.6s_infinite] bg-gradient-to-r from-transparent via-white/[0.04] to-transparent" />
        <div className="mb-3 h-4 w-3/5 rounded bg-white/[0.07]" />
        <div className="mb-2 h-3 w-2/5 rounded bg-white/[0.05]" />
        <div className="h-3 w-1/3 rounded bg-white/[0.05]" />
      </div>
    </div>
  );
}


interface AppCardProps {
  app: AppDef;
  index: number;
  isSuperadmin?: boolean;
  onEdit: (app: AppDef) => void;
  onDelete: (app: AppDef) => void;
  onUsers: (app: AppDef) => void;
}

function AppCard({ app, index, isSuperadmin, onEdit, onDelete, onUsers }: AppCardProps) {
  const { t } = useTranslation();
  const createdAt = new Date(app.created_at).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });

  const initial = app.name.charAt(0).toUpperCase();

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease, delay: index * 0.07 }}
      className="group relative rounded-2xl border border-white/[0.06] bg-white/[0.03] p-5 transition-all duration-200 hover:bg-white/[0.06] brand-border-hover"
    >
      {/* Gradient accent bar on hover */}
      <div className="absolute left-5 right-5 top-0 h-[2px] rounded-full brand-accent-bar opacity-0 transition-opacity duration-300 group-hover:opacity-100" />

      {/* Line 1: avatar + name + badges + date */}
      <div className="flex items-center gap-3.5">
        <div
          className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-white/[0.06] text-[15px] font-extrabold"
          style={{
            background:
              "linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.15), rgba(var(--brand-secondary-rgb), 0.15))",
            color: "var(--brand-light)",
          }}
        >
          {initial}
        </div>

        <div className="min-w-0 flex-1 flex items-center justify-between gap-2">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-bold text-[#F8FAFC]">
              {app.name}
            </h3>
            <div className="flex flex-wrap gap-1.5 mt-1">
              <Badge
                className="gap-1 text-[10px]"
                variant="outline"
                style={{
                  borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
                  backgroundColor: "rgba(var(--brand-primary-rgb), 0.1)",
                  color: "var(--brand-light)",
                }}
              >
                <Table2 size={10} strokeWidth={1.5} />
                {app.tables?.length ?? 0}{" "}
                {t("apps.table", { count: app.tables?.length ?? 0 })}
              </Badge>
              <Badge
                className={cn(
                  "gap-1 text-[10px]",
                  app.auth_email_enabled
                    ? "text-purple-300 hover:bg-white/[0.08]"
                    : "border-white/[0.10] bg-white/[0.05] text-[#94A3B8] hover:bg-white/[0.08]",
                )}
                variant="outline"
                style={app.auth_email_enabled ? { borderColor: "rgba(var(--brand-secondary-rgb), 0.2)", backgroundColor: "rgba(var(--brand-secondary-rgb), 0.1)" } : undefined}
              >
                {app.auth_email_enabled ? <Mail size={10} strokeWidth={1.5} /> : <MailX size={10} strokeWidth={1.5} />}
              </Badge>
            </div>
          </div>
          <span className="text-[10px] text-[#64748B] tracking-wide shrink-0 whitespace-nowrap">{createdAt}</span>
        </div>
      </div>

      {/* Line 2: owner + actions */}
      <div className="mt-3 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          {isSuperadmin && (app.owner_name || app.owner_email) && (
            <span className="flex items-center gap-1 text-[10px] text-[#64748B] truncate">
              <User size={10} strokeWidth={1.5} className="shrink-0" />
              <span className="truncate">{app.owner_name || app.owner_email}</span>
            </span>
          )}
        </div>

        <div className="flex gap-1">
          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button
              variant="outline"
              size="icon"
              onClick={() => onUsers(app)}
              title="Usuários"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-white"
            >
              <Users size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>

          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button
              variant="outline"
              size="icon"
              onClick={() => window.open(`/docs/${app.name}`, '_blank')}
              title="API Docs (Swagger)"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-white"
            >
              <BookOpen size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>

          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button
              variant="outline"
              size="icon"
              onClick={() => onEdit(app)}
              title="Editar app"
              className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-white"
            >
              <Pencil size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>

          <motion.div whileHover={{ scale: 1.05 }} whileTap={{ scale: 0.95 }}>
            <Button
              variant="outline"
              size="icon"
              onClick={() => onDelete(app)}
              title="Deletar app"
              className="size-7 rounded-lg border-red-500/20 bg-red-500/[0.06] text-red-400 hover:bg-red-500/10 hover:text-red-400"
            >
              <Trash2 size={12} strokeWidth={1.5} />
            </Button>
          </motion.div>
        </div>
      </div>
    </motion.div>
  );
}


function EmptyState({ onCreateClick }: { onCreateClick: () => void }) {
  const { t } = useTranslation();
  return (
    <motion.div
      {...fadeUp}
      className="flex min-h-[360px] items-center justify-center"
    >
      <div className="relative w-full max-w-[380px] overflow-hidden rounded-3xl border border-white/[0.10] bg-white/[0.05] p-1.5 text-center">
        <div className="relative z-[1] rounded-[20px] bg-white/[0.03] px-8 py-10 shadow-[inset_0_1px_1px_rgba(255,255,255,0.08)]">
          <motion.div
            animate={{ y: [0, -6, 0] }}
            transition={{ duration: 2.5, repeat: Infinity, ease: "easeInOut" }}
            className="mx-auto mb-5 flex size-16 items-center justify-center rounded-[18px]"
            style={{
              borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
              backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            }}
          >
            <LayoutGrid
              size={28}
              strokeWidth={1.5}
              style={{ color: "var(--brand-light)" }}
            />
          </motion.div>

          <h3 className="mb-2 text-base font-bold">{t("apps.emptyTitle")}</h3>
          <p className="mb-6 text-[13px] leading-relaxed text-[#94A3B8]">
            {t("apps.emptyDesc")}
          </p>

          <motion.div
            whileHover={{ scale: 1.02 }}
            whileTap={{ scale: 0.98 }}
            className="inline-flex"
          >
            <Button
              onClick={onCreateClick}
              className="gap-2 rounded-3xl px-[22px] py-2.5 text-sm font-semibold text-white border-0 hover:opacity-90"
              style={{
                background:
                  "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
              }}
            >
              {t("apps.create")}
              <span className="flex size-[22px] items-center justify-center rounded-full bg-white/[0.15]">
                <Plus size={12} strokeWidth={2} />
              </span>
            </Button>
          </motion.div>
        </div>
      </div>
    </motion.div>
  );
}


function ErrorState({ message }: { message: string }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">
      {t("apps.error")}: {message}
    </div>
  );
}


export default function AppsPage() {
  const { t } = useTranslation();
  const { data: apps, isLoading, error } = useApps();
  const deleteApp = useDeleteApp();
  const navigate = useNavigate();
  const { data: me } = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/me', { credentials: 'include' })
      if (!res.ok) return null
      return res.json() as Promise<{ id: string; email: string; name: string; role: string; language: string }>
    },
    retry: false,
  })
  const isSuperadmin = me?.role === 'superadmin'

  const [deleteTarget, setDeleteTarget] = useState<AppDef | null>(null);

  function handleEdit(app: AppDef) {
    navigate(`/apps/${app.id}/edit`);
  }

  function handleUsers(app: AppDef) {
    navigate(`/apps/${app.id}/users`);
  }

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    try {
      await deleteApp.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch {
    }
  }

  return (
    <>
      <div className="relative z-10">
        {/* Header */}
        <motion.div {...fadeUp} className="mb-9">
          <span
            className="mb-3 inline-block rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
            style={{
              borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
              backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
              color: "var(--brand-light)",
            }}
          >
            Apps
          </span>

          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 className="mb-1.5 text-[28px] font-extrabold leading-tight">
                {t("apps.title")}
              </h2>
              <p className="text-sm text-[#94A3B8]">
                {t("apps.subtitle")}
              </p>
            </div>

            <motion.div
              whileHover={{ scale: 1.02 }}
              whileTap={{ scale: 0.98 }}
              className="shrink-0"
            >
              <Button
                onClick={() => navigate("/apps/new")}
                className="gap-2 rounded-3xl px-5 py-2.5 text-sm font-semibold text-white border-0 hover:opacity-90"
                style={{
                  background:
                    "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
                }}
              >
                {t("apps.create")}
                <span className="flex size-6 items-center justify-center rounded-full bg-white/[0.12]">
                  <Plus size={12} strokeWidth={2} />
                </span>
              </Button>
            </motion.div>
          </div>
        </motion.div>

        {/* Content */}
        <AnimatePresence mode="wait">
          {isLoading && (
            <motion.div
              key="loading"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-4"
            >
              <style>{`@keyframes shimmer { 0%{transform:translateX(-100%)} 100%{transform:translateX(100%)} }`}</style>
              <SkeletonCard />
              <SkeletonCard />
              <SkeletonCard />
              <SkeletonCard />
            </motion.div>
          )}

          {!isLoading && error && (
            <motion.div
              key="error"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
            >
              <ErrorState message={(error as Error).message} />
            </motion.div>
          )}

          {!isLoading && !error && apps && apps.length === 0 && (
            <motion.div
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
            >
              <EmptyState onCreateClick={() => navigate("/apps/new")} />
            </motion.div>
          )}

          {!isLoading && !error && apps && apps.length > 0 && (
            <motion.div
              key="grid"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-4"
            >
              {apps.map((app, i) => (
                <AppCard
                  key={app.id}
                  app={app}
                  index={i}
                  isSuperadmin={isSuperadmin}
                  onEdit={handleEdit}
                  onDelete={setDeleteTarget}
                  onUsers={handleUsers}
                />
              ))}
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* Delete dialog */}
      <DeleteConfirmDialog
        open={Boolean(deleteTarget)}
        appName={deleteTarget?.name ?? ""}
        loading={deleteApp.isPending}
        onConfirm={handleConfirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  );
}
