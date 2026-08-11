import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  ColumnDef,
  WebhookEventMapping,
  WebhookFieldMapping,
  WebhookSubscription,
  useActivateWebhook,
  useDeleteEventMapping,
  useEventMappings,
  useSaveEventMapping,
  useTablePolicies,
} from "../lib/api";
import { Icon } from "@/components/ui/icon";
import { EmptyState } from "@/components/patterns/states";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const ACTIONS = ["insert", "update", "delete"] as const;

interface FlatField {
  path: string;
  preview: string;
}

// Flattens the captured sample into dot-notation paths, mirroring the
// backend's ExtractPath resolution exactly (internal/webhookengine/mapping.go):
// nested object keys join with ".", array indices are numeric segments.
function flatten(value: unknown, prefix: string, out: FlatField[]): void {
  if (value === null || value === undefined) {
    out.push({ path: prefix, preview: "null" });
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((v, i) => flatten(v, prefix ? `${prefix}.${i}` : String(i), out));
    return;
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) {
      out.push({ path: prefix, preview: "{}" });
      return;
    }
    for (const [k, v] of entries) {
      flatten(v, prefix ? `${prefix}.${k}` : k, out);
    }
    return;
  }
  out.push({ path: prefix, preview: JSON.stringify(value) });
}

interface WebhookMappingEditorProps {
  appId: string;
  webhook: WebhookSubscription;
  tables: { name: string; columns: ColumnDef[] }[];
}

