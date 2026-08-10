import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ColumnDef, PolicyClause, TablePolicyRow, useApp, useCreateTablePolicy, useDeleteTablePolicy, useTablePolicies, useUpdateTablePolicy } from "../lib/api";
import { Icon } from "@/components/ui/icon";
import { EmptyState } from "@/components/patterns";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Mirrors the fixed allowlist in internal/provisioner/policy.go — the
// builder never offers free text for column/operator/logic/claim, only
// values the backend already accepts.
const OPERATORS = ["=", "!=", "IN", "NOT IN", ">", "<", ">=", "<=", "IS NULL", "IS NOT NULL"];
const UNARY_OPERATORS = new Set(["IS NULL", "IS NOT NULL"]);
const CLAIMS = ["role", "sub", "email"];
const ACTIONS = ["select", "insert", "update", "delete"];
const LOGIC_VALUES = ["AND", "OR"];

interface ClauseDraft {
  column: string;
  operator: string;
  value_source: "claim" | "literal";
  value: string;
  logic: string;
}

const emptyClause = (column: string): ClauseDraft => ({
  column,
  operator: "=",
  value_source: "claim",
  value: "",
  logic: "AND",
});

interface TablePoliciesTabProps {
  appId: string;
  tableName: string;
  columns: ColumnDef[];
}

function clauseSummary(clause: TablePolicyRow["clauses"][number]): string {
  const value =
    clause.operator === "IS NULL" || clause.operator === "IS NOT NULL"
      ? ""
      : clause.value_source === "claim"
        ? ` claim:${clause.value}`
        : ` ${clause.value}`;
  const prefix = clause.logic ? `${clause.logic} ` : "";
  return `${prefix}${clause.column} ${clause.operator}${value}`.trim();
}

