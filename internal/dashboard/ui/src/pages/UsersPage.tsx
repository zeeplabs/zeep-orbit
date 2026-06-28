import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { motion, AnimatePresence } from "framer-motion";
import { Plus, Trash2, Mail, Shield, ShieldAlert, Users, Lock } from "lucide-react";
import ChangePasswordModal from "./ChangePasswordModal";
import { useUsers, useCreateUser, useDeleteUser, UserDef } from "../lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
      <div className="h-4 w-48 rounded bg-white/[0.07]" />
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

function roleBadge(role: string) {
  const isSuper = role === "superadmin";
  return (
    <Badge
      variant="outline"
      className={`gap-1.5 text-[11px] font-medium ${
        isSuper
          ? "border-purple-500/20 bg-purple-500/[0.10] text-purple-300"
          : "border-sky-500/20 bg-sky-500/[0.10] text-sky-300"
      }`}
    >
      {isSuper ? (
        <ShieldAlert size={11} strokeWidth={1.5} />
      ) : (
        <Shield size={11} strokeWidth={1.5} />
      )}
      {isSuper ? "Superadmin" : "Admin"}
    </Badge>
  );
}

interface CreateUserModalProps {
  open: boolean;
  onClose: () => void;
}

function CreateUserModal({ open, onClose }: CreateUserModalProps) {
  const createUser = useCreateUser();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("admin");
  const [error, setError] = useState("");

  async function handleSubmit() {
    setError("");
    try {
      await createUser.mutateAsync({ email, password, role });
      setEmail("");
      setPassword("");
      setRole("admin");
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(isOpen) => { if (!isOpen) onClose(); }}>
      <DialogContent className="max-w-[420px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0 [&>button]:text-[#94A3B8] [&>button]:hover:text-[#F8FAFC] [&>button]:hover:bg-white/[0.08]"
        style={{ boxShadow: '0 0 40px rgba(var(--brand-primary-rgb), 0.10)' }}
      >
        <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
          <DialogHeader className="mb-0">
            <div className="w-11 h-11 rounded-xl bg-white/[0.08] border border-white/[0.10] flex items-center justify-center mb-[18px]">
              <Users size={18} strokeWidth={1.5} className="text-[#94A3B8]" />
            </div>
            <DialogTitle className="text-base font-bold text-[#F8FAFC] mb-2">
              Novo Usuário
            </DialogTitle>
            <DialogDescription className="text-[13px] text-[#94A3B8] leading-relaxed mb-6">
              Crie um novo usuário para acessar o dashboard.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-4">
            <div>
              <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                Email
              </label>
              <Input
                type="email"
                placeholder="usuario@exemplo.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="h-10 rounded-md border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B]"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                Senha
              </label>
              <Input
                type="password"
                placeholder="Mínimo 8 caracteres"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="h-10 rounded-md border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B]"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-[12px] font-medium text-[#94A3B8] uppercase tracking-wider">
                Permissão
              </label>
              <Select value={role} onValueChange={setRole}>
                <SelectTrigger className="h-10 rounded-md border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent className="border-white/[0.10] bg-[#0D0D14]/95 backdrop-blur-xl">
                  <SelectItem value="admin" className="text-[13px] text-[#F8FAFC]">Admin</SelectItem>
                  <SelectItem value="superadmin" className="text-[13px] text-[#F8FAFC]">Superadmin</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {error && (
            <p className="mt-3 text-[12px] text-red-400">{error}</p>
          )}

          <DialogFooter className="mt-6 flex flex-row gap-2.5 sm:flex-row sm:justify-start sm:space-x-0">
            <Button
              variant="outline"
              onClick={onClose}
              disabled={createUser.isPending}
              className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
            >
              Cancelar
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={createUser.isPending}
              className="flex-1 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
              style={{
                background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
              }}
            >
              {createUser.isPending ? "Criando..." : "Criar"}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}

interface DeleteUserDialogProps {
  open: boolean;
  user: UserDef | null;
  loading: boolean;
  error: string;
  onConfirm: () => void;
  onCancel: () => void;
}

function DeleteUserDialog({ open, user, loading, error, onConfirm, onCancel }: DeleteUserDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => { if (!isOpen) onCancel(); }}>
      <DialogContent className="max-w-[420px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0 [&>button]:text-[#94A3B8] [&>button]:hover:text-[#F8FAFC] [&>button]:hover:bg-white/[0.08]"
        style={{ boxShadow: '0 0 40px rgba(var(--brand-primary-rgb), 0.10)' }}
      >
        <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
          <DialogHeader className="mb-0">
            <div className="w-11 h-11 rounded-xl bg-red-500/[0.12] border border-red-500/[0.20] flex items-center justify-center mb-[18px]">
              <Trash2 size={18} strokeWidth={1.5} className="text-red-500" />
            </div>
            <DialogTitle className="text-base font-bold text-[#F8FAFC] mb-2">
              Remover usuário?
            </DialogTitle>
            <DialogDescription className="text-[13px] text-[#94A3B8] leading-relaxed mb-6">
              O usuário <strong className="text-[#F8FAFC]">{user?.email}</strong> perderá acesso ao dashboard. Esta ação não pode ser desfeita.
            </DialogDescription>
          </DialogHeader>

          {error && (
            <p className="mb-4 px-2 text-[12px] text-red-400">{error}</p>
          )}

          <DialogFooter className="flex flex-row gap-2.5 sm:flex-row sm:justify-start sm:space-x-0">
            <Button
              variant="outline"
              onClick={onCancel}
              disabled={loading}
              className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
            >
              Cancelar
            </Button>
            <Button
              onClick={onConfirm}
              disabled={loading}
              className="flex-1 rounded-xl bg-red-500 hover:bg-red-600 text-white font-semibold border-0 disabled:bg-red-500/40"
            >
              {loading ? "Removendo..." : "Remover"}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default function UsersPage() {
  const { data: users, isLoading, error } = useUsers();
  const deleteUser = useDeleteUser();

  const { data: currentUser } = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/me", { credentials: "include" });
      if (!res.ok) return null;
      return res.json() as Promise<{ id: string; email: string; role: string }>;
    },
    retry: false,
    staleTime: 30000,
  });

  const [showCreateModal, setShowCreateModal] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<UserDef | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const [passwordTarget, setPasswordTarget] = useState<UserDef | null>(null);

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    setDeleteError("");
    try {
      await deleteUser.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch (err) {
      setDeleteError((err as Error).message);
    }
  }

  function handleCloseDelete() {
    setDeleteTarget(null);
    setDeleteError("");
  }

  return (
    <>
      <div className="relative z-10">
        {/* Header */}
        <motion.div {...fadeUp} className="mb-9">
          <span
            className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
            style={{
              borderColor: 'rgba(var(--brand-primary-rgb), 0.2)',
              backgroundColor: 'rgba(var(--brand-primary-rgb), 0.12)',
              color: 'var(--brand-light)',
            }}
          >
            <Users size={12} strokeWidth={1.5} />
            Usuários
          </span>

          <div className="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 className="mb-1.5 text-[28px] font-extrabold leading-tight">
                Gerenciar Usuários
              </h2>
              <p className="text-sm text-[#94A3B8]">
                Administre quem tem acesso ao dashboard da plataforma
              </p>
            </div>

            <motion.div
              whileHover={{ scale: 1.02 }}
              whileTap={{ scale: 0.98 }}
              className="shrink-0"
            >
              <Button
                onClick={() => setShowCreateModal(true)}
                className="gap-2 rounded-3xl px-5 py-2.5 text-sm font-semibold text-white border-0 hover:opacity-90"
                style={{
                  background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
                }}
              >
                Novo Usuário
                <span className="flex size-6 items-center justify-center rounded-full bg-white/[0.12]">
                  <Plus size={12} strokeWidth={2} />
                </span>
              </Button>
            </motion.div>
          </div>
        </motion.div>

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

          {!isLoading && !error && users && users.length === 0 && (
            <motion.div
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="flex items-center justify-center rounded-2xl border border-white/[0.06] bg-white/[0.02] px-6 py-12"
            >
              <div className="text-center">
                <Users size={32} strokeWidth={1} className="mx-auto mb-3 text-[#64748B]" />
                <p className="text-sm text-[#94A3B8]">Nenhum usuário encontrado</p>
              </div>
            </motion.div>
          )}

          {!isLoading && !error && users && users.length > 0 && (
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
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                        Email
                      </th>
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                        Permissão
                      </th>
                      <th className="px-4 py-3 text-left text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                        Criado em
                      </th>
                      <th className="px-4 py-3 text-right text-[11px] font-semibold uppercase tracking-[0.08em] text-[#64748B]">
                        Ações
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((u, i) => (
                      <motion.tr
                        key={u.id}
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        transition={{ delay: i * 0.04 }}
                        className="group border-b border-white/[0.04] last:border-0 hover:bg-white/[0.03]"
                      >
                        <td className="px-4 py-3.5">
                          <div className="flex items-center gap-2.5">
                            <div
                              className="flex size-8 items-center justify-center rounded-lg border border-white/[0.06] text-[12px] font-bold"
                              style={{
                                background: 'linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.15), rgba(var(--brand-secondary-rgb), 0.15))',
                                color: 'var(--brand-light)',
                              }}
                            >
                              {u.email.charAt(0).toUpperCase()}
                            </div>
                            <div className="flex items-center gap-2">
                              <Mail size={12} strokeWidth={1.5} className="text-[#64748B]" />
                              <span className="text-[13px] font-medium text-[#F8FAFC]">
                                {u.email}
                              </span>
                            </div>
                          </div>
                        </td>
                        <td className="px-4 py-3.5">{roleBadge(u.role)}</td>
                        <td className="px-4 py-3.5 text-[12px] text-[#64748B]">
                          {formatDate(u.created_at)}
                        </td>
                        <td className="px-4 py-3.5 text-right">
                          {currentUser && u.id !== currentUser.id && (
                            <motion.div
                              whileHover={{ scale: 1.05 }}
                              whileTap={{ scale: 0.95 }}
                              className="inline-flex"
                            >
                              <Button
                                variant="outline"
                                size="icon"
                                onClick={() => { setPasswordTarget(u); }}
                                title="Trocar senha"
                                className="size-7 rounded-lg border-white/[0.10] bg-white/[0.04] text-[#94A3B8] opacity-0 transition-opacity group-hover:opacity-100 hover:bg-white/[0.08] hover:text-[#F8FAFC] mr-1"
                              >
                                <Lock size={12} strokeWidth={1.5} />
                              </Button>
                              <Button
                                variant="outline"
                                size="icon"
                                onClick={() => { setDeleteTarget(u); setDeleteError(""); }}
                                title="Remover usuário"
                                className="size-7 rounded-lg border-red-500/20 bg-red-500/[0.06] text-red-400 opacity-0 transition-opacity group-hover:opacity-100 hover:bg-red-500/10 hover:text-red-400"
                              >
                                <Trash2 size={12} strokeWidth={1.5} />
                              </Button>
                            </motion.div>
                          )}
                        </td>
                      </motion.tr>
                    ))}
                  </tbody>
                </table>
              </motion.div>

              {/* Mobile card list */}
              <motion.div
                key="mobile-cards"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                className="md:hidden flex flex-col gap-3"
              >
                {users.map((u, i) => (
                  <motion.div
                    key={u.id}
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: i * 0.04 }}
                    className="rounded-xl border border-white/[0.06] bg-white/[0.03] p-4"
                  >
                    <div className="flex items-center justify-between mb-3">
                      <div className="flex items-center gap-3">
                        <div
                          className="flex size-10 items-center justify-center rounded-xl border border-white/[0.06] text-[14px] font-bold"
                          style={{
                            background: 'linear-gradient(to bottom right, rgba(var(--brand-primary-rgb), 0.15), rgba(var(--brand-secondary-rgb), 0.15))',
                            color: 'var(--brand-light)',
                          }}
                        >
                          {u.email.charAt(0).toUpperCase()}
                        </div>
                        <div>
                          <p className="text-[13px] font-medium text-[#F8FAFC]">{u.email}</p>
                          <p className="text-[11px] text-[#64748B] mt-0.5">{formatDate(u.created_at)}</p>
                        </div>
                      </div>
                      {currentUser && u.id !== currentUser.id && (
                        <div style={{ display: "flex", gap: 6 }}>
                          <Button
                            variant="outline"
                            size="icon"
                            onClick={() => { setPasswordTarget(u); }}
                            title="Trocar senha"
                            className="size-8 rounded-xl border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-[#F8FAFC]"
                          >
                            <Lock size={14} strokeWidth={1.5} />
                          </Button>
                          <Button
                            variant="outline"
                            size="icon"
                            onClick={() => { setDeleteTarget(u); setDeleteError(""); }}
                            title="Remover usuário"
                            className="size-8 rounded-xl border-red-500/20 bg-red-500/[0.06] text-red-400 hover:bg-red-500/10 hover:text-red-400"
                          >
                            <Trash2 size={14} strokeWidth={1.5} />
                          </Button>
                        </div>
                      )}
                    </div>
                    <div>{roleBadge(u.role)}</div>
                  </motion.div>
                ))}
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>

      {/* Create modal */}
      <CreateUserModal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
      />

      {/* Delete dialog */}
      <DeleteUserDialog
        open={Boolean(deleteTarget)}
        user={deleteTarget}
        loading={deleteUser.isPending}
        error={deleteError}
        onConfirm={handleConfirmDelete}
        onCancel={handleCloseDelete}
      />

      <ChangePasswordModal
        open={Boolean(passwordTarget)}
        onClose={() => setPasswordTarget(null)}
        targetUserId={passwordTarget?.id}
        targetEmail={passwordTarget?.email}
      />
    </>
  );
}
