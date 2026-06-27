import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { motion, AnimatePresence } from "framer-motion";
import {
  Plus,
  Pencil,
  Trash2,
  Table2,
  Mail,
  MailX,
  LayoutGrid,
} from "lucide-react";
import { useApps, useDeleteApp, AppDef } from "../lib/api";
import DeleteConfirmDialog from "../components/DeleteConfirmDialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// ── Animation presets ──────────────────────────────────────────────────────────

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

// ── Skeleton card ──────────────────────────────────────────────────────────────

function SkeletonCard() {
  return (
    <div className="rounded-2xl border border-white/[0.06] bg-white/[0.03] p-4">
      <div className="relative overflow-hidden rounded-xl bg-white/[0.03]">
        <div className="absolute inset-0 animate-[shimmer_1.6s_infinite] bg-gradient-to-r from-transparent via-white/[0.04] to-transparent" />
        <div className="mb-3 h-4 w-3/5 rounded bg-white/[0.07]" />
        <div className="mb-2 h-3 w-2/5 rounded bg-white/[0.05]" />
        <div className="h-3 w-1/3 rounded bg-white/[0.05]" />
      </div>
    </div>
  );
}

// ── App card ───────────────────────────────────────────────────────────────────

interface AppCardProps {
  app: AppDef;
  index: number;
  onEdit: (app: AppDef) => void;
  onDelete: (app: AppDef) => void;
}

function AppCard({ app, index, onEdit, onDelete }: AppCardProps) {
  const createdAt = new Date(app.created_at).toLocaleDateString("pt-BR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease, delay: index * 0.07 }}
      className="group relative rounded-2xl border border-white/[0.06] bg-white/[0.03] p-4 transition-colors hover:border-white/[0.12] hover:bg-white/[0.05]"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="mb-2.5 truncate text-sm font-bold text-[#F8FAFC]">
            {app.name}
          </h3>

          <div className="flex flex-wrap gap-1.5">
            <Badge
              className="gap-1 border-[#0347A5]/20 bg-[#0347A5]/10 text-[10px] text-[#B3D1FF] hover:bg-[#0347A5]/20"
              variant="outline"
            >
              <Table2 size={10} strokeWidth={1.5} />
              {app.tables?.length ?? 0}{" "}
              {app.tables?.length === 1 ? "tabela" : "tabelas"}
            </Badge>

            <Badge
              className={cn(
                "gap-1 text-[10px]",
                app.auth_email_enabled
                  ? "border-purple-500/20 bg-[#7C3AED]/10 text-purple-300 hover:bg-[#7C3AED]/20"
                  : "border-white/[0.10] bg-white/[0.05] text-[#94A3B8] hover:bg-white/[0.08]",
              )}
              variant="outline"
            >
              {app.auth_email_enabled ? (
                <Mail size={10} strokeWidth={1.5} />
              ) : (
                <MailX size={10} strokeWidth={1.5} />
              )}
            </Badge>
          </div>
        </div>

        <div className="flex shrink-0 gap-1 transition-opacity opacity-0 group-hover:opacity-100">
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

      <p className="mt-3 text-[10px] text-[#64748B]">Criado em {createdAt}</p>
    </motion.div>
  );
}

// ── Empty state ────────────────────────────────────────────────────────────────

function EmptyState({ onCreateClick }: { onCreateClick: () => void }) {
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
            className="mx-auto mb-5 flex size-16 items-center justify-center rounded-[18px] border border-[#0347A5]/20 bg-[#0347A5]/12"
          >
            <LayoutGrid
              size={28}
              strokeWidth={1.5}
              className="text-[#B3D1FF]"
            />
          </motion.div>

          <h3 className="mb-2 text-base font-bold">Nenhum app criado</h3>
          <p className="mb-6 text-[13px] leading-relaxed text-[#94A3B8]">
            Crie seu primeiro app para gerar APIs automaticamente e gerenciar
            seus dados.
          </p>

          <motion.div
            whileHover={{ scale: 1.02 }}
            whileTap={{ scale: 0.98 }}
            className="inline-flex"
          >
            <Button
              onClick={onCreateClick}
              className="gap-2 rounded-3xl bg-gradient-to-br from-[#0347A5] to-[#7C3AED] px-[22px] py-2.5 text-sm font-semibold text-white hover:opacity-90"
            >
              Criar App
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

// ── Error state ────────────────────────────────────────────────────────────────

function ErrorState({ message }: { message: string }) {
  return (
    <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">
      Erro ao carregar apps: {message}
    </div>
  );
}

// ── Main page ──────────────────────────────────────────────────────────────────

export default function AppsPage() {
  const { data: apps, isLoading, error } = useApps();
  const deleteApp = useDeleteApp();
  const navigate = useNavigate();

  const [deleteTarget, setDeleteTarget] = useState<AppDef | null>(null);

  function handleEdit(app: AppDef) {
    navigate(`/apps/${app.id}/edit`);
  }

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    try {
      await deleteApp.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch {
      // error surfaces via deleteApp.error if needed
    }
  }

  return (
    <>
      {/* Mesh orb backdrop — fixed, behind sidebar */}
      <div
        className="pointer-events-none fixed bottom-0 right-0 top-0 z-0"
        style={{ left: 240 }}
      >
        <div className="absolute inset-0 bg-[radial-gradient(ellipse_60%_50%_at_70%_20%,rgba(3,71,165,0.08)_0%,transparent_70%),radial-gradient(ellipse_40%_40%_at_30%_70%,rgba(124,58,237,0.06)_0%,transparent_70%)]" />
      </div>

      <div className="relative z-10">
        {/* Header */}
        <motion.div {...fadeUp} className="mb-9">
          <span className="mb-3 inline-block rounded-full border border-[#0347A5]/20 bg-[#0347A5]/12 px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em] text-[#B3D1FF]">
            Apps
          </span>

          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 className="mb-1.5 text-[28px] font-extrabold leading-tight">
                Seus Aplicativos
              </h2>
              <p className="text-sm text-[#94A3B8]">
                Gerencie seus apps e APIs geradas automaticamente
              </p>
            </div>

            <motion.div
              whileHover={{ scale: 1.02 }}
              whileTap={{ scale: 0.98 }}
              className="shrink-0"
            >
              <Button
                onClick={() => navigate("/apps/new")}
                className="gap-2 rounded-3xl bg-gradient-to-br from-[#0347A5] to-[#7C3AED] px-5 py-2.5 text-sm font-semibold text-white hover:opacity-90"
              >
                Criar App
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
              className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5"
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
              className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5"
            >
              {apps.map((app, i) => (
                <AppCard
                  key={app.id}
                  app={app}
                  index={i}
                  onEdit={handleEdit}
                  onDelete={setDeleteTarget}
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