export default function TablePoliciesTab({ appId, tableName, columns }: TablePoliciesTabProps) {
  const { t } = useTranslation();
  const { data: app } = useApp(appId);
  const availableRoles = app?.enduser_roles_config ?? [];
  const { data: policies, isLoading } = useTablePolicies(appId, tableName);
  const deletePolicy = useDeleteTablePolicy(appId, tableName);
  const createPolicy = useCreateTablePolicy(appId, tableName);
  const updatePolicy = useUpdateTablePolicy(appId, tableName);

  const [showForm, setShowForm] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<TablePolicyRow | null>(null);
  const firstColumn = columns[0]?.name ?? "";
  const [name, setName] = useState("");
  const [action, setAction] = useState("select");
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [clauses, setClauses] = useState<ClauseDraft[]>([emptyClause(firstColumn)]);
  const [formError, setFormError] = useState<string | null>(null);

  // Roles offered as chips: the app's configured list, plus any role
  // already selected but no longer in that list (orphaned) — always shown,
  // never silently dropped (ROLECFG-16).
  const chipRoles = Array.from(new Set([...availableRoles, ...selectedRoles]));

  const toggleRole = (role: string) =>
    setSelectedRoles((prev) => (prev.includes(role) ? prev.filter((r) => r !== role) : [...prev, role]));

  const remove = (policy: TablePolicyRow) => {
    if (!confirm(t("tablePolicies.deleteConfirm", { name: policy.pg_policy_name }))) return;
    deletePolicy.mutate(policy.id);
  };

  const resetForm = () => {
    setName("");
    setAction("select");
    setSelectedRoles([]);
    setClauses([emptyClause(firstColumn)]);
    setFormError(null);
  };

  const openForm = () => {
    resetForm();
    setEditingPolicy(null);
    setShowForm(true);
  };

  // Pre-populates the form from an existing policy — including any role no
  // longer in enduser_roles_config, which chipRoles already keeps visible
  // as a selected chip (ROLECFG-16, now exercised via edit).
  const openEditForm = (policy: TablePolicyRow) => {
    setName(policy.pg_policy_name);
    setAction(policy.action);
    setSelectedRoles(policy.roles);
    setClauses(
      policy.clauses.map((c, i) => ({
        column: c.column,
        operator: c.operator,
        value_source: c.value_source === "literal" ? "literal" : "claim",
        value: c.value ?? "",
        logic: i > 0 ? c.logic ?? "AND" : "AND",
      })),
    );
    setFormError(null);
    setEditingPolicy(policy);
    setShowForm(true);
  };

  const addClause = () => setClauses((prev) => [...prev, emptyClause(firstColumn)]);
  const removeClause = (ci: number) => setClauses((prev) => prev.filter((_, i) => i !== ci));
  const updateClause = (ci: number, patch: Partial<ClauseDraft>) =>
    setClauses((prev) => prev.map((c, i) => (i === ci ? { ...c, ...patch } : c)));

  const submit = async () => {
    setFormError(null);
    if (!name.trim()) {
      setFormError(t("tablePolicies.nameRequired"));
      return;
    }
    if (selectedRoles.length === 0) {
      setFormError(t("tablePolicies.rolesRequired"));
      return;
    }
    if (clauses.some((c) => !UNARY_OPERATORS.has(c.operator) && !c.value.trim())) {
      setFormError(t("tablePolicies.valueRequired"));
      return;
    }

    const payloadClauses: PolicyClause[] = clauses.map((c, i) => {
      const isUnary = UNARY_OPERATORS.has(c.operator);
      const base: PolicyClause = {
        column: c.column,
        operator: c.operator,
        ...(isUnary ? {} : { value_source: c.value_source, value: c.value.trim() }),
      };
      if (i > 0) base.logic = c.logic;
      return base;
    });

    const def = { name: name.trim(), action, roles: selectedRoles, clauses: payloadClauses };

    try {
      if (editingPolicy) {
        await updatePolicy.mutateAsync({ policyId: editingPolicy.id, def });
        toast.success(t("tablePolicies.updateSuccess"));
      } else {
        await createPolicy.mutateAsync(def);
        toast.success(t("tablePolicies.createSuccess"));
      }
      setShowForm(false);
      setEditingPolicy(null);
      resetForm();
    } catch {
      // useCreateTablePolicy/useUpdateTablePolicy's onError already shows toast.error(error.message)
    }
  };

  const isSaving = createPolicy.isPending || updatePolicy.isPending;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[11px] text-[var(--text-secondary)]">{t("tablePolicies.explainer")}</p>
        {!showForm && (
          <Button className="shrink-0 gap-1.5" size="sm" onClick={openForm}>
            <Icon name="add" size={15} />
            {t("tablePolicies.addPolicy")}
          </Button>
        )}
      </div>

      {showForm && (
        <div className="flex flex-col gap-3 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase().replace(/[\s-]+/g, "_"))}
              placeholder={t("tablePolicies.namePlaceholder")}
              className="h-8 w-[180px] px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
            />
            <Select value={action} onValueChange={setAction}>
              <SelectTrigger className="h-8 w-[110px] text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ACTIONS.map((a) => (
                  <SelectItem key={a} value={a} className="text-[12px]">
                    {a}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-wrap items-center gap-1.5" title={t("tablePolicies.rolesChipsHint")}>
            <span className="text-[11px] font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">
              {t("tablePolicies.rolesChipsLabel")}
            </span>
            {chipRoles.length === 0 ? (
              <span className="text-[12px] text-[var(--text-tertiary)]">{t("tablePolicies.rolesPlaceholder")}</span>
            ) : (
              chipRoles.map((role) => {
                const selected = selectedRoles.includes(role);
                return (
                  <button
                    key={role}
                    type="button"
                    onClick={() => toggleRole(role)}
                    className={
                      selected
                        ? "rounded-full border border-[var(--primary)] bg-[var(--primary)] px-2.5 py-1 text-[12px] font-semibold text-white cursor-pointer"
                        : "rounded-full border border-[var(--border)] bg-[var(--surface)] px-2.5 py-1 text-[12px] font-semibold text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
                    }
                  >
                    {role}
                  </button>
                );
              })
            )}
          </div>

          <div className="flex flex-col gap-2">
            {clauses.map((clause, ci) => (
              <div key={ci} className="flex flex-wrap items-center gap-2">
                {ci > 0 && (
                  <Select value={clause.logic} onValueChange={(val) => updateClause(ci, { logic: val })}>
                    <SelectTrigger className="h-8 w-[70px] shrink-0 text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {LOGIC_VALUES.map((l) => (
                        <SelectItem key={l} value={l} className="text-[12px]">
                          {l}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                <Select value={clause.column} onValueChange={(val) => updateClause(ci, { column: val })}>
                  <SelectTrigger className="h-8 w-[140px] shrink-0 text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
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
                <Select
                  value={clause.operator}
                  onValueChange={(val) =>
                    updateClause(ci, {
                      operator: val,
                      ...(UNARY_OPERATORS.has(val) ? { value: "" } : {}),
                    })
                  }
                >
                  <SelectTrigger className="h-8 w-[110px] shrink-0 text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {OPERATORS.map((op) => (
                      <SelectItem key={op} value={op} className="text-[12px]">
                        {op}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {!UNARY_OPERATORS.has(clause.operator) && (
                  <>
                    <Select
                      value={clause.value_source}
                      onValueChange={(val) => updateClause(ci, { value_source: val as "claim" | "literal" })}
                    >
                      <SelectTrigger className="h-8 w-[90px] shrink-0 text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="claim" className="text-[12px]">
                          {t("tablePolicies.valueSourceClaim")}
                        </SelectItem>
                        <SelectItem value="literal" className="text-[12px]">
                          {t("tablePolicies.valueSourceLiteral")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    {clause.value_source === "claim" ? (
                      <Select value={clause.value} onValueChange={(val) => updateClause(ci, { value: val })}>
                        <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-[var(--surface)] border-[var(--border)] rounded-md px-2 brand-focus">
                          <SelectValue placeholder={t("tablePolicies.claimPlaceholder")} />
                        </SelectTrigger>
                        <SelectContent>
                          {CLAIMS.map((c) => (
                            <SelectItem key={c} value={c} className="text-[12px]">
                              {c}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input
                        value={clause.value}
                        onChange={(e) => updateClause(ci, { value: e.target.value })}
                        placeholder={t("tablePolicies.valuePlaceholder")}
                        className="h-8 w-[120px] shrink-0 px-2.5 text-[13px] bg-[var(--surface)] border-[var(--border)] rounded-md brand-focus"
                      />
                    )}
                  </>
                )}
                <button
                  type="button"
                  title={t("tablePolicies.removeClause")}
                  onClick={() => removeClause(ci)}
                  disabled={clauses.length <= 1}
                  className="w-7 h-7 flex items-center justify-center rounded-md border border-[var(--danger)]/20 bg-[var(--danger-tint)] text-[var(--danger)] cursor-pointer hover:bg-[var(--danger-tint)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed shrink-0"
                >
                  <Icon name="delete" size={12} />
                </button>
              </div>
            ))}
          </div>

          <button
            type="button"
            onClick={addClause}
            disabled={columns.length === 0}
            className="flex items-center gap-1.5 text-[12px] font-semibold bg-transparent border border-[var(--border)] rounded-full px-3 py-1.5 cursor-pointer hover:bg-[var(--hover-surface)] transition-colors self-start disabled:opacity-40 disabled:cursor-not-allowed"
            style={{ color: "var(--primary)" }}
          >
            <Icon name="add" size={11} />
            {t("tablePolicies.addClause")}
          </button>

          {formError && <p className="text-xs text-[var(--danger)]">{formError}</p>}

          <div className="flex items-center justify-end gap-2 border-t border-[var(--border)] pt-3">
            <button
              type="button"
              onClick={() => {
                setShowForm(false);
                setEditingPolicy(null);
              }}
              disabled={isSaving}
              className="text-[12px] font-medium px-4 py-1.5 rounded-full border border-[var(--border)] bg-transparent text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
            >
              {t("tablePolicies.cancel")}
            </button>
            <button
              type="button"
              onClick={submit}
              disabled={isSaving}
              className="text-[12px] font-semibold px-4 py-1.5 rounded-full text-white cursor-pointer disabled:opacity-50"
              style={{ background: "var(--primary)" }}
            >
              {isSaving ? t("tablePolicies.saving") : t("tablePolicies.save")}
            </button>
          </div>
        </div>
      )}

      {!isLoading && (policies?.length ?? 0) === 0 && (
        <EmptyState
          icon="shield"
          title={t("tablePolicies.empty")}
          description={t("tablePolicies.emptyDesc")}
        />
      )}

      <div className="flex flex-col gap-2.5">
        {(policies ?? []).map((policy) => (
          <div
            key={policy.id}
            className="flex flex-col gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-3 py-2.5"
          >
            <div className="flex items-center gap-2">
              <Icon name="shield" size={13} className="shrink-0 text-[var(--text-tertiary)]" />
              <span className="text-[13px] font-semibold text-[var(--text-primary)]">
                {policy.pg_policy_name}
              </span>
              <span className="rounded-full border border-[var(--border)] px-2 py-0.5 text-[11px] uppercase text-[var(--text-secondary)]">
                {policy.action}
              </span>
              <span className="ml-auto flex gap-1">
                {policy.roles.map((r) => (
                  <span
                    key={r}
                    className="rounded-full bg-[var(--primary)]/10 px-2 py-0.5 text-[11px] text-[var(--primary)]"
                  >
                    {r}
                  </span>
                ))}
              </span>
              <button
                type="button"
                title={t("tablePolicies.edit")}
                onClick={() => openEditForm(policy)}
                className="w-7 h-7 flex items-center justify-center rounded-md border border-[var(--border)] bg-[var(--surface)] text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)] transition-colors"
              >
                <Icon name="edit" size={12} />
              </button>
              <button
                type="button"
                title={t("tablePolicies.delete")}
                onClick={() => remove(policy)}
                disabled={deletePolicy.isPending}
                className="w-7 h-7 flex items-center justify-center rounded-md border border-[var(--danger)]/20 bg-[var(--danger-tint)] text-[var(--danger)] cursor-pointer hover:bg-[var(--danger-tint)] transition-colors disabled:opacity-50"
              >
                <Icon name="delete" size={12} />
              </button>
            </div>
            <div className="flex flex-col gap-0.5 pl-[19px] font-mono text-[11px] text-[var(--text-secondary)]">
              {policy.clauses.map((clause, i) => (
                <span key={i}>{clauseSummary(clause)}</span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
