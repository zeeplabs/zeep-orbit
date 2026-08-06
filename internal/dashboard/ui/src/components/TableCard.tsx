import { useState } from "react";
import { useTranslation, Trans } from "react-i18next";
import { TableDef, ColumnDef, IndexDef, ReferenceDef } from "../lib/api";
import { cn } from "@/lib/utils";
import { Icon } from "@/components/ui/icon";
import { StatusPill } from "@/components/patterns";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

const COLUMN_TYPES = [
  "text",
  "integer",
  "bigint",
  "boolean",
  "uuid",
  "timestamptz",
  "numeric",
  "jsonb",
];

// Mirrors the allowlist in internal/config/validate.go (defaultExpressions)
// — the only SQL expressions the backend accepts for a column default. Keep
// these two lists in sync; the backend rejects anything not on its own copy
// regardless of what this UI offers.
const DEFAULT_EXPRESSIONS: Record<string, string> = {
  uuid: "gen_random_uuid()",
  timestamptz: "now()",
};

// Strips a numeric default's input down to what the column type can
// actually hold, as the user types — an optional leading "-" and, for
// numeric only, a single ".". The backend (internal/config/validate.go)
// still re-validates on submit; this is just to stop obviously-wrong
// characters (letters, a second "-"/".") from ever being typed.
const sanitizeNumericDefault = (type: string, raw: string): string => {
  let v = raw.replace(type === "numeric" ? /[^0-9.-]/g : /[^0-9-]/g, "");
  v = v.length > 0 ? v[0].replace(/[^0-9-]/, "") + v.slice(1).replace(/-/g, "") : v;
  if (type === "numeric") {
    const firstDot = v.indexOf(".");
    if (firstDot !== -1) v = v.slice(0, firstDot + 1) + v.slice(firstDot + 1).replace(/\./g, "");
  }
  return v;
};

const ON_DELETE_VALUES: NonNullable<ReferenceDef["on_delete"]>[] = ["no_action", "cascade", "restrict", "set_null"];
const ON_DELETE_LABEL_KEYS: Record<string, string> = {
  no_action: "tableCard.onDeleteNoAction",
  cascade: "tableCard.onDeleteCascade",
  restrict: "tableCard.onDeleteRestrict",
  set_null: "tableCard.onDeleteSetNull",
};

const BASE_AUTO_COLUMNS = [
  { name: "id", type: "uuid", required: true, unique: true },
  { name: "created_at", type: "timestamptz", required: true, unique: false },
  { name: "updated_at", type: "timestamptz", required: true, unique: false },
];

const OWNER_ID_AUTO_COLUMN = { name: "owner_id", type: "uuid", required: true, unique: false };

// owner_id só existe quando RLS está ativo (provisioner.go: rls == "owner" || rls == "enabled").
const autoColumnsFor = (rls: string) =>
  rls === "enabled" || rls === "owner" ? [...BASE_AUTO_COLUMNS, OWNER_ID_AUTO_COLUMN] : BASE_AUTO_COLUMNS;

const emptyColumn = (): ColumnDef => ({
  name: "",
  type: "text",
  required: false,
  default: "",
  default_is_expression: false,
  unique: false,
  references: null,
});

const emptyIndex = (): IndexDef => ({
  name: "",
  columns: [],
  unique: false,
});

interface TableCardProps {
  table: TableDef;
  otherTables: TableDef[];
  authEmailEnabled: boolean;
  draftRlsHint?: string;
  locked: boolean;
  startInEdit: boolean;
  onEnterEdit: () => void;
  onExitEdit: () => void;
  onCreate: (input: TableDef) => Promise<TableDef>;
  onUpdate: (input: { rls: string; columns: ColumnDef[]; indexes: IndexDef[] }) => Promise<TableDef>;
  onDelete: () => Promise<void>;
  onSaved: (saved: TableDef) => void;
  onDiscardDraft: () => void;
  onDeleted: () => void;
}

