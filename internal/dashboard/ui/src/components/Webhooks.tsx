import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  ColumnDef,
  CreateWebhookInput,
  WebhookDelivery,
  WebhookSubscription,
  useCreateWebhook,
  useDeleteWebhook,
  useRotateWebhookToken,
  useUpdateWebhook,
  useWebhookDeliveries,
  useWebhooks,
} from "../lib/api";
import { Icon } from "@/components/ui/icon";
import { EmptyState, LoadingState } from "@/components/patterns/states";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { FormDrawer } from "@/components/patterns/FormDrawer";
import WebhookMappingEditor from "@/components/WebhookMappingEditor";

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
  tables: { name: string; columns: ColumnDef[] }[];
}

export default function Webhooks({ appId, tables }: WebhooksProps) {
  const { t } = useTranslation();
  const { data: webhooks, isLoading } = useWebhooks(appId);
  const createWebhook = useCreateWebhook(appId);
  const updateWebhook = useUpdateWebhook(appId);
  const rotateToken = useRotateWebhookToken(appId);
  const deleteWebhook = useDeleteWebhook(appId);

  const [showForm, setShowForm] = useState(false);
  const [editingWebhookId, setEditingWebhookId] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [method, setMethod] = useState<CreateWebhookInput["method"]>("POST");
  const [eventTypePath, setEventTypePath] = useState("");
  const [eventIdPath, setEventIdPath] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [expandedWebhookId, setExpandedWebhookId] = useState<string | null>(null);
  const [mappingWebhookId, setMappingWebhookId] = useState<string | null>(null);

  const resetForm = () => {
    setEditingWebhookId(null);
    setName("");
    setMethod("POST");
    setEventTypePath("");
    setEventIdPath("");
    setFormError(null);
  };

  const startEdit = (webhook: WebhookSubscription) => {
    setEditingWebhookId(webhook.id);
    setName(webhook.name);
    setMethod(webhook.method);
    setEventTypePath(webhook.event_type_path);
    setEventIdPath(webhook.event_id_path ?? "");
    setFormError(null);
    setShowForm(true);
  };

  const isSaving = createWebhook.isPending || updateWebhook.isPending;

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
      if (editingWebhookId) {
        await updateWebhook.mutateAsync({
          webhookId: editingWebhookId,
          name: name.trim(),
          method,
          event_type_path: eventTypePath.trim(),
          event_id_path: eventIdPath.trim(),
        });
      } else {
        await createWebhook.mutateAsync({
          name: name.trim(),
          method,
          event_type_path: eventTypePath.trim(),
          event_id_path: eventIdPath.trim(),
        });
      }
      setShowForm(false);
      resetForm();
    } catch {
      // useCreateWebhook/useUpdateWebhook's onError already shows toast.error(error.message)
    }
  };

  const rotate = async (webhook: WebhookSubscription) => {
    if (!confirm(t("webhooks.rotateConfirm", { name: webhook.name }))) return;
    try {
      await rotateToken.mutateAsync(webhook.id);
      toast.success(t("webhooks.rotateSuccess"));
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
        <Button
          className="shrink-0 gap-1.5"
          size="sm"
          onClick={() => {
            resetForm();
            setShowForm(true);
          }}
        >
          <Icon name="add" size={15} />
          {t("webhooks.addWebhook")}
        </Button>
      </div>

      <FormDrawer
        open={showForm}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setShowForm(false);
            resetForm();
          }
        }}
        title={editingWebhookId ? t("webhooks.editWebhook") : t("webhooks.addWebhook")}
        description={t("webhooks.explainer")}
        footer={
          <div className="flex w-full gap-2.5">
            <Button
              variant="outline"
              className="flex-1"
              onClick={() => {
                setShowForm(false);
                resetForm();
              }}
              disabled={isSaving}
            >
              {t("webhooks.cancel")}
            </Button>
            <Button className="flex-1" disabled={isSaving} onClick={submit}>
              {t("webhooks.save")}
            </Button>
          </div>
        }
      >
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="webhook-name">{t("webhooks.nameLabel")}</Label>
            <Input
              id="webhook-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("webhooks.namePlaceholder")}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="webhook-method">{t("webhooks.methodLabel")}</Label>
            <Select value={method} onValueChange={(v) => setMethod(v as CreateWebhookInput["method"])}>
              <SelectTrigger id="webhook-method">
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

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="webhook-event-type-path">{t("webhooks.eventTypePathLabel")}</Label>
            <Input
              id="webhook-event-type-path"
              value={eventTypePath}
              onChange={(e) => setEventTypePath(e.target.value)}
              placeholder={t("webhooks.eventTypePathPlaceholder")}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="webhook-event-id-path">{t("webhooks.eventIdPathLabel")}</Label>
            <Input
              id="webhook-event-id-path"
              value={eventIdPath}
              onChange={(e) => setEventIdPath(e.target.value)}
              placeholder={t("webhooks.eventIdPathPlaceholder")}
            />
          </div>

          {formError && <p className="text-[12px] text-[var(--danger)]">{formError}</p>}
        </div>
      </FormDrawer>

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
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setMappingWebhookId(mappingWebhookId === webhook.id ? null : webhook.id)}
                  >
                    {mappingWebhookId === webhook.id ? t("webhooks.hideMapping") : t("webhooks.mapWebhook")}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setExpandedWebhookId(expandedWebhookId === webhook.id ? null : webhook.id)}
                  >
                    {expandedWebhookId === webhook.id
                      ? t("webhooks.hideDeliveries")
                      : t("webhooks.viewDeliveries")}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => startEdit(webhook)} title={t("webhooks.edit")}>
                    <Icon name="edit" size={15} />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => rotate(webhook)} title={t("webhooks.rotateToken")}>
                    <Icon name="refresh" size={15} />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => remove(webhook)} title={t("webhooks.delete")}>
                    <Icon name="delete" size={15} />
                  </Button>
                </div>
              </div>
              <p className="text-[11px] text-[var(--text-tertiary)]">{t("webhooks.eventTypePathLabel")}: {webhook.event_type_path}</p>
              {webhook.token ? (
                <div className="mt-1 flex items-center gap-2">
                  <code className="flex-1 overflow-x-auto rounded-md border border-[var(--border)] bg-[var(--sunken)] px-2.5 py-1.5 text-[11px]">
                    {webhookUrl(webhook.id, webhook.token)}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => copyToClipboard(webhookUrl(webhook.id, webhook.token), t("webhooks.copySuccess"))}
                  >
                    <Icon name="content_copy" size={15} />
                  </Button>
                </div>
              ) : (
                <p className="mt-1 text-[11px] text-[var(--danger)]">{t("webhooks.tokenUnavailable")}</p>
              )}
              {mappingWebhookId === webhook.id && (
                <WebhookMappingEditor appId={appId} webhook={webhook} tables={tables} />
              )}
              {expandedWebhookId === webhook.id && (
                <WebhookDeliveryLog appId={appId} webhookId={webhook.id} />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// SUCCESS_OUTCOMES / ERROR_OUTCOMES drive the visual distinction the spec
// requires between success and error deliveries (spec P2
// dashboard-delivery-log AC1). "unmapped"/"duplicate_skipped"/"row_not_found"
// are deliberate no-ops, not failures, so they get a neutral tone.
const SUCCESS_OUTCOMES = new Set<WebhookDelivery["outcome"]>(["captured", "inserted", "updated", "deleted", "verification_challenge"]);
const ERROR_OUTCOMES = new Set<WebhookDelivery["outcome"]>(["invalid_token", "malformed", "write_error"]);

function outcomeBadgeClass(outcome: WebhookDelivery["outcome"]): string {
  if (SUCCESS_OUTCOMES.has(outcome)) {
    return "bg-[var(--success-surface)] text-[var(--success)]";
  }
  if (ERROR_OUTCOMES.has(outcome)) {
    return "bg-[var(--danger-surface,var(--danger))] text-[var(--on-danger,white)]";
  }
  return "bg-[var(--hover-surface)] text-[var(--text-secondary)]";
}

interface WebhookDeliveryLogProps {
  appId: string;
  webhookId: string;
}

// WebhookDeliveryLog: T16 — read-only list of a webhook's deliveries
// (timestamp, outcome badge, event-type value), expandable per entry to show
// the raw received payload and, on failure, the recorded error detail
// (spec P2 dashboard-delivery-log AC1/AC2).
function WebhookDeliveryLog({ appId, webhookId }: WebhookDeliveryLogProps) {
  const { t } = useTranslation();
  const { data: deliveries, isLoading } = useWebhookDeliveries(appId, webhookId);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  if (isLoading) return <LoadingState rows={3} />;

  if (!deliveries?.length) {
    return (
      <p className="mt-2 text-[11px] text-[var(--text-tertiary)]">{t("webhooks.deliveriesEmpty")}</p>
    );
  }

  return (
    <div className="mt-2 flex flex-col gap-1 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-2">
      {deliveries.map((d) => (
        <div key={d.id} className="flex flex-col gap-1">
          <button
            type="button"
            onClick={() => setExpandedId(expandedId === d.id ? null : d.id)}
            className="flex items-center justify-between gap-2 rounded-md px-2 py-1 text-left text-[12px] hover:bg-[var(--hover-surface)]"
          >
            <span className="text-[var(--text-tertiary)]">{new Date(d.received_at).toLocaleString()}</span>
            <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${outcomeBadgeClass(d.outcome)}`}>
              {t(`webhooks.outcome.${d.outcome}`)}
            </span>
            <span className="truncate font-mono text-[var(--text-secondary)]">{d.event_type_value ?? "—"}</span>
          </button>
          {expandedId === d.id && (
            <div className="flex flex-col gap-1 rounded-md border border-[var(--border)] bg-[var(--surface)] p-2 text-[11px]">
              <p className="font-medium text-[var(--text-secondary)]">{t("webhooks.rawPayload")}</p>
              <pre className="overflow-x-auto whitespace-pre-wrap font-mono">
                {JSON.stringify(d.raw_payload, null, 2)}
              </pre>
              {d.error_detail && (
                <>
                  <p className="font-medium text-[var(--danger)]">{t("webhooks.errorDetail")}</p>
                  <p className="font-mono text-[var(--danger)]">{d.error_detail}</p>
                </>
              )}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
