import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { TableDef, ColumnDef, IndexDef, ReferenceDef } from "../lib/api";
import { hasOwnerColumn } from "../lib/rls";
import { cn } from "@/lib/utils";
import { Icon } from "@/components/ui/icon";
import { StatusPill, ConfirmDialog, FormDrawer, MarkdownContent } from "@/components/patterns";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import TablePoliciesTab from "@/components/TablePolicies";

const COLUMN_TYPES = [
  "text",
  "integer",
  "bigint",
  "boolean",
  "uuid",
  "timestamptz",
  "numeric",
  "jsonb",
  "enum",
];

// Mirrors config.ValidateEnumValues (internal/config/validate.go): 1-50
// values, each 1-100 chars, no exact-match duplicates. Client-side mirror
// for immediate feedback only — the backend re-validates on submit.
const ENUM_MAX_VALUES = 50;
const ENUM_MAX_VALUE_LENGTH = 100;

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

// Mirrors internal/dashboard/handler.go's special-cased "_auth_users" target
// — not a real entry in app.tables (provisioned separately when email auth
// is on), so it never shows up via the otherTables prop and needs its own UI path.
const AUTH_USERS_TABLE = "_auth_users";

const BASE_AUTO_COLUMNS = [
  { name: "id", type: "uuid", required: true, unique: true },
  { name: "created_at", type: "timestamptz", required: true, unique: false },
  { name: "updated_at", type: "timestamptz", required: true, unique: false },
];

const OWNER_ID_AUTO_COLUMN = { name: "owner_id", type: "uuid", required: true, unique: false };

// owner_id só existe quando RLS está ativo (config.HasOwnerColumn: rls == "owner" || rls == "enabled" || rls == "policy").
const autoColumnsFor = (rls: string) =>
  hasOwnerColumn(rls) ? [...BASE_AUTO_COLUMNS, OWNER_ID_AUTO_COLUMN] : BASE_AUTO_COLUMNS;

// Mirrors config.AutoScopesByOwner: "policy" mode is a different group from
// ""/"owner"/"enabled" — switching a saved table between the two groups
// changes which rows each role can see (RLSP-07), so the UI must confirm
// before applying it. Switching within the same group (e.g. "owner" ->
// "enabled") does not change row visibility semantics and needs no warning.
const isPolicyRLS = (rls: string) => rls === "policy";

