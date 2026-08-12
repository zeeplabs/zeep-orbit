import { useTranslation } from "react-i18next";

interface HelpClause {
  logic?: "AND" | "OR";
  column: string;
  operator: string;
  valueKind: "claim" | "literal";
  value: string;
}

interface HelpExample {
  titleKey: string;
  descKey: string;
  clauses: HelpClause[];
}

// Every operator/claim below is a member of the real allowlist
// (internal/provisioner/policy.go:21-32,40-45) — no LIKE, no SQL function,
// no invented claim. Cross-checked manually against that file.
const EXAMPLES: HelpExample[] = [
  {
    titleKey: "tablePolicies.help.example1Title",
    descKey: "tablePolicies.help.example1Desc",
    clauses: [{ column: "created_by_email", operator: "=", valueKind: "claim", value: "email" }],
  },
  {
    titleKey: "tablePolicies.help.example2Title",
    descKey: "tablePolicies.help.example2Desc",
    clauses: [
      { column: "region", operator: "IN", valueKind: "literal", value: "us,eu" },
      { logic: "AND", column: "priority", operator: ">=", valueKind: "literal", value: "2" },
    ],
  },
  {
    titleKey: "tablePolicies.help.example3Title",
    descKey: "tablePolicies.help.example3Desc",
    clauses: [
      { column: "archived_at", operator: "IS NULL", valueKind: "literal", value: "" },
      { logic: "OR", column: "owner_id", operator: "=", valueKind: "claim", value: "sub" },
    ],
  },
];

const UNARY_OPERATORS = new Set(["IS NULL", "IS NOT NULL"]);

function clauseText(clause: HelpClause): string {
  const prefix = clause.logic ? `${clause.logic} ` : "";
  if (UNARY_OPERATORS.has(clause.operator)) {
    return `${prefix}${clause.column} ${clause.operator}`;
  }
  const value = clause.valueKind === "claim" ? `claim:${clause.value}` : clause.value;
  return `${prefix}${clause.column} ${clause.operator} ${value}`;
}

// PolicyHelpContent (PTPL-08): static tutorial rendered inside FormDrawer —
// no live form, no state, every example uses only the real operator/claim
// allowlist so nothing shown here would ever be rejected by BuildPolicySQL.
export function PolicyHelpContent() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-4">
      <p className="text-[13px] text-[var(--text-secondary)]">{t("tablePolicies.help.intro")}</p>

      <ul className="flex flex-col gap-1 text-[12px] text-[var(--text-secondary)]">
        <li>
          <strong className="text-[var(--text-primary)]">{t("tablePolicies.help.conceptColumnLabel")}</strong>{" "}
          {t("tablePolicies.help.conceptColumn")}
        </li>
        <li>
          <strong className="text-[var(--text-primary)]">{t("tablePolicies.help.conceptOperatorLabel")}</strong>{" "}
          {t("tablePolicies.help.conceptOperator")}
        </li>
        <li>
          <strong className="text-[var(--text-primary)]">{t("tablePolicies.help.conceptValueSourceLabel")}</strong>{" "}
          {t("tablePolicies.help.conceptValueSource")}
        </li>
        <li>
          <strong className="text-[var(--text-primary)]">{t("tablePolicies.help.conceptLogicLabel")}</strong>{" "}
          {t("tablePolicies.help.conceptLogic")}
        </li>
      </ul>

      <div className="flex flex-col gap-3">
        {EXAMPLES.map((example) => (
          <div
            key={example.titleKey}
            className="flex flex-col gap-1.5 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] p-3"
          >
            <p className="text-[13px] font-semibold text-[var(--text-primary)]">{t(example.titleKey)}</p>
            <p className="text-[12px] text-[var(--text-secondary)]">{t(example.descKey)}</p>
            <div className="flex flex-col gap-0.5 pl-1 font-mono text-[11px] text-[var(--text-secondary)]">
              {example.clauses.map((clause, i) => (
                <span key={i}>{clauseText(clause)}</span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
