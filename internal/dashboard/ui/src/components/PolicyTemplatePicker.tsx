import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ColumnDef, PolicyDef, TablePolicyRow, useCreateTablePolicy } from "../lib/api";
import { hasOwnerColumn } from "../lib/rls";
import {
  TEMPLATE_DEFINITIONS,
  TemplateId,
  buildOwnerOnlyPolicies,
  buildOpenReadPolicy,
  buildReadOnlyPolicy,
  buildValueMatchPolicy,
} from "../lib/policyTemplates";
import { RoleChipPicker } from "./RoleChipPicker";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Actions offered for owner_only (PTPL-01) — the only single-action template
// that lets the user pick more than one action; the others are fixed to
// "select" per TEMPLATE_DEFINITIONS.actionsFixed.
const OWNER_ONLY_ACTIONS = ["select", "insert", "update", "delete"];

export interface PolicyTemplatePickerProps {
  appId: string;
  tableName: string;
  rls: string;
  availableRoles: string[];
  columns: ColumnDef[];
  existingPolicies: TablePolicyRow[];
  onDone: () => void;
}

// PolicyTemplatePicker (PTPL-01/02/04/05/06/07) — the default screen for
// creating a table policy. Renders each template from TEMPLATE_DEFINITIONS,
// collects its minimal input, and applies the generated PolicyDef(s) via
// the same useCreateTablePolicy hook the advanced form already uses.
export function PolicyTemplatePicker({
  appId,
  tableName,
  rls,
  availableRoles,
  columns,
  existingPolicies,
  onDone,
}: PolicyTemplatePickerProps) {
  const { t } = useTranslation();
  const createPolicy = useCreateTablePolicy(appId, tableName);

  const [activeTemplate, setActiveTemplate] = useState<TemplateId | null>(null);
  const [ownerActions, setOwnerActions] = useState<string[]>([]);
  const [roles, setRoles] = useState<string[]>([]);
  const [valueMatchColumn, setValueMatchColumn] = useState(columns[0]?.name ?? "");
  const [valueMatchValue, setValueMatchValue] = useState("");
  const [isApplying, setIsApplying] = useState(false);

  const resetInputs = () => {
    setOwnerActions([]);
    setRoles([]);
    setValueMatchColumn(columns[0]?.name ?? "");
    setValueMatchValue("");
  };

  const toggleTemplate = (id: TemplateId) => {
    if (isApplying) return;
    if (activeTemplate === id) {
      setActiveTemplate(null);
      return;
    }
    resetInputs();
    setActiveTemplate(id);
  };

  const toggleOwnerAction = (action: string) =>
    setOwnerActions((prev) => (prev.includes(action) ? prev.filter((a) => a !== action) : [...prev, action]));

  const toggleRole = (role: string) =>
    setRoles((prev) => (prev.includes(role) ? prev.filter((r) => r !== role) : [...prev, role]));

  // Applies 1+ generated PolicyDefs sequentially, awaiting each — a failure
  // on any call surfaces via useCreateTablePolicy's own onError
  // (toast.error(error.message)) and stops the remaining calls.
  const applySequentially = async (defs: PolicyDef[]) => {
    setIsApplying(true);
    try {
      for (const def of defs) {
        await createPolicy.mutateAsync(def);
      }
      toast.success(t("tablePolicies.createSuccess"));
      setActiveTemplate(null);
      resetInputs();
      onDone();
    } catch {
      // useCreateTablePolicy's onError already showed toast.error(error.message);
      // leave the template's inputs open for retry.
    } finally {
      setIsApplying(false);
    }
  };

  const applyOwnerOnly = () => applySequentially(buildOwnerOnlyPolicies(ownerActions, roles));
  const applyOpenRead = () => applySequentially([buildOpenReadPolicy(roles)]);
  const applyReadOnly = () => applySequentially([buildReadOnlyPolicy(roles)]);
  const applyValueMatch = () =>
    applySequentially([buildValueMatchPolicy(valueMatchColumn, valueMatchValue.trim(), roles)]);

  const visibleTemplates = TEMPLATE_DEFINITIONS.filter(
    (def) => def.kind !== "composite" && (!def.requiresOwnerColumn || hasOwnerColumn(rls)),
  );

  return (
    <div className="flex flex-col gap-2.5">
      {visibleTemplates.map((def) => (
        <div
          key={def.id}
          className="flex flex-col gap-2.5 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3"
        >
          <div className="flex items-center justify-between gap-2">
            <div className="flex flex-col gap-0.5">
              <span className="text-[13px] font-semibold text-[var(--text-primary)]">
                {t(`tablePolicies.templates.${def.id}.title`)}
              </span>
              <span className="text-[11px] text-[var(--text-secondary)]">
                {t(`tablePolicies.templates.${def.id}.description`)}
              </span>
            </div>
            {def.kind !== "info" && (
              <Button
                size="sm"
                variant={activeTemplate === def.id ? "secondary" : "outline"}
                disabled={isApplying}
                onClick={() => toggleTemplate(def.id)}
              >
                {activeTemplate === def.id ? t("tablePolicies.templates.close") : t("tablePolicies.templates.use")}
              </Button>
            )}
          </div>

          {activeTemplate === def.id && def.id === "owner_only" && (
            <div className="flex flex-col gap-2.5">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-[11px] font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">
                  {t("tablePolicies.templates.actionsLabel")}
                </span>
                {OWNER_ONLY_ACTIONS.map((action) => {
                  const selected = ownerActions.includes(action);
                  return (
                    <button
                      key={action}
                      type="button"
                      onClick={() => toggleOwnerAction(action)}
                      className={
                        selected
                          ? "rounded-full border border-[var(--primary)] bg-[var(--primary)] px-2.5 py-1 text-[12px] font-semibold text-white cursor-pointer"
                          : "rounded-full border border-[var(--border)] bg-[var(--surface)] px-2.5 py-1 text-[12px] font-semibold text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
                      }
                    >
                      {action}
                    </button>
                  );
                })}
              </div>
              <RoleChipPicker
                availableRoles={availableRoles}
                selected={roles}
                onToggle={toggleRole}
                label={t("tablePolicies.rolesChipsLabel")}
                placeholder={t("tablePolicies.rolesPlaceholder")}
              />
              <Button
                size="sm"
                className="self-start"
                disabled={isApplying || ownerActions.length === 0 || roles.length === 0}
                onClick={applyOwnerOnly}
              >
                {t("tablePolicies.templates.apply")}
              </Button>
            </div>
          )}

          {activeTemplate === def.id && (def.id === "open_read" || def.id === "read_only") && (
            <div className="flex flex-col gap-2.5">
              <RoleChipPicker
                availableRoles={availableRoles}
                selected={roles}
                onToggle={toggleRole}
                label={t("tablePolicies.rolesChipsLabel")}
                placeholder={t("tablePolicies.rolesPlaceholder")}
              />
              <Button
                size="sm"
                className="self-start"
                disabled={isApplying || roles.length === 0}
                onClick={def.id === "open_read" ? applyOpenRead : applyReadOnly}
              >
                {t("tablePolicies.templates.apply")}
              </Button>
            </div>
          )}

          {activeTemplate === def.id && def.id === "value_match" && (
            <div className="flex flex-col gap-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <Select value={valueMatchColumn} onValueChange={setValueMatchColumn}>
                  <SelectTrigger className="h-8 w-[160px] shrink-0 text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                    <SelectValue placeholder={t("tablePolicies.columnPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {columns.map((c) => (
                      <SelectItem key={c.name} value={c.name} className="text-[12px]">
                        {c.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Input
                  value={valueMatchValue}
                  onChange={(e) => setValueMatchValue(e.target.value)}
                  placeholder={t("tablePolicies.valuePlaceholder")}
                  className="h-8 w-[160px] shrink-0 px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
                />
              </div>
              <RoleChipPicker
                availableRoles={availableRoles}
                selected={roles}
                onToggle={toggleRole}
                label={t("tablePolicies.rolesChipsLabel")}
                placeholder={t("tablePolicies.rolesPlaceholder")}
              />
              <Button
                size="sm"
                className="self-start"
                disabled={isApplying || !valueMatchColumn || !valueMatchValue.trim() || roles.length === 0}
                onClick={applyValueMatch}
              >
                {t("tablePolicies.templates.apply")}
              </Button>
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
