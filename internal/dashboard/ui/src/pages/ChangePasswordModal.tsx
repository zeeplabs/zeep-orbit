import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Lock, Loader2, Eye, EyeOff, CheckCircle } from "lucide-react";
import { useChangeMyPassword, useChangeUserPassword } from "../lib/api";
import { Button } from "@/components/ui/button";

interface ChangePasswordModalProps {
  open: boolean;
  onClose: () => void;
  targetUserId?: string;
  targetEmail?: string;
}

const ease = [0.32, 0.72, 0, 1] as const;

export default function ChangePasswordModal({ open, onClose, targetUserId, targetEmail }: ChangePasswordModalProps) {
  const isSuperAdminAction = Boolean(targetUserId);
  const changeMyPassword = useChangeMyPassword();
  const changeUserPassword = useChangeUserPassword();
  const isPending = changeMyPassword.isPending || changeUserPassword.isPending;

  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  const [showCurrent, setShowCurrent] = useState(false);
  const [showNew, setShowNew] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);

  const reset = () => {
    setCurrentPassword("");
    setNewPassword("");
    setConfirmPassword("");
    setError("");
    setSuccess(false);
  };

  const handleClose = () => {
    reset();
    onClose();
  };

  const handleSubmit = async () => {
    setError("");

    if (!newPassword) {
      setError("Nova senha é obrigatória");
      return;
    }
    if (newPassword.length < 8) {
      setError("Nova senha deve ter no mínimo 8 caracteres");
      return;
    }
    if (newPassword !== confirmPassword) {
      setError("Nova senha e confirmação não conferem");
      return;
    }
    if (!isSuperAdminAction && !currentPassword) {
      setError("Senha atual é obrigatória");
      return;
    }

    try {
      if (isSuperAdminAction && targetUserId) {
        await changeUserPassword.mutateAsync({
          userId: targetUserId,
          new_password: newPassword,
          confirm_password: confirmPassword,
        });
      } else {
        await changeMyPassword.mutateAsync({
          current_password: currentPassword,
          new_password: newPassword,
          confirm_password: confirmPassword,
        });
      }
      setSuccess(true);
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const inputClass =
    "h-10 rounded-md border border-white/[0.10] bg-white/[0.06] text-[13px] text-[#F8FAFC] placeholder:text-[#64748B] w-full pl-4 pr-10 outline-none brand-focus";

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          style={{
            position: "fixed",
            inset: 0,
            zIndex: 100,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            background: "rgba(0,0,0,0.6)",
            backdropFilter: "blur(4px)",
            padding: 16,
          }}
          onClick={(e) => { if (e.target === e.currentTarget) handleClose(); }}
        >
          <motion.div
            initial={{ opacity: 0, scale: 0.95, y: 20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 20 }}
            transition={{ duration: 0.2, ease }}
            style={{
              width: "100%",
              maxWidth: 420,
              background: "var(--bg-card, #1a1a2e)",
              border: "1px solid rgba(255,255,255,0.1)",
              borderRadius: 16,
              overflow: "hidden",
            }}
          >
            <div style={{ padding: "20px 24px 0" }}>
              <div
                style={{
                  width: 44,
                  height: 44,
                  borderRadius: 12,
                  background: "rgba(var(--brand-primary-rgb), 0.12)",
                  border: "1px solid rgba(var(--brand-primary-rgb), 0.2)",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  marginBottom: 16,
                }}
              >
                <Lock size={18} style={{ color: "var(--brand-primary)" }} />
              </div>
              <h2 style={{ fontSize: 16, fontWeight: 700, margin: "0 0 4px" }}>
                {isSuperAdminAction ? `Trocar senha de ${targetEmail}` : "Alterar senha"}
              </h2>
              <p style={{ fontSize: 13, color: "var(--text-muted)", margin: "0 0 20px", lineHeight: 1.4 }}>
                {isSuperAdminAction
                  ? "Defina uma nova senha para este usuário."
                  : "Informe sua senha atual e a nova senha desejada."}
              </p>
            </div>

            {success ? (
              <div style={{ padding: "0 24px 24px" }}>
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    gap: 12,
                    padding: "24px 0",
                  }}
                >
                  <CheckCircle size={40} style={{ color: "#22c55e" }} />
                  <p style={{ fontSize: 14, fontWeight: 600, color: "#F8FAFC" }}>
                    Senha alterada com sucesso
                  </p>
                </div>
                <Button
                  onClick={handleClose}
                  className="w-full rounded-xl border-0 text-white font-semibold h-10"
                  style={{
                    background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
                  }}
                >
                  Fechar
                </Button>
              </div>
            ) : (
              <div style={{ padding: "0 24px 24px", display: "flex", flexDirection: "column", gap: 14 }}>
                {!isSuperAdminAction && (
                  <div>
                    <label style={{ fontSize: 11, fontWeight: 600, color: "var(--text-muted)", textTransform: "uppercase", letterSpacing: "0.05em", display: "block", marginBottom: 6 }}>
                      Senha atual
                    </label>
                    <div style={{ position: "relative" }}>
                      <input
                        type={showCurrent ? "text" : "password"}
                        value={currentPassword}
                        onChange={(e) => setCurrentPassword(e.target.value)}
                        placeholder="Sua senha atual"
                        className={inputClass}
                      />
                      <button
                        type="button"
                        onClick={() => setShowCurrent(!showCurrent)}
                        style={{
                          position: "absolute",
                          right: 10,
                          top: "50%",
                          transform: "translateY(-50%)",
                          background: "none",
                          border: "none",
                          color: "var(--text-muted)",
                          cursor: "pointer",
                          padding: 4,
                        }}
                      >
                        {showCurrent ? <EyeOff size={16} /> : <Eye size={16} />}
                      </button>
                    </div>
                  </div>
                )}

                <div>
                  <label style={{ fontSize: 11, fontWeight: 600, color: "var(--text-muted)", textTransform: "uppercase", letterSpacing: "0.05em", display: "block", marginBottom: 6 }}>
                    Nova senha
                  </label>
                  <div style={{ position: "relative" }}>
                    <input
                      type={showNew ? "text" : "password"}
                      value={newPassword}
                      onChange={(e) => setNewPassword(e.target.value)}
                      placeholder="Mínimo 8 caracteres"
                      className={inputClass}
                    />
                    <button
                      type="button"
                      onClick={() => setShowNew(!showNew)}
                      style={{
                        position: "absolute",
                        right: 10,
                        top: "50%",
                        transform: "translateY(-50%)",
                        background: "none",
                        border: "none",
                        color: "var(--text-muted)",
                        cursor: "pointer",
                        padding: 4,
                      }}
                    >
                      {showNew ? <EyeOff size={16} /> : <Eye size={16} />}
                    </button>
                  </div>
                </div>

                <div>
                  <label style={{ fontSize: 11, fontWeight: 600, color: "var(--text-muted)", textTransform: "uppercase", letterSpacing: "0.05em", display: "block", marginBottom: 6 }}>
                    Confirmar nova senha
                  </label>
                  <div style={{ position: "relative" }}>
                    <input
                      type={showConfirm ? "text" : "password"}
                      value={confirmPassword}
                      onChange={(e) => setConfirmPassword(e.target.value)}
                      placeholder="Repita a nova senha"
                      className={inputClass}
                    />
                    <button
                      type="button"
                      onClick={() => setShowConfirm(!showConfirm)}
                      style={{
                        position: "absolute",
                        right: 10,
                        top: "50%",
                        transform: "translateY(-50%)",
                        background: "none",
                        border: "none",
                        color: "var(--text-muted)",
                        cursor: "pointer",
                        padding: 4,
                      }}
                    >
                      {showConfirm ? <EyeOff size={16} /> : <Eye size={16} />}
                    </button>
                  </div>
                </div>

                {error && (
                  <p style={{ fontSize: 12, color: "#ef4444", background: "rgba(239,68,68,0.08)", border: "1px solid rgba(239,68,68,0.2)", borderRadius: 8, padding: "8px 12px", margin: 0 }}>
                    {error}
                  </p>
                )}

                <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleClose}
                    disabled={isPending}
                    className="flex-1 rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10] hover:text-[#F8FAFC] font-medium"
                  >
                    Cancelar
                  </Button>
                  <Button
                    size="sm"
                    onClick={handleSubmit}
                    disabled={isPending}
                    className="flex-1 rounded-xl border-0 text-white font-semibold disabled:opacity-40"
                    style={{
                      background: 'linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))',
                    }}
                  >
                    {isPending ? (
                      <>
                        <Loader2 size={14} style={{ marginRight: 6, animation: "spin 1s linear infinite" }} />
                        Alterando...
                      </>
                    ) : (
                      "Alterar senha"
                    )}
                  </Button>
                </div>
              </div>
            )}
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