// Radix Select reserves the empty string as its internal placeholder sentinel
// (shouldShowPlaceholder in @radix-ui/react-select) — a SelectItem with
// value="" never renders its label into the trigger. rls: "" is the real,
// backend-valid "Public"/no-RLS value (config.ValidRLS), so the Select UI
// maps it to this sentinel and back; nothing outside the Select ever sees it.
const RLS_PUBLIC_SENTINEL = "public";
const toSelectValue = (rls: string) => (rls === "" ? RLS_PUBLIC_SENTINEL : rls);
const fromSelectValue = (val: string) => (val === RLS_PUBLIC_SENTINEL ? "" : val);

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
  appId: string;
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
  appId,
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
  const [pendingRlsValue, setPendingRlsValue] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

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

  // Only warns on a saved table (isDraft === false) crossing between the
  // ""/"owner"/"enabled" group and "policy" — a brand-new table has no rows
  // yet, so there is nothing to warn about (spec.md RLSP-07 AC1).
  const changeRls = (val: string) => {
    if (!isDraft && isPolicyRLS(val) !== isPolicyRLS(rls)) {
      setPendingRlsValue(val);
      return;
    }
    setRls(val);
  };

  const confirmRlsChange = () => {
    if (pendingRlsValue === null) return;
    setRls(pendingRlsValue);
    setPendingRlsValue(null);
  };

  const addColumn = () => setColumns((prev) => [...prev, emptyColumn()]);
  const removeColumn = (ci: number) =>
    setColumns((prev) => prev.filter((_, i) => i !== ci));
  const updateColumn = (ci: number, patch: Partial<ColumnDef>) =>
    setColumns((prev) => prev.map((c, i) => (i === ci ? { ...c, ...patch } : c)));

  const referenceTargetColumns = (tableName: string): string[] => {
    if (tableName === AUTH_USERS_TABLE) return ["id"];
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
    if (columns.some((c) => c.type === "enum" && (c.allowed_values ?? []).length === 0)) {
      setError(t("tableCard.allowedValuesRequired"));
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

  function remove() {
    setConfirmingDelete(true);
  }

  async function confirmRemove() {
    setConfirmingDelete(false);
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
        <Select value={toSelectValue(rls)} onValueChange={(val) => changeRls(fromSelectValue(val))}>
          <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-[var(--sunken)] border-[var(--border)] text-[var(--text-primary)] rounded-md px-3 brand-focus">
            <SelectValue />
          </SelectTrigger>
          <SelectContent className="bg-[var(--surface-raised)] border-[var(--border)] text-[var(--text-primary)]">
            <SelectItem value={RLS_PUBLIC_SENTINEL} className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
              {t("appForm.tablePublic")}
            </SelectItem>
            <SelectItem
              value="enabled"
              disabled={!authEmailEnabled}
              className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]"
            >
              {t("appForm.tableRestricted")}
            </SelectItem>
            <SelectItem
              value="policy"
              disabled={!authEmailEnabled}
              className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]"
            >
              {t("appForm.tablePolicy")}
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

      {isDraft ? (
        <SchemaEditor
          t={t}
          appId={appId}
          rls={rls}
          columns={columns}
          indexes={indexes}
          otherTables={otherTables}
          authEmailEnabled={authEmailEnabled}
          isDraft={isDraft}
          table={table}
          deleting={deleting}
          saving={saving}
          error={error}
          showRelationshipsInfo={showRelationshipsInfo}
          setShowRelationshipsInfo={setShowRelationshipsInfo}
          showIndexInfo={showIndexInfo}
          setShowIndexInfo={setShowIndexInfo}
          addColumn={addColumn}
          removeColumn={removeColumn}
          updateColumn={updateColumn}
          referenceTargetColumns={referenceTargetColumns}
          addIndex={addIndex}
          removeIndex={removeIndex}
          updateIndex={updateIndex}
          toggleIndexColumn={toggleIndexColumn}
          remove={remove}
          cancel={cancel}
          save={save}
        />
      ) : (
        <Tabs defaultValue="schema" className="w-full">
          <TabsList className="mx-4 mb-2 inline-flex h-auto w-auto justify-start gap-0.5 rounded-[10px] bg-[var(--sunken)] p-[3px]">
            <TabsTrigger
              value="schema"
              className="rounded-[7px] px-3 py-1.5 text-[12.5px] font-semibold text-[var(--text-secondary)] data-[state=active]:bg-[var(--surface)] data-[state=active]:text-[var(--text-primary)] data-[state=active]:shadow-sm"
            >
              {t("tableCard.schemaTab")}
            </TabsTrigger>
            <TabsTrigger
              value="policies"
              className="rounded-[7px] px-3 py-1.5 text-[12.5px] font-semibold text-[var(--text-secondary)] data-[state=active]:bg-[var(--surface)] data-[state=active]:text-[var(--text-primary)] data-[state=active]:shadow-sm"
            >
              {t("tablePolicies.tab")}
            </TabsTrigger>
          </TabsList>
          <TabsContent value="schema" className="mt-0">
            <SchemaEditor
              t={t}
              appId={appId}
              rls={rls}
              columns={columns}
              indexes={indexes}
              otherTables={otherTables}
              authEmailEnabled={authEmailEnabled}
              isDraft={isDraft}
              table={table}
              deleting={deleting}
              saving={saving}
              error={error}
              showRelationshipsInfo={showRelationshipsInfo}
              setShowRelationshipsInfo={setShowRelationshipsInfo}
              showIndexInfo={showIndexInfo}
              setShowIndexInfo={setShowIndexInfo}
              addColumn={addColumn}
              removeColumn={removeColumn}
              updateColumn={updateColumn}
              referenceTargetColumns={referenceTargetColumns}
              addIndex={addIndex}
              removeIndex={removeIndex}
              updateIndex={updateIndex}
              toggleIndexColumn={toggleIndexColumn}
              remove={remove}
              cancel={cancel}
              save={save}
            />
          </TabsContent>
          <TabsContent value="policies" className="mt-0 px-4 pb-4">
            <TablePoliciesTab appId={appId} tableName={table.name} columns={table.columns} rls={table.rls} />
          </TabsContent>
        </Tabs>
      )}

      <ConfirmDialog
        open={pendingRlsValue !== null}
        title={t("tableCard.rlsModeSwitchTitle")}
        message={t("tableCard.rlsModeSwitchConfirm")}
        confirmLabel={t("tableCard.continue")}
        cancelLabel={t("tableCard.cancel")}
        icon="warning"
        onConfirm={confirmRlsChange}
        onCancel={() => setPendingRlsValue(null)}
      />
      <ConfirmDialog
        open={confirmingDelete}
        title={t("tableCard.deleteTitle")}
        message={t("tableCard.deleteConfirm", { name: table.name })}
        confirmLabel={t("tableCard.deleteTable")}
        cancelLabel={t("tableCard.cancel")}
        destructive
        icon="delete"
        loading={deleting}
        onConfirm={confirmRemove}
        onCancel={() => setConfirmingDelete(false)}
      />
    </div>
  );
}

