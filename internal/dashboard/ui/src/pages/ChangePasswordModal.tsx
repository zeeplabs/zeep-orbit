import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useChangeMyPassword, useChangeUserPassword } from "../lib/api";
import { Button } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { Dialog, DialogContent, DialogTitle, DialogDescription } from "@/components/ui/dialog";

interface ChangePasswordModalProps {
  open: boolean;
  onClose: () => void;
  targetUserId?: string;
  targetEmail?: string;
}

export default function ChangePasswordModal({ open, onClose, targetUserId, targetEmail }: ChangePasswordModalProps) {
  const { t } = useTranslation();
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
      setError(t("changePassword.minLength"));
      return;
    }
    if (newPassword.length < 8) {
      setError(t("changePassword.minLength"));
      return;
    }
    if (newPassword !== confirmPassword) {
      setError(t("changePassword.mismatch"));
      return;
    }
    if (!isSuperAdminAction && !currentPassword) {
      setError("Current password is required");
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
    "h-10 rounded-[10px] border border-[var(--border)] bg-[var(--surface)] text-[13px] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] w-full pl-4 pr-10 outline-none brand-focus";

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) handleClose(); }}>
      <DialogContent className="max-w-[420px] p-0 overflow-hidden">
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
                <Icon name="lock" size={18} style={{ color: "var(--primary)" }} />
              </div>
              <DialogTitle style={{ fontSize: 16, fontWeight: 700, margin: "0 0 4px" }}>
                {t("changePassword.title")}
              </DialogTitle>
              <DialogDescription style={{ fontSize: 13, color: "var(--text-muted)", margin: "0 0 20px", lineHeight: 1.4 }}>
                {isSuperAdminAction
                  ? t("changePassword.title")
                  : t("changePassword.title")}
              </DialogDescription>
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
                  <Icon name="check_circle" size={40} style={{ color: "var(--success)" }} />
                  <p style={{ fontSize: 14, fontWeight: 600, color: "var(--text-primary)" }}>
                    {t("changePassword.title")}
                  </p>
                </div>
                <Button
                  onClick={handleClose}
                  className="w-full rounded-[10px] border-0 text-white font-semibold h-10"
                  style={{
                    background: 'linear-gradient(to bottom right, var(--primary), var(--accent))',
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
                        title="Show/hide current password"
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
                        {showCurrent ? <Icon name="visibility_off" size={16} /> : <Icon name="visibility" size={16} />}
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
                      title="Show/hide new password"
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
                        {showNew ? <Icon name="visibility_off" size={16} /> : <Icon name="visibility" size={16} />}
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
                      title="Show/hide confirm password"
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
                        {showConfirm ? <Icon name="visibility_off" size={16} /> : <Icon name="visibility" size={16} />}
                    </button>
                  </div>
                </div>

                {error && (
                  <p style={{ fontSize: 12, color: "var(--danger)", background: "var(--danger-tint)", border: "1px solid var(--danger)", borderRadius: 10, padding: "8px 12px", margin: 0 }}>
                    {error}
                  </p>
                )}

                <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleClose}
                    disabled={isPending}
                    className="flex-1 rounded-[10px] border-[var(--border)] bg-[var(--surface)] text-[var(--text-secondary)] hover:bg-[var(--hover-surface)] hover:text-[var(--text-primary)] font-medium"
                  >
                    Cancelar
                  </Button>
                  <Button
                    size="sm"
                    onClick={handleSubmit}
                    disabled={isPending}
                    className="flex-1 rounded-[10px] border-0 text-white font-semibold disabled:opacity-40"
                    style={{
                      background: 'linear-gradient(to bottom right, var(--primary), var(--accent))',
                    }}
                  >
                    {isPending ? (
                      <>
                        <Icon name="progress_activity" size={14} style={{ marginRight: 6 }} className="animate-spin" />
                        Alterando...
                      </>
                    ) : (
                      "Alterar senha"
                    )}
                  </Button>
                </div>
              </div>
            )}
        </DialogContent>
    </Dialog>
  );
}
