import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  PersonalAccessToken,
  useCreatePAT,
  usePATs,
  useRevokePAT,
} from "../lib/api";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Icon } from "@/components/ui/icon";
import { EmptyState, LoadingState } from "@/components/patterns/states";
import { ConfirmDialog } from "@/components/patterns/ConfirmDialog";

interface PersonalAccessTokensProps {
  open: boolean;
  onClose: () => void;
}

// PersonalAccessTokens: Settings-equivalent modal for an admin's own MCP
// personal access tokens (mcp-server spec T14) — list/create/revoke,
// mirroring AppDetailsPage's per-app token list UI, following Webhooks.tsx's
// mutation + ConfirmDialog + toast.error pattern (AGENTS.md §5).
export default function PersonalAccessTokens({ open, onClose }: PersonalAccessTokensProps) {
  const { t } = useTranslation();
  const { data: allPats, isLoading } = usePATs();
  // ListPATs returns every token including revoked ones (design.md: kept
  // for auditability) — the active list only shows tokens still usable, so
  // revoking one visibly removes it here without a manual reload.
  const pats = allPats?.filter((pat) => !pat.revoked_at);
  const createPAT = useCreatePAT();
  const revokePAT = useRevokePAT();

  const [showCreateForm, setShowCreateForm] = useState(false);
  const [name, setName] = useState("");
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<PersonalAccessToken | null>(null);

  const handleClose = () => {
    setShowCreateForm(false);
    setName("");
    setCreatedToken(null);
    setCopied(false);
    onClose();
  };

  const handleCreate = async () => {
    if (!name.trim()) return;
    try {
      const result = await createPAT.mutateAsync(name.trim());
      setCreatedToken(result.token);
      setShowCreateForm(false);
      setName("");
    } catch {
      // useCreatePAT's onError already shows toast.error(error.message)
    }
  };

  const handleCopy = async () => {
    if (!createdToken) return;
    try {
      await navigator.clipboard.writeText(createdToken);
      setCopied(true);
      toast.success(t("pats.copySuccess"));
    } catch {
      // Clipboard API can be unavailable -- the token is still visible for manual copy.
    }
  };

  const confirmRevoke = () => {
    if (!revokeTarget) return;
    revokePAT.mutate(revokeTarget.id);
    setRevokeTarget(null);
  };

  return (
    <>
      <Dialog open={open} onOpenChange={(o) => { if (!o) handleClose(); }}>
        <DialogContent className="max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{t("pats.title")}</DialogTitle>
            <DialogDescription>{t("pats.explainer")}</DialogDescription>
          </DialogHeader>

          {createdToken ? (
            <div className="flex flex-col gap-3">
              <p className="text-[12px] font-semibold text-[var(--warning)]">{t("pats.createdWarning")}</p>
              <div className="flex items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
                <code className="max-h-32 flex-1 overflow-y-auto break-all font-mono text-sm text-[var(--primary)]">
                  {createdToken}
                </code>
                <Button
                  variant="outline"
                  size="icon"
                  className="size-9 shrink-0"
                  title={t("common.copyToClipboard")}
                  onClick={handleCopy}
                >
                  <Icon name="content_copy" size={16} />
                </Button>
              </div>
              {copied && <p className="text-[11px] text-[var(--success)]">{t("pats.copySuccess")}</p>}
              <Button onClick={() => setCreatedToken(null)}>{t("pats.done")}</Button>
            </div>
          ) : (
            <>
              {showCreateForm ? (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="pat-name">{t("pats.nameLabel")}</Label>
                    <Input
                      id="pat-name"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder={t("pats.namePlaceholder")}
                    />
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      className="flex-1"
                      onClick={() => { setShowCreateForm(false); setName(""); }}
                      disabled={createPAT.isPending}
                    >
                      {t("pats.cancel")}
                    </Button>
                    <Button className="flex-1" onClick={handleCreate} disabled={!name.trim() || createPAT.isPending}>
                      {createPAT.isPending ? t("pats.creating") : t("pats.create")}
                    </Button>
                  </div>
                </div>
              ) : (
                <Button className="w-fit gap-1.5" size="sm" onClick={() => setShowCreateForm(true)}>
                  <Icon name="add" size={15} />
                  {t("pats.newToken")}
                </Button>
              )}

              <div className="mt-4">
                {isLoading ? (
                  <LoadingState rows={2} />
                ) : !pats?.length ? (
                  <EmptyState icon="key" title={t("pats.empty")} description={t("pats.emptyDesc")} />
                ) : (
                  <div className="flex flex-col gap-2">
                    {pats.map((pat) => (
                      <div
                        key={pat.id}
                        className="flex items-center justify-between rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3"
                      >
                        <div className="flex min-w-0 flex-col gap-0.5">
                          <p className="truncate text-sm font-semibold text-[var(--text-primary)]">{pat.name}</p>
                          <p className="text-[11px] text-[var(--text-tertiary)]">
                            {pat.last_used_at
                              ? t("pats.lastUsed", { date: new Date(pat.last_used_at).toLocaleDateString() })
                              : t("pats.neverUsed")}
                          </p>
                        </div>
                        <Button
                          variant="outline"
                          size="icon"
                          className="size-8 shrink-0 text-[var(--text-secondary)] hover:text-[var(--danger)]"
                          title={t("pats.revoke")}
                          onClick={() => setRevokeTarget(pat)}
                        >
                          <Icon name="close" size={15} />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={revokeTarget !== null}
        title={t("pats.revokeTitle")}
        message={t("pats.revokeConfirm", { name: revokeTarget?.name ?? "" })}
        confirmLabel={t("pats.revoke")}
        cancelLabel={t("pats.cancel")}
        destructive
        icon="close"
        loading={revokePAT.isPending}
        onConfirm={confirmRevoke}
        onCancel={() => setRevokeTarget(null)}
      />
    </>
  );
}
