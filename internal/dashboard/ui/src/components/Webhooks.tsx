import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  CreateWebhookInput,
  CreatedWebhook,
  WebhookSubscription,
  useCreateWebhook,
  useDeleteWebhook,
  useRotateWebhookToken,
  useWebhooks,
} from "../lib/api";
import { Icon } from "@/components/ui/icon";
import { EmptyState, LoadingState } from "@/components/patterns/states";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Every HTTP method the dashboard's create form offers matches exactly what
// the backend's CreateWebhook handler accepts (internal/dashboard/webhooks_handler.go).
const METHODS = ["GET", "POST", "PUT", "PATCH"] as const;

function webhookUrl(webhookId: string, token: string): string {
  const base = typeof window !== "undefined" ? window.location.origin : "";
  return `${base}/hooks/${webhookId}/${token}`;
}

async function copyToClipboard(value: string, successMessage: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(successMessage);
  } catch {
    // Clipboard API can be unavailable (permissions, non-HTTPS context) --
    // the value is still visible in the field for manual copy, so this is
    // a silent no-op rather than a user-facing error.
  }
}

interface WebhooksProps {
  appId: string;
}

export default function Webhooks({ appId }: WebhooksProps) {
  const { t } = useTranslation();
  const { data: webhooks, isLoading } = useWebhooks(appId);
  const createWebhook = useCreateWebhook(appId);
  const rotateToken = useRotateWebhookToken(appId);
  const deleteWebhook = useDeleteWebhook(appId);

  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [method, setMethod] = useState<CreateWebhookInput["method"]>("POST");
  const [eventTypePath, setEventTypePath] = useState("");
  const [eventIdPath, setEventIdPath] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<CreatedWebhook | null>(null);

  const resetForm = () => {
    setName("");
    setMethod("POST");
    setEventTypePath("");
    setEventIdPath("");
    setFormError(null);
  };

  const submit = async () => {
    setFormError(null);
    if (!name.trim()) {
      setFormError(t("webhooks.nameRequired"));
      return;
    }
    if (!eventTypePath.trim()) {
      setFormError(t("webhooks.eventTypePathRequired"));
      return;
    }
    try {
      const created = await createWebhook.mutateAsync({
        name: name.trim(),
        method,
        event_type_path: eventTypePath.trim(),
        ...(eventIdPath.trim() ? { event_id_path: eventIdPath.trim() } : {}),
      });
      setRevealed(created);
      setShowForm(false);
      resetForm();
    } catch {
      // useCreateWebhook's onError already shows toast.error(error.message)
    }
  };

  const rotate = async (webhook: WebhookSubscription) => {
    if (!confirm(t("webhooks.rotateConfirm", { name: webhook.name }))) return;
    try {
      const { token } = await rotateToken.mutateAsync(webhook.id);
      setRevealed({ ...webhook, token });
    } catch {
      // onError already toasts
    }
  };

  const remove = (webhook: WebhookSubscription) => {
    if (!confirm(t("webhooks.deleteConfirm", { name: webhook.name }))) return;
    deleteWebhook.mutate(webhook.id);
  };

  if (isLoading) return <LoadingState />;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[11px] text-[var(--text-secondary)]">{t("webhooks.explainer")}</p>
        {!showForm && (
          <Button className="shrink-0 gap-1.5" size="sm" onClick={() => setShowForm(true)}>
            <Icon name="add" size={15} />
            {t("webhooks.addWebhook")}
          </Button>
        )}
      </div>

      {revealed && (
        <div className="flex flex-col gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3">
          <p className="text-[12px] font-medium text-[var(--text-primary)]">
            {t("webhooks.tokenRevealTitle")}
          </p>
          <p className="text-[11px] text-[var(--text-secondary)]">{t("webhooks.tokenRevealHint")}</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 overflow-x-auto rounded-md border border-[var(--border)] bg-[var(--surface)] px-2.5 py-1.5 text-[12px]">
              {webhookUrl(revealed.id, revealed.token)}
            </code>
            <Button
              variant="outline"
              size="sm"
              onClick={() => copyToClipboard(webhookUrl(revealed.id, revealed.token), t("webhooks.copySuccess"))}
            >
              <Icon name="content_copy" size={15} />
            </Button>
          </div>
          <Button variant="outline" size="sm" className="self-start" onClick={() => setRevealed(null)}>
            {t("webhooks.dismiss")}
          </Button>
        </div>
      )}

      {showForm && (
        <div className="flex flex-col gap-3 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("webhooks.namePlaceholder")}
              className="h-8 w-[200px] px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
            />
            <Select value={method} onValueChange={(v) => setMethod(v as CreateWebhookInput["method"])}>
              <SelectTrigger className="h-8 w-[110px] text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {METHODS.map((m) => (
                  <SelectItem key={m} value={m}>
                    {m}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={eventTypePath}
              onChange={(e) => setEventTypePath(e.target.value)}
              placeholder={t("webhooks.eventTypePathPlaceholder")}
              className="h-8 w-[220px] px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
            />
            <Input
              value={eventIdPath}
              onChange={(e) => setEventIdPath(e.target.value)}
              placeholder={t("webhooks.eventIdPathPlaceholder")}
              className="h-8 w-[220px] px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
            />
          </div>
          {formError && <p className="text-[12px] text-[var(--danger)]">{formError}</p>}
          <div className="flex items-center gap-2">
            <Button size="sm" disabled={createWebhook.isPending} onClick={submit}>
              {t("webhooks.save")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setShowForm(false);
                resetForm();
              }}
            >
              {t("webhooks.cancel")}
            </Button>
          </div>
        </div>
      )}

      {!webhooks?.length ? (
        <EmptyState title={t("webhooks.empty")} description={t("webhooks.emptyDesc")} />
      ) : (
        <div className="flex flex-col gap-2">
          {webhooks.map((webhook) => (
            <div
              key={webhook.id}
              className="flex flex-col gap-1 rounded-[10px] border border-[var(--border)] bg-[var(--surface)] p-3"
            >
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <span className="text-[13px] font-medium text-[var(--text-primary)]">{webhook.name}</span>
                  <span
                    className={
                      webhook.status === "active"
                        ? "rounded-full bg-[var(--success-surface)] px-2 py-0.5 text-[10px] font-medium text-[var(--success)]"
                        : "rounded-full bg-[var(--hover-surface)] px-2 py-0.5 text-[10px] font-medium text-[var(--text-secondary)]"
                    }
                  >
                    {webhook.status === "active" ? t("webhooks.statusActive") : t("webhooks.statusCapture")}
                  </span>
                  <span className="text-[11px] text-[var(--text-tertiary)]">{webhook.method}</span>
                </div>
                <div className="flex items-center gap-1">
                  <Button variant="ghost" size="sm" onClick={() => rotate(webhook)} title={t("webhooks.rotateToken")}>
                    <Icon name="refresh" size={15} />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => remove(webhook)} title={t("webhooks.delete")}>
                    <Icon name="delete" size={15} />
                  </Button>
                </div>
              </div>
              <p className="text-[11px] text-[var(--text-tertiary)]">{t("webhooks.eventTypePathLabel")}: {webhook.event_type_path}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