function SchemaEditor({
  t,
  appId,
  rls,
  columns,
  indexes,
  otherTables,
  authEmailEnabled,
  isDraft,
  table,
  deleting,
  saving,
  error,
  showRelationshipsInfo,
  setShowRelationshipsInfo,
  showIndexInfo,
  setShowIndexInfo,
  addColumn,
  removeColumn,
  updateColumn,
  referenceTargetColumns,
  addIndex,
  removeIndex,
  updateIndex,
  toggleIndexColumn,
  remove,
  cancel,
  save,
}: {
  t: (key: string, opts?: Record<string, unknown>) => string;
  appId: string;
  rls: string;
  columns: ColumnDef[];
  indexes: IndexDef[];
  otherTables: TableDef[];
  authEmailEnabled: boolean;
  isDraft: boolean;
  table: TableDef;
  deleting: boolean;
  saving: boolean;
  error: string | null;
  showRelationshipsInfo: boolean;
  setShowRelationshipsInfo: (v: boolean) => void;
  showIndexInfo: boolean;
  setShowIndexInfo: (v: boolean) => void;
  addColumn: () => void;
  removeColumn: (ci: number) => void;
  updateColumn: (ci: number, patch: Partial<ColumnDef>) => void;
  referenceTargetColumns: (tableName: string) => string[];
  addIndex: () => void;
  removeIndex: (ii: number) => void;
  updateIndex: (ii: number, patch: Partial<IndexDef>) => void;
  toggleIndexColumn: (ii: number, colName: string) => void;
  remove: () => void;
  cancel: () => void;
  save: () => void;
}) {
  return (
    <>
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
          {columns.map((col, ci) => {
            // An enum column already saved on the backend (table.columns, the
            // pre-edit prop) cannot have its allowed_values changed via this
            // generic form — UpdateAppTable's PUT path rejects that (T7's
            // guard: an existing column's CHECK constraint is never
            // re-emitted by addMissingColumns). A brand-new column added
            // this session has no "before" state, so it's still freely
            // editable here.
            const existingEnumColumn = !isDraft
              ? table.columns.find((c) => c.name === col.name && c.type === "enum")
              : undefined;
            return (
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
                      // default the backend will reject. Same reasoning for
                      // allowed_values: only meaningful for "enum", so it's
                      // reset to an empty draft list when switching in, and
                      // dropped entirely when switching away.
                      updateColumn(ci, {
                        type: val,
                        default: "",
                        default_is_expression: false,
                        allowed_values: val === "enum" ? (col.allowed_values ?? []) : undefined,
                      })
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
                    disabled={otherTables.length === 0 && !(authEmailEnabled && col.type === "uuid")}
                    onClick={() =>
                      updateColumn(
                        ci,
                        col.references
                          ? { references: null }
                          : {
                              references: {
                                table: otherTables[0]?.name ?? (authEmailEnabled && col.type === "uuid" ? AUTH_USERS_TABLE : ""),
                                column: "id",
                                on_delete: "no_action",
                              },
                            },
                      )
                    }
                    className={cn(
                      "w-7 h-7 flex items-center justify-center rounded-md border transition-colors",
                      col.references
                        ? "border-[var(--primary)]/40 bg-[var(--primary)]/10 text-[var(--primary)]"
                        : "border-[var(--border)] bg-transparent text-[var(--text-secondary)]",
                      otherTables.length === 0 && !(authEmailEnabled && col.type === "uuid") && "opacity-30 cursor-not-allowed",
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
                      {authEmailEnabled && col.type === "uuid" && (
                        <SelectItem value={AUTH_USERS_TABLE} className="text-[12px] focus:bg-[var(--hover-surface)] focus:text-[var(--text-primary)]">
                          {AUTH_USERS_TABLE}
                        </SelectItem>
                      )}
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

              {col.type === "enum" && existingEnumColumn && table.id && (
                <div className="flex flex-wrap items-center gap-2 mt-2 pl-1">
                  <EnumAllowedValuesEditor t={t} values={col.allowed_values ?? []} onChange={() => {}} readOnly />
                  <EditEnumValuesAction
                    t={t}
                    appId={appId}
                    tableId={table.id}
                    columnName={col.name}
                    values={col.allowed_values ?? []}
                    onSaved={(values) => updateColumn(ci, { allowed_values: values })}
                  />
                </div>
              )}
              {col.type === "enum" && !existingEnumColumn && (
                <EnumAllowedValuesEditor
                  t={t}
                  values={col.allowed_values ?? []}
                  onChange={(values) => updateColumn(ci, { allowed_values: values })}
                />
              )}
            </div>
            );
          })}
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

      <FormDrawer
        open={showRelationshipsInfo}
        onOpenChange={setShowRelationshipsInfo}
        title={t("tableCard.relationshipsInfoBtn")}
      >
        <MarkdownContent content={t("tableCard.relationshipsExplainer")} />
      </FormDrawer>

      <FormDrawer
        open={showIndexInfo}
        onOpenChange={setShowIndexInfo}
        title={t("tableCard.indexInfoBtn")}
      >
        <MarkdownContent content={t("tableCard.indexExplainer")} />
      </FormDrawer>
    </>
  );
}

// EnumAllowedValuesEditor is a small chip-list input for an "enum" column's
// allowed_values — add one value at a time (Enter or the + button), remove
// with the chip's own close button. Caps mirror config.ValidateEnumValues
// (ENUM_MAX_VALUES/ENUM_MAX_VALUE_LENGTH) for immediate feedback only; the
// backend re-validates on submit regardless.
function EnumAllowedValuesEditor({
  t,
  values,
  onChange,
  readOnly,
}: {
  t: (key: string, opts?: Record<string, unknown>) => string;
  values: string[];
  onChange: (values: string[]) => void;
  readOnly?: boolean;
}) {
  const [draft, setDraft] = useState("");
  const [err, setErr] = useState<string | null>(null);

  const addValue = () => {
    const v = draft.trim();
    if (!v) return;
    if (v.length > ENUM_MAX_VALUE_LENGTH) {
      setErr(t("tableCard.allowedValuesTooLong"));
      return;
    }
    if (values.includes(v)) {
      setErr(t("tableCard.allowedValuesDuplicate"));
      return;
    }
    if (values.length >= ENUM_MAX_VALUES) {
      setErr(t("tableCard.allowedValuesTooMany"));
      return;
    }
    setErr(null);
    setDraft("");
    onChange([...values, v]);
  };

  return (
    <div className="flex flex-wrap items-center gap-2 mt-2 pl-1">
      <span className="text-[11px] text-[var(--text-secondary)]">{t("tableCard.allowedValuesLabel")}</span>
      <div className="flex flex-wrap gap-1">
        {values.map((v) => (
          <span
            key={v}
            className="flex items-center gap-1 text-[11px] px-2 py-0.5 rounded-full border border-[var(--border)] text-[var(--text-secondary)]"
          >
            {v}
            {!readOnly && (
              <button
                type="button"
                onClick={() => onChange(values.filter((x) => x !== v))}
                title={t("tableCard.removeAllowedValue")}
                className="bg-transparent border-none cursor-pointer p-0 flex items-center text-[var(--text-tertiary)] hover:text-[var(--danger)]"
              >
                <Icon name="close" size={10} />
              </button>
            )}
          </span>
        ))}
        {values.length === 0 && (
          <span className="text-[11px] text-[var(--text-tertiary)]">{t("tableCard.allowedValuesEmpty")}</span>
        )}
      </div>
      {readOnly ? null : (
      <Input
        value={draft}
        onChange={(e) => {
          setDraft(e.target.value);
          setErr(null);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            addValue();
          }
        }}
        placeholder={t("tableCard.allowedValuesPlaceholder")}
        className="h-7 w-[160px] px-2 text-[12px] bg-[var(--sunken)] border-[var(--border)] rounded-md text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] brand-focus"
      />
      )}
      {!readOnly && (
        <button
          type="button"
          onClick={addValue}
          title={t("tableCard.addAllowedValue")}
          className="w-6 h-6 flex items-center justify-center rounded-md border border-[var(--border)] bg-transparent text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
        >
          <Icon name="add" size={10} />
        </button>
      )}
      {err && <span className="w-full text-[11px] text-[var(--danger)]">{err}</span>}
    </div>
  );
}

// updateColumnEnumValues calls the dedicated PATCH endpoint (T8/Batch 1)
// directly — bypassing the generic PUT /tables/{id} path, which T7 already
// made reject an AllowedValues change on an existing enum column. Mirrors
// apiFetch's error-body-to-message shape (lib/api.ts) without importing it,
// since this file's scope for this task is TableCard.tsx itself.
async function updateColumnEnumValues(
  appId: string,
  tableId: string,
  columnName: string,
  values: string[],
): Promise<void> {
  const res = await fetch(
    `/dashboard/api/apps/${appId}/tables/${tableId}/columns/${encodeURIComponent(columnName)}/enum-values`,
    {
      method: "PATCH",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ allowed_values: values }),
    },
  );
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      message = body.error ?? message;
    } catch {
      // no JSON body — keep the generic HTTP-status message.
    }
    throw new Error(message);
  }
}

