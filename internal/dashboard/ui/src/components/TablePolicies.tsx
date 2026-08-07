import { useTranslation } from "react-i18next";
import { TablePolicyRow, useDeleteTablePolicy, useTablePolicies } from "../lib/api";
import { Icon } from "@/components/ui/icon";
import { EmptyState } from "@/components/patterns";

interface TablePoliciesTabProps {
  appId: string;
  tableName: string;
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

export default function TablePoliciesTab({ appId, tableName }: TablePoliciesTabProps) {
  const { t } = useTranslation();
  const { data: policies, isLoading } = useTablePolicies(appId, tableName);
  const deletePolicy = useDeleteTablePolicy(appId, tableName);

  const remove = (policy: TablePolicyRow) => {
    if (!confirm(t("tablePolicies.deleteConfirm", { name: policy.pg_policy_name }))) return;
    deletePolicy.mutate(policy.id);
  };

  return (
    <div className="flex flex-col gap-4">
      <p className="text-[11px] text-[var(--text-secondary)]">{t("tablePolicies.explainer")}</p>

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