export default function WebhookMappingEditor({ appId, webhook, tables }: WebhookMappingEditorProps) {
  const { t } = useTranslation();
  const { data: mappings } = useEventMappings(appId, webhook.id);
  const saveMapping = useSaveEventMapping(appId, webhook.id);
  const deleteMapping = useDeleteEventMapping(appId, webhook.id);
  const activateWebhook = useActivateWebhook(appId);

  const fields = useMemo(() => {
    const out: FlatField[] = [];
    if (webhook.captured_sample) flatten(webhook.captured_sample, "", out);
    return out;
  }, [webhook.captured_sample]);

  const [eventTypeValue, setEventTypeValue] = useState("");
  const [action, setAction] = useState<(typeof ACTIONS)[number]>("insert");
  const [targetTable, setTargetTable] = useState(tables[0]?.name ?? "");
  const [links, setLinks] = useState<WebhookFieldMapping[]>([]);
  const [matchKeyColumn, setMatchKeyColumn] = useState("");
  const [pendingPath, setPendingPath] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const selectedTable = tables.find((tb) => tb.name === targetTable);
  const requiresMatchKey = action !== "insert";

  const { data: targetTablePolicies } = useTablePolicies(appId, targetTable);
  // Native RLS only turns on for a table once it has at least one policy
  // (table_policies_store.go) — with zero policies the webhook role's write
  // is unrestricted, so there's nothing to warn about there. With at least
  // one policy, the webhook role needs its own matching policy for this
  // mapping's action, or its writes will 500 at delivery time.
  const webhookRoleMissingPolicy =
    (targetTablePolicies?.length ?? 0) > 0 &&
    !targetTablePolicies?.some((p) => p.action === action && p.roles.includes("webhook"));

  const pickField = (path: string) => setPendingPath(path);

  const pickColumn = (column: string) => {
    if (!pendingPath) return;
    setLinks((prev) => [...prev.filter((l) => l.column !== column), { source_path: pendingPath, column }]);
    setPendingPath(null);
  };

  const removeLink = (column: string) => {
    setLinks((prev) => prev.filter((l) => l.column !== column));
    if (matchKeyColumn === column) setMatchKeyColumn("");
  };

  const resetForm = () => {
    setEventTypeValue("");
    setAction("insert");
    setLinks([]);
    setMatchKeyColumn("");
    setPendingPath(null);
    setFormError(null);
  };

  const submit = async () => {
    setFormError(null);
    if (!eventTypeValue.trim()) {
      setFormError(t("webhookMapping.eventTypeValueRequired"));
      return;
    }
    if (!targetTable) {
      setFormError(t("webhookMapping.targetTableRequired"));
      return;
    }
    if (links.length === 0) {
      setFormError(t("webhookMapping.fieldMappingRequired"));
      return;
    }
    if (requiresMatchKey && !matchKeyColumn) {
      setFormError(t("webhookMapping.matchKeyRequired"));
      return;
    }

    try {
      await saveMapping.mutateAsync({
        event_type_value: eventTypeValue.trim(),
        action,
        target_table: targetTable,
        ...(requiresMatchKey ? { match_key_column: matchKeyColumn } : {}),
        field_mappings: links,
      });
      toast.success(t("webhookMapping.saveSuccess"));
      resetForm();
    } catch {
      // useSaveEventMapping's onError already shows toast.error(error.message)
    }
  };

  const activate = async () => {
    try {
      await activateWebhook.mutateAsync(webhook.id);
      toast.success(t("webhookMapping.activateSuccess"));
    } catch {
      // onError already toasts
    }
  };

  const hasMappings = (mappings?.length ?? 0) > 0;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[11px] text-[var(--text-secondary)]">{t("webhookMapping.explainer")}</p>
        <Button
          size="sm"
          disabled={!hasMappings || webhook.status === "active" || activateWebhook.isPending}
          onClick={activate}
        >
          <Icon name="check" size={15} />
          {webhook.status === "active" ? t("webhookMapping.activated") : t("webhookMapping.activate")}
        </Button>
      </div>

      {!webhook.captured_sample ? (
        <EmptyState
          title={t("webhookMapping.noSample")}
          description={t("webhookMapping.noSampleDesc")}
        />
      ) : (
        <div className="grid grid-cols-2 gap-3">
          <div className="flex flex-col gap-1 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-2">
            <p className="text-[11px] font-medium text-[var(--text-secondary)]">{t("webhookMapping.sampleFields")}</p>
            {fields.map((f) => (
              <button
                key={f.path}
                type="button"
                onClick={() => pickField(f.path)}
                className={
                  "flex items-center justify-between gap-2 rounded-md px-2 py-1 text-left text-[12px] hover:bg-[var(--hover-surface)] " +
                  (pendingPath === f.path ? "bg-[var(--hover-surface)] font-medium" : "")
                }
              >
                <span className="truncate font-mono">{f.path}</span>
                <span className="truncate text-[var(--text-tertiary)]">{f.preview}</span>
              </button>
            ))}
          </div>
          <div className="flex flex-col gap-1 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-2">
            <p className="text-[11px] font-medium text-[var(--text-secondary)]">{t("webhookMapping.tableColumns")}</p>
            {(selectedTable?.columns ?? []).map((c) => {
              const link = links.find((l) => l.column === c.name);
              return (
                <button
                  key={c.name}
                  type="button"
                  onClick={() => (link ? removeLink(c.name) : pickColumn(c.name))}
                  disabled={!link && !pendingPath}
                  className="flex items-center justify-between gap-2 rounded-md px-2 py-1 text-left text-[12px] hover:bg-[var(--hover-surface)] disabled:opacity-50"
                >
                  <span className="font-mono">{c.name}</span>
                  {link ? (
                    <span className="truncate text-[var(--brand)]">{link.source_path}</span>
                  ) : (
                    <span className="text-[var(--text-tertiary)]">{t("webhookMapping.clickToLink")}</span>
                  )}
                </button>
              );
            })}
          </div>
        </div>
      )}

      <div className="flex flex-col gap-3 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3">
        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={eventTypeValue}
            onChange={(e) => setEventTypeValue(e.target.value)}
            placeholder={t("webhookMapping.eventTypeValuePlaceholder")}
            className="h-8 w-[200px] px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
          />
          <Select value={action} onValueChange={(v) => setAction(v as (typeof ACTIONS)[number])}>
            <SelectTrigger className="h-8 w-[110px] text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ACTIONS.map((a) => (
                <SelectItem key={a} value={a}>
                  {a}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={targetTable} onValueChange={setTargetTable}>
            <SelectTrigger className="h-8 w-[160px] text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {tables.map((tb) => (
                <SelectItem key={tb.name} value={tb.name}>
                  {tb.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {requiresMatchKey && (
            <Select value={matchKeyColumn} onValueChange={setMatchKeyColumn}>
              <SelectTrigger className="h-8 w-[160px] text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                <SelectValue placeholder={t("webhookMapping.matchKeyPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {links.map((l) => (
                  <SelectItem key={l.column} value={l.column}>
                    {l.column}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
        {webhookRoleMissingPolicy && (
          <p className="text-[12px] text-[var(--warning)]">
            {t("webhookMapping.policyWarning", { table: targetTable, action })}
          </p>
        )}
        {formError && <p className="text-[12px] text-[var(--danger)]">{formError}</p>}
        <div>
          <Button size="sm" disabled={saveMapping.isPending} onClick={submit}>
            {t("webhookMapping.saveMapping")}
          </Button>
        </div>
      </div>

      {mappings && mappings.length > 0 && (
        <div className="flex flex-col gap-2">
          {mappings.map((m: WebhookEventMapping) => (
            <div
              key={m.id}
              className="flex flex-col gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--surface)] p-2.5 text-[12px]"
            >
              <div className="flex items-center justify-between gap-2">
                <div>
                  <span className="font-medium">{m.event_type_value}</span>
                  <span className="text-[var(--text-tertiary)]"> — {m.action} → {m.target_table}</span>
                </div>
                <Button variant="ghost" size="sm" onClick={() => deleteMapping.mutate(m.id)}>
                  <Icon name="delete" size={15} />
                </Button>
              </div>
              <div className="flex flex-col gap-1 rounded-md border border-[var(--border)] bg-[var(--sunken)] p-2">
                {m.field_mappings.map((fm) => (
                  <div key={fm.column} className="flex items-center gap-1.5 font-mono text-[11px]">
                    <span className="truncate text-[var(--text-secondary)]">{fm.source_path}</span>
                    <Icon name="arrow_forward" size={12} className="shrink-0 text-[var(--text-tertiary)]" />
                    <span className="truncate text-[var(--brand)]">{fm.column}</span>
                    {m.match_key_column === fm.column && (
                      <span className="shrink-0 rounded-full bg-[var(--accent-tint)] px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wide text-[var(--accent)]">
                        {t("webhookMapping.matchKeyBadge")}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