// EditEnumValuesAction is the dedicated "edit allowed values" action for an
// EXISTING enum column (column-enum-type T13) — separate from the generic
// save-all-columns form, since that form's PUT path can't apply the change
// (T7's guard). Opens a focused drawer, widens/narrows via the real
// endpoint, and surfaces a narrowing rejection (EnumValueInUseError, from
// T8/Batch 1) as a plain error message.
function EditEnumValuesAction({
  t,
  appId,
  tableId,
  columnName,
  values,
  onSaved,
}: {
  t: (key: string, opts?: Record<string, unknown>) => string;
  appId: string;
  tableId: string;
  columnName: string;
  values: string[];
  onSaved: (values: string[]) => void;
}) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [draftValues, setDraftValues] = useState<string[]>(values);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const openDrawer = () => {
    setDraftValues(values);
    setError(null);
    setOpen(true);
  };

  const saveValues = async () => {
    if (draftValues.length === 0) {
      setError(t("tableCard.allowedValuesRequired"));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await updateColumnEnumValues(appId, tableId, columnName, draftValues);
      onSaved(draftValues);
      queryClient.invalidateQueries({ queryKey: ["apps"] });
      setOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("tableCard.saveError"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <button
        type="button"
        onClick={openDrawer}
        className="flex items-center gap-1 text-[11px] font-semibold bg-transparent border border-[var(--border)] rounded-full px-2 py-0.5 cursor-pointer hover:bg-[var(--hover-surface)] transition-colors"
        style={{ color: "var(--primary)" }}
      >
        <Icon name="edit" size={10} />
        {t("tableCard.editAllowedValues")}
      </button>
      <FormDrawer
        open={open}
        onOpenChange={setOpen}
        title={t("tableCard.editAllowedValuesTitle", { column: columnName })}
      >
        <div className="flex flex-col gap-3 p-1">
          <p className="text-[12px] text-[var(--text-secondary)]">{t("tableCard.editAllowedValuesExplainer")}</p>
          <EnumAllowedValuesEditor t={t} values={draftValues} onChange={setDraftValues} />
          {error && <p className="text-xs text-[var(--danger)]">{error}</p>}
          <div className="flex justify-end gap-2 mt-2">
            <button
              type="button"
              onClick={() => setOpen(false)}
              disabled={saving}
              className="text-[12px] font-medium px-4 py-1.5 rounded-full border border-[var(--border)] bg-transparent text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
            >
              {t("tableCard.cancel")}
            </button>
            <button
              type="button"
              onClick={saveValues}
              disabled={saving}
              className="text-[12px] font-semibold px-4 py-1.5 rounded-full text-white cursor-pointer disabled:opacity-50"
              style={{ background: "var(--primary)" }}
            >
              {saving ? t("tableCard.savingTable") : t("tableCard.saveTable")}
            </button>
          </div>
        </div>
      </FormDrawer>
    </>
  );
}