export default function TableCard({
  table,
  otherTables,
  authEmailEnabled,
  draftRlsHint,
  locked,
  startInEdit,
  onEnterEdit,
  onExitEdit,
  onCreate,
  onUpdate,
  onDelete,
  onSaved,
  onDiscardDraft,
  onDeleted,
}: TableCardProps) {
  const { t } = useTranslation();
  const isDraft = !table.id;
  const [editing, setEditing] = useState(startInEdit);
  const [name, setName] = useState(table.name);
  const [rls, setRls] = useState(table.rls);
  const [columns, setColumns] = useState<ColumnDef[]>(
    table.columns.length > 0 ? table.columns.map((c) => ({ ...c })) : [emptyColumn()],
  );
  const [indexes, setIndexes] = useState<IndexDef[]>(
    (table.indexes ?? []).map((idx) => ({ ...idx, columns: [...idx.columns] })),
  );
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showRelationshipsInfo, setShowRelationshipsInfo] = useState(false);
  const [showIndexInfo, setShowIndexInfo] = useState(false);

  const enterEdit = () => {
    setName(table.name);
    setRls(table.rls);
    setColumns(table.columns.map((c) => ({ ...c })));
    setIndexes((table.indexes ?? []).map((idx) => ({ ...idx, columns: [...idx.columns] })));
    setError(null);
    setEditing(true);
    onEnterEdit();
  };

  const cancel = () => {
    setError(null);
    setEditing(false);
    onExitEdit();
    if (isDraft) onDiscardDraft();
  };

  const addColumn = () => setColumns((prev) => [...prev, emptyColumn()]);
  const removeColumn = (ci: number) =>
    setColumns((prev) => prev.filter((_, i) => i !== ci));
  const updateColumn = (ci: number, patch: Partial<ColumnDef>) =>
    setColumns((prev) => prev.map((c, i) => (i === ci ? { ...c, ...patch } : c)));

  const referenceTargetColumns = (tableName: string): string[] => {
    const target = otherTables.find((t) => t.name === tableName);
    if (!target) return ["id"];
    return ["id", ...target.columns.filter((c) => c.unique).map((c) => c.name)];
  };

  const addIndex = () => setIndexes((prev) => [...prev, emptyIndex()]);
  const removeIndex = (ii: number) =>
    setIndexes((prev) => prev.filter((_, i) => i !== ii));
  const updateIndex = (ii: number, patch: Partial<IndexDef>) =>
    setIndexes((prev) => prev.map((idx, i) => (i === ii ? { ...idx, ...patch } : idx)));
  const toggleIndexColumn = (ii: number, colName: string) =>
    setIndexes((prev) =>
      prev.map((idx, i) => {
        if (i !== ii) return idx;
        const has = idx.columns.includes(colName);
        return { ...idx, columns: has ? idx.columns.filter((c) => c !== colName) : [...idx.columns, colName] };
      }),
    );

  async function save() {
    setError(null);
    if (!name.trim()) {
      setError(t("appForm.tableNameRequired"));
      return;
    }
    if (columns.some((c) => !c.name.trim())) {
      setError(t("tableCard.columnsNameRequired"));
      return;
    }
    if (indexes.some((idx) => !idx.name.trim() || idx.columns.length === 0)) {
      setError(t("tableCard.indexInvalid"));
      return;
    }
    setSaving(true);
    try {
      if (isDraft) {
        const saved = await onCreate({ name, rls, columns, indexes });
        onSaved(saved);
      } else {
        const saved = await onUpdate({ rls, columns, indexes });
        onSaved(saved);
      }
      setEditing(false);
      onExitEdit();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("tableCard.saveError"));
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (!confirm(t("tableCard.deleteConfirm", { name: table.name }))) return;
    setDeleting(true);
    setError(null);
    try {
      await onDelete();
      onDeleted();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("tableCard.deleteError"));
      setDeleting(false);
    }
  }

  if (!editing) {
    const totalCols = table.columns.length + autoColumnsFor(table.rls).length;
    return (
      <div className="overflow-hidden rounded-[14px] border border-[var(--border)] bg-[var(--surface)]">
        <div
          onClick={() => { if (!locked) enterEdit(); }}
          className={cn(
            "flex items-center gap-3 px-[18px] py-4",
            locked ? "cursor-default" : "cursor-pointer hover:bg-[var(--hover-surface)]",
          )}
        >
          <Icon name="table_chart" size={18} className="shrink-0 text-[var(--text-tertiary)]" />
          <span className="text-[14.5px] font-bold text-[var(--text-primary)]" style={{ fontFamily: "var(--font-display)" }}>
            {table.name}
          </span>
          <StatusPill
            label={table.rls === "enabled" ? t("appForm.tableRestricted") : t("appForm.tablePublic")}
            tone={table.rls === "enabled" ? "primary" : "neutral"}
            dot={false}
          />
          <span className="ml-auto text-xs text-[var(--text-tertiary)]">
            {t("tableCard.columnsCount", { count: totalCols })}
          </span>
          <Icon name="expand_more" size={20} className="text-[var(--text-tertiary)]" />
        </div>
        {error && <p className="px-[18px] pb-3 text-xs text-[var(--danger)]">{error}</p>}
      </div>
    );
  }

  return (
    <div
      className="animate-in fade-in-0 slide-in-from-bottom-2 duration-200 bg-[var(--surface)] border border-[var(--border)] rounded-[14px] overflow-hidden"
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <Icon name="table_chart" size={15} className="text-[var(--primary)] shrink-0" />
        <Input
          value={name}
          disabled={!isDraft}
          onChange={(e) => setName(e.target.value.toLowerCase().replace(/[\s-]+/g, "_"))}
          placeholder={t("appForm.tableName")}
          className="h-8 px-3 py-1.5 text-[13px] bg-[var(--sunken)] border-[var(--border)] rounded-md text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] brand-focus"
        />
        <Select value={rls} onValueChange={setRls}>
          <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-3 brand-focus">
            <SelectValue />
          </SelectTrigger>
          <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
            <SelectItem value="disabled" className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
              {t("appForm.tablePublic")}
            </SelectItem>
            <SelectItem
              value="enabled"
              disabled={!authEmailEnabled}
              className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]"
            >
              {t("appForm.tableRestricted")}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      {!authEmailEnabled && (
        <p className="px-4 text-[11px] text-[var(--text-secondary)]">
          {t("tableCard.restrictedHint")}
        </p>
      )}
      {isDraft && draftRlsHint && (
        <p className="px-4 text-[11px] text-[var(--text-secondary)]">
          {draftRlsHint}
        </p>
      )}

      <div className="px-4 pb-4">
        <div className="flex items-center justify-between gap-2 mb-2">
          <p className="text-[11px] text-[var(--text-secondary)]">
            {t("tableCard.autoColumnsNote")}
          </p>
          <button
            type="button"
            onClick={() => setShowRelationshipsInfo(true)}
            className="flex items-center gap-1 text-[11px] text-[var(--text-secondary)] hover:text-[var(--primary)] bg-transparent border-none cursor-pointer shrink-0 transition-colors"
          >
            <Icon name="error" size={12} />
            {t("tableCard.relationshipsInfoBtn")}
          </button>
        </div>
        <div className="grid gap-3 mb-1" style={{ gridTemplateColumns: "1fr 140px 80px 80px 140px 32px 40px" }}>
          <span className="text-[11px] text-[var(--text-secondary)] font-semibold">{t("appForm.columnName")}</span>
          <span className="text-[11px] text-[var(--text-secondary)] font-semibold">{t("appForm.columnType")}</span>
          <span className="text-[11px] text-[var(--text-secondary)] font-semibold text-center">{t("appForm.columnReq")}</span>
          <span className="text-[11px] text-[var(--text-secondary)] font-semibold text-center">{t("appForm.columnUnique")}</span>
          <span className="text-[11px] text-[var(--text-secondary)] font-semibold">{t("tableCard.defaultValue")}</span>
          <span />
          <span />
        </div>

        <div className="flex flex-col gap-2.5 mb-2.5">
          {autoColumnsFor(rls).map((auto) => (
            <div
              key={auto.name}
              className="grid gap-3 items-center max-md:flex max-md:flex-col max-md:gap-2 max-md:p-3 max-md:bg-[var(--sunken)] max-md:rounded-[10px] max-md:border max-md:border-[var(--border)]"
              style={{ gridTemplateColumns: "1fr 140px 80px 80px 140px 32px 40px" }}
            >
              <div className="h-8 px-2.5 flex items-center gap-1.5 text-[13px] bg-[var(--sunken)] border border-[var(--border)] rounded-md text-[var(--text-secondary)]">
                <Icon name="lock" size={10} className="shrink-0" />
                {auto.name}
              </div>
              <div className="contents max-md:flex max-md:items-center max-md:gap-2">
                <div className="h-8 w-[130px] max-md:flex-1 flex items-center px-2 text-[12px] bg-[var(--sunken)] border border-[var(--border)] rounded-md text-[var(--text-secondary)]">
                  {auto.type}
                </div>
                <div className="flex justify-center">
                  <Switch checked={auto.required} disabled className="h-5 w-9 opacity-40" />
                </div>
                <div className="flex justify-center">
                  <Switch checked={auto.unique} disabled className="h-5 w-9 opacity-40" />
                </div>
                <span />
                <span />
                <span />
              </div>
            </div>
          ))}
        </div>

        <div className="flex flex-col gap-2.5 mb-3">
          {columns.map((col, ci) => (
            <div key={ci} className="max-md:p-3 max-md:bg-[var(--sunken)] max-md:rounded-[10px] max-md:border max-md:border-[var(--border)]">
              <div
                className="grid gap-3 items-center max-md:flex max-md:flex-col max-md:gap-2"
                style={{ gridTemplateColumns: "1fr 140px 80px 80px 140px 32px 40px" }}
              >
                <Input
                  value={col.name}
                  onChange={(e) =>
                    updateColumn(ci, { name: e.target.value.toLowerCase().replace(/[\s-]+/g, "_") })
                  }
                  placeholder={t("tableCard.columnNamePlaceholder")}
                  className="h-8 px-2.5 py-1.5 text-[13px] bg-[var(--sunken)] border-[var(--border)] rounded-md text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] brand-focus"
                />
                <div className="contents max-md:flex max-md:items-center max-md:gap-2">
                  <Select
                    value={col.type}
                    onValueChange={(val) =>
                      // A default value/expression is only ever valid for
                      // the type it was set under (a boolean "true" isn't a
                      // valid uuid, an expression picked for uuid isn't
                      // valid for integer, etc.) — changing type always
                      // clears it rather than risk sending a mismatched
                      // default the backend will reject.
                      updateColumn(ci, { type: val, default: "", default_is_expression: false })
                    }
                  >
                    <SelectTrigger className="h-8 w-[130px] max-md:flex-1 text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
                      {COLUMN_TYPES.map((t) => (
                        <SelectItem key={t} value={t} className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
                          {t}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <div className="flex justify-center">
                    <Switch
                      checked={col.required}
                      onCheckedChange={(val) => updateColumn(ci, { required: val })}
                      className="h-5 w-9"
                    />
                  </div>
                  <div className="flex justify-center">
                    <Switch
                      checked={col.unique}
                      onCheckedChange={(val) => updateColumn(ci, { unique: val })}
                      className="h-5 w-9"
                    />
                  </div>
                  <div className="flex items-center max-md:w-full">
                    {col.type === "boolean" ? (
                      <Select
                        value={col.default === "" ? "__none__" : col.default}
                        onValueChange={(val) =>
                          updateColumn(ci, val === "__none__"
                            ? { default: "", default_is_expression: false }
                            : { default: val, default_is_expression: false })
                        }
                      >
                        <SelectTrigger className="h-8 w-full text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-2 brand-focus">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
                          <SelectItem value="__none__" className="text-[12px]">{t("tableCard.defaultNone")}</SelectItem>
                          <SelectItem value="true" className="text-[12px]">true</SelectItem>
                          <SelectItem value="false" className="text-[12px]">false</SelectItem>
                        </SelectContent>
                      </Select>
                    ) : DEFAULT_EXPRESSIONS[col.type] ? (
                      <Select
                        value={col.default_is_expression ? col.default : "__none__"}
                        onValueChange={(val) =>
                          updateColumn(ci, val === "__none__"
                            ? { default: "", default_is_expression: false }
                            : { default: val, default_is_expression: true })
                        }
                      >
                        <SelectTrigger className="h-8 w-full text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-2 brand-focus">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
                          <SelectItem value="__none__" className="text-[12px]">{t("tableCard.defaultNone")}</SelectItem>
                          <SelectItem value={DEFAULT_EXPRESSIONS[col.type]} className="text-[12px] font-mono">
                            {DEFAULT_EXPRESSIONS[col.type]}
                          </SelectItem>
                        </SelectContent>
                      </Select>
                    ) : col.type === "integer" || col.type === "bigint" || col.type === "numeric" ? (
                      <Input
                        value={col.default}
                        onChange={(e) =>
                          updateColumn(ci, {
                            default: sanitizeNumericDefault(col.type, e.target.value),
                            default_is_expression: false,
                          })
                        }
                        inputMode={col.type === "numeric" ? "decimal" : "numeric"}
                        placeholder={t("tableCard.defaultNone")}
                        className="h-8 w-full px-2 text-[12px] bg-[var(--sunken)] border-[var(--border)] rounded-md text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] brand-focus"
                      />
                    ) : (
                      <span />
                    )}
                  </div>
                  <button
                    type="button"
                    title={t("tableCard.referenceFk")}
                    disabled={otherTables.length === 0}
                    onClick={() =>
                      updateColumn(
                        ci,
                        col.references
                          ? { references: null }
                          : { references: { table: otherTables[0]?.name ?? "", column: "id", on_delete: "no_action" } },
                      )
                    }
                    className={cn(
                      "w-7 h-7 flex items-center justify-center rounded-md border transition-colors",
                      col.references
                        ? "border-[var(--primary)]/40 bg-[var(--primary)]/10 text-[var(--primary)]"
                        : "border-[var(--border)] bg-transparent text-[var(--text-secondary)]",
                      otherTables.length === 0 && "opacity-30 cursor-not-allowed",
                    )}
                  >
                    <Icon name="link" size={12} />
                  </button>
                  <button
                    type="button"
                    title={t("tableCard.removeColumn")}
                    onClick={() => removeColumn(ci)}
                    disabled={columns.length <= 1}
                    className={cn(
                      "w-7 h-7 flex items-center justify-center rounded-md border border-[var(--danger)]/20 bg-[var(--danger-tint)] transition-colors",
                      columns.length <= 1 ? "text-[var(--danger)]/40 cursor-not-allowed" : "text-[var(--danger)] cursor-pointer hover:bg-[var(--danger-tint)]",
                    )}
                  >
                    <Icon name="delete" size={12} />
                  </button>
                </div>
              </div>

              {col.references && (
                <div className="flex flex-wrap items-center gap-2 mt-2 pl-1">
                  <span className="text-[11px] text-[var(--text-secondary)]">{t("tableCard.references")}</span>
                  <Select
                    value={col.references.table}
                    onValueChange={(val) =>
                      updateColumn(ci, { references: { ...col.references!, table: val, column: "id" } })
                    }
                  >
                    <SelectTrigger className="h-7 w-[140px] text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-2 brand-focus">
                      <SelectValue placeholder={t("tableCard.tablePlaceholder")} />
                    </SelectTrigger>
                    <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
                      {otherTables.map((ot) => (
                        <SelectItem key={ot.name} value={ot.name} className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
                          {ot.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <span className="text-[11px] text-[var(--text-secondary)]">{t("tableCard.column")}</span>
                  <Select
                    value={col.references.column}
                    onValueChange={(val) => updateColumn(ci, { references: { ...col.references!, column: val } })}
                  >
                    <SelectTrigger className="h-7 w-[110px] text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
                      {referenceTargetColumns(col.references.table).map((c) => (
                        <SelectItem key={c} value={c} className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
                          {c}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <span className="text-[11px] text-[var(--text-secondary)]">{t("tableCard.onDelete")}</span>
                  <Select
                    value={col.references.on_delete ?? "no_action"}
                    onValueChange={(val) =>
                      updateColumn(ci, {
                        references: { ...col.references!, on_delete: val as ReferenceDef["on_delete"] },
                      })
                    }
                  >
                    <SelectTrigger className="h-7 w-[170px] text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
                      {ON_DELETE_VALUES.map((val) => (
                        <SelectItem key={val} value={val!} className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
                          {t(ON_DELETE_LABEL_KEYS[val!])}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>
          ))}
        </div>

        <button
          type="button"
          onClick={addColumn}
          className="flex items-center gap-1.5 text-[12px] font-semibold bg-transparent border border-[var(--border)] rounded-full px-3 py-1.5 cursor-pointer hover:bg-[var(--hover-surface)] transition-colors self-start"
          style={{ color: "var(--primary)" }}
        >
          <Icon name="add" size={11} />
          {t("appForm.addColumn")}
        </button>

        <div className="mt-4 pt-4 border-t border-[var(--border)]">
          <div className="flex items-center justify-between gap-2 mb-2">
            <p className="text-[11px] text-[var(--text-secondary)] font-semibold">{t("tableCard.indexesTitle")}</p>
            <button
              type="button"
              onClick={() => setShowIndexInfo(true)}
              className="flex items-center gap-1 text-[11px] text-[var(--text-secondary)] hover:text-[var(--primary)] bg-transparent border-none cursor-pointer shrink-0 transition-colors"
            >
            <Icon name="error" size={12} />
              {t("tableCard.indexInfoBtn")}
            </button>
          </div>

          <div className="flex flex-col gap-2 mb-2">
            {indexes.map((idx, ii) => (
              <div
                key={ii}
                className="flex flex-wrap items-center gap-2 bg-[var(--sunken)] border border-[var(--border)] rounded-[10px] px-3 py-2"
              >
                <Input
                  value={idx.name}
                  onChange={(e) => updateIndex(ii, { name: e.target.value.toLowerCase().replace(/[\s-]+/g, "_") })}
                  placeholder={t("tableCard.indexNamePlaceholder")}
                  className="h-7 w-[160px] shrink-0 px-2 py-1 text-[12px] bg-[var(--sunken)] border-[var(--border)] rounded-md text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] brand-focus"
                />
                <div className="flex flex-wrap gap-1 flex-1 min-w-[120px]">
                  {columns.filter((c) => c.name.trim()).map((c) => (
                    <button
                      key={c.name}
                      type="button"
                      onClick={() => toggleIndexColumn(ii, c.name)}
                      className={cn(
                        "text-[11px] px-2 py-0.5 rounded-full border transition-colors",
                        idx.columns.includes(c.name)
                          ? "border-[var(--primary)]/40 bg-[var(--primary)]/10 text-[var(--primary)]"
                          : "border-[var(--border)] text-[var(--text-secondary)] hover:bg-[var(--hover-surface)]",
                      )}
                    >
                      {c.name}
                    </button>
                  ))}
                </div>
                <div className="flex items-center gap-2 shrink-0 ml-auto">
                  <div className="flex items-center gap-1.5">
                    <span className="text-[11px] text-[var(--text-secondary)]">{t("tableCard.uniqueLabel")}</span>
                    <Switch
                      checked={!!idx.unique}
                      onCheckedChange={(val) => updateIndex(ii, { unique: val })}
                      className="h-5 w-9"
                    />
                  </div>
                  <button
                    type="button"
                    title={t("tableCard.removeIndex")}
                    onClick={() => removeIndex(ii)}
                    className="w-7 h-7 flex items-center justify-center rounded-md border border-[var(--danger)]/20 bg-[var(--danger-tint)] text-[var(--danger)] cursor-pointer hover:bg-[var(--danger-tint)] transition-colors shrink-0"
                  >
                    <Icon name="delete" size={12} />
                  </button>
                </div>
              </div>
            ))}
          </div>

          <button
            type="button"
            onClick={addIndex}
            className="flex items-center gap-1.5 text-[12px] font-semibold bg-transparent border border-[var(--border)] rounded-full px-3 py-1.5 cursor-pointer hover:bg-[var(--hover-surface)] transition-colors self-start"
            style={{ color: "var(--primary)" }}
          >
          <Icon name="add" size={11} />
            {t("tableCard.addIndex")}
          </button>
        </div>

        {error && <p className="text-xs text-[var(--danger)] mt-3">{error}</p>}

        <div className="mt-6 flex items-center justify-between gap-2 border-t border-[var(--border)] pt-4">
          {!isDraft ? (
            <button
              type="button"
              onClick={remove}
              disabled={deleting || saving}
              className="text-[13px] font-semibold text-[var(--danger)] bg-transparent border-none cursor-pointer disabled:opacity-50"
            >
              {deleting ? t("tableCard.deletingTable") : t("tableCard.deleteTable")}
            </button>
          ) : (
            <span />
          )}
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={cancel}
              disabled={saving}
              className="text-[12px] font-medium px-4 py-1.5 rounded-full border border-[var(--border)] bg-transparent text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
            >
              {t("appForm.cancel")}
            </button>
            <button
              type="button"
              onClick={save}
              disabled={saving}
              className="text-[12px] font-semibold px-4 py-1.5 rounded-full text-white cursor-pointer disabled:opacity-50"
              style={{ background: "var(--primary)" }}
            >
              {saving ? t("tableCard.savingTable") : t("tableCard.saveTable")}
            </button>
          </div>
        </div>
      </div>

      <Dialog open={showRelationshipsInfo} onOpenChange={setShowRelationshipsInfo}>
        <DialogContent className="max-w-[480px] border border-[var(--border)] bg-[var(--surface-raised)] backdrop-blur-xl rounded-[18px] p-0 gap-0">
          <div className="bg-[var(--surface)] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
            <DialogHeader className="mb-0">
              <div className="w-11 h-11 rounded-[12px] bg-[var(--hover-surface)] border border-[var(--border)] flex items-center justify-center mb-[18px]">
                <Icon name="link" size={18} className="text-[var(--text-secondary)]" />
              </div>
              <DialogTitle className="text-base font-bold text-[var(--text-primary)] mb-2">
                {t("tableCard.relationshipsInfoBtn")}
              </DialogTitle>
              <DialogDescription className="text-[13px] text-[var(--text-secondary)] leading-relaxed">
                <Trans
                  i18nKey="tableCard.relationshipsExplainer"
                  components={{
                    1: <code className="text-[var(--primary)]" />,
                    3: <code className="text-[var(--primary)]" />,
                    5: <code className="text-[var(--primary)]" />,
                    7: <strong className="text-[var(--primary)] font-medium" />,
                    9: <strong className="text-[var(--primary)] font-medium" />,
                    11: <strong className="text-[var(--primary)] font-medium" />,
                    13: <strong className="text-[var(--primary)] font-medium" />,
                  }}
                />
              </DialogDescription>
            </DialogHeader>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={showIndexInfo} onOpenChange={setShowIndexInfo}>
        <DialogContent className="max-w-[480px] border border-[var(--border)] bg-[var(--surface-raised)] backdrop-blur-xl rounded-[18px] p-0 gap-0">
          <div className="bg-[var(--surface)] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
            <DialogHeader className="mb-0">
              <div className="w-11 h-11 rounded-[12px] bg-[var(--hover-surface)] border border-[var(--border)] flex items-center justify-center mb-[18px]">
                <Icon name="error" size={18} className="text-[var(--text-secondary)]" />
              </div>
              <DialogTitle className="text-base font-bold text-[var(--text-primary)] mb-2">
                {t("tableCard.indexInfoBtn")}
              </DialogTitle>
              <DialogDescription className="text-[13px] text-[var(--text-secondary)] leading-relaxed">
                <Trans
                  i18nKey="tableCard.indexExplainer"
                  components={{
                    1: <strong className="text-[var(--primary)] font-medium" />,
                    3: <strong className="text-[var(--primary)] font-medium" />,
                    5: <code className="text-[var(--primary)]" />,
                    7: <code className="text-[var(--primary)]" />,
                    9: <strong className="text-[var(--primary)] font-medium" />,
                  }}
                />
              </DialogDescription>
            </DialogHeader>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
