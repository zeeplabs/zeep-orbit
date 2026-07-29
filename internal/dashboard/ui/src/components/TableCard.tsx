import { useState } from "react";
import { motion } from "framer-motion";
import { Plus, Trash2, Table2, Link2, Lock } from "lucide-react";
import { TableDef, ColumnDef, IndexDef, ReferenceDef } from "../lib/api";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

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

const ON_DELETE_OPTIONS: { value: NonNullable<ReferenceDef["on_delete"]>; label: string }[] = [
  { value: "no_action", label: "Nenhuma ação" },
  { value: "cascade", label: "Cascata (apaga junto)" },
  { value: "restrict", label: "Restringir (bloqueia exclusão)" },
  { value: "set_null", label: "Definir como nulo" },
];

const AUTO_COLUMNS = [
  { name: "id", type: "uuid", required: true, unique: true },
  { name: "created_at", type: "timestamptz", required: true, unique: false },
  { name: "updated_at", type: "timestamptz", required: true, unique: false },
];

const emptyColumn = (): ColumnDef => ({
  name: "",
  type: "text",
  required: false,
  default: "",
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
      setError("Nome da tabela é obrigatório");
      return;
    }
    if (columns.some((c) => !c.name.trim())) {
      setError("Toda coluna precisa de um nome");
      return;
    }
    if (indexes.some((idx) => !idx.name.trim() || idx.columns.length === 0)) {
      setError("Todo índice precisa de um nome e ao menos uma coluna");
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
      setError(err instanceof Error ? err.message : "Erro ao salvar tabela");
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (!confirm(`Excluir a tabela "${table.name}"? Isso apaga os dados dela.`)) return;
    setDeleting(true);
    setError(null);
    try {
      await onDelete();
      onDeleted();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Erro ao excluir tabela");
      setDeleting(false);
    }
  }

  if (!editing) {
    const foreignKeys = table.columns
      .filter((c) => c.references?.table)
      .map((c) => ({
        text: `${c.name} → ${c.references!.table}.${c.references!.column}`,
        onDelete: c.references!.on_delete && c.references!.on_delete !== "no_action" ? c.references!.on_delete : null,
      }));
    const tableIndexes = table.indexes ?? [];

    return (
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-xl px-4 py-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Table2 size={15} strokeWidth={1.5} className="text-[#B3D1FF] shrink-0" />
            <span className="text-[13px] font-semibold text-[#F8FAFC]">{table.name}</span>
            <span className="text-[10px] font-semibold uppercase tracking-wide px-2 py-0.5 rounded-full border border-white/[0.10] text-[#94A3B8]">
              {table.rls === "enabled" ? "Restrito" : "Público"}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={locked}
              onClick={enterEdit}
              className={cn(
                "text-[12px] font-medium px-3 py-1.5 rounded-full border border-white/[0.10] bg-transparent",
                locked ? "text-white/30 cursor-not-allowed" : "text-[#F8FAFC] cursor-pointer hover:bg-white/[0.06]",
              )}
            >
              Editar
            </button>
            <button
              type="button"
              disabled={locked || deleting}
              onClick={remove}
              className={cn(
                "w-7 h-7 flex items-center justify-center rounded-md border border-red-500/[0.12] bg-red-500/[0.06]",
                locked || deleting ? "text-red-400/30 cursor-not-allowed" : "text-red-400 cursor-pointer hover:bg-red-500/[0.12]",
              )}
            >
              <Trash2 size={12} strokeWidth={1.5} />
            </button>
          </div>
        </div>

        {(foreignKeys.length > 0 || tableIndexes.length > 0) && (
          <div className="flex flex-wrap gap-1.5 mt-2.5 pl-[27px]">
            {foreignKeys.map((fk, i) => (
              <span
                key={`fk-${i}`}
                className="inline-flex items-center gap-1 text-[10px] font-medium px-2 py-0.5 rounded-full border border-white/[0.08] text-[#94A3B8]"
              >
                <Link2 size={9} strokeWidth={1.5} />
                {fk.text}
                {fk.onDelete && ` (${fk.onDelete})`}
              </span>
            ))}
            {tableIndexes.map((idx, i) => (
              <span
                key={`idx-${i}`}
                className="inline-flex items-center gap-1 text-[10px] font-medium px-2 py-0.5 rounded-full border border-white/[0.08] text-[#94A3B8]"
              >
                {idx.name} ({idx.columns.join(", ")}){idx.unique ? " UNIQUE" : ""}
              </span>
            ))}
          </div>
        )}

        {error && <p className="text-xs text-red-400 mt-2">{error}</p>}
      </div>
    );
  }

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
      className="bg-white/[0.04] border border-white/[0.08] rounded-xl overflow-hidden"
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <Table2 size={15} strokeWidth={1.5} className="text-[#B3D1FF] shrink-0" />
        <Input
          value={name}
          disabled={!isDraft}
          onChange={(e) => setName(e.target.value.toLowerCase().replace(/[\s-]+/g, "_"))}
          placeholder="tabela_1"
          className="h-8 px-3 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus"
        />
        <Select value={rls} onValueChange={setRls}>
          <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-3 brand-focus">
            <SelectValue />
          </SelectTrigger>
          <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
            <SelectItem value="disabled" className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
              Público
            </SelectItem>
            <SelectItem
              value="enabled"
              disabled={!authEmailEnabled}
              className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
            >
              Restrito
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      {!authEmailEnabled && (
        <p className="px-4 text-[11px] text-[#94A3B8]">
          "Restrito" exige autenticação por e-mail ligada para este app.
        </p>
      )}

      <div className="px-4 pb-4">
        <p className="text-[11px] text-[#94A3B8] mb-2">
          As colunas abaixo são criadas automaticamente e não podem ser editadas ou removidas.
        </p>
        <div className="grid gap-3 mb-1" style={{ gridTemplateColumns: "1fr 140px 80px 80px 32px 40px" }}>
          <span className="text-[11px] text-[#94A3B8] font-semibold">Nome</span>
          <span className="text-[11px] text-[#94A3B8] font-semibold">Tipo</span>
          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">Req.</span>
          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">Único</span>
          <span />
          <span />
        </div>

        <div className="flex flex-col gap-2.5 mb-2.5">
          {AUTO_COLUMNS.map((auto) => (
            <div
              key={auto.name}
              className="grid gap-3 items-center max-md:flex max-md:flex-col max-md:gap-2 max-md:p-3 max-md:bg-white/[0.03] max-md:rounded-xl max-md:border max-md:border-white/[0.06]"
              style={{ gridTemplateColumns: "1fr 140px 80px 80px 32px 40px" }}
            >
              <div className="h-8 px-2.5 flex items-center gap-1.5 text-[13px] bg-white/[0.02] border border-white/[0.06] rounded-md text-[#94A3B8]">
                <Lock size={10} strokeWidth={1.5} className="shrink-0" />
                {auto.name}
              </div>
              <div className="contents max-md:flex max-md:items-center max-md:gap-2">
                <div className="h-8 w-[130px] max-md:flex-1 flex items-center px-2 text-[12px] bg-white/[0.02] border border-white/[0.06] rounded-md text-[#94A3B8]">
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
              </div>
            </div>
          ))}
        </div>

        <div className="flex flex-col gap-2.5 mb-3">
          {columns.map((col, ci) => (
            <div key={ci} className="max-md:p-3 max-md:bg-white/[0.03] max-md:rounded-xl max-md:border max-md:border-white/[0.06]">
              <div
                className="grid gap-3 items-center max-md:flex max-md:flex-col max-md:gap-2"
                style={{ gridTemplateColumns: "1fr 140px 80px 80px 32px 40px" }}
              >
                <Input
                  value={col.name}
                  onChange={(e) =>
                    updateColumn(ci, { name: e.target.value.toLowerCase().replace(/[\s-]+/g, "_") })
                  }
                  placeholder="nome_coluna"
                  className="h-8 px-2.5 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus"
                />
                <div className="contents max-md:flex max-md:items-center max-md:gap-2">
                  <Select value={col.type} onValueChange={(val) => updateColumn(ci, { type: val })}>
                    <SelectTrigger className="h-8 w-[130px] max-md:flex-1 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                      {COLUMN_TYPES.map((t) => (
                        <SelectItem key={t} value={t} className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
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
                  <button
                    type="button"
                    title="Referenciar outra tabela (FK)"
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
                        ? "border-[var(--brand-light)]/40 bg-[var(--brand-light)]/10 text-[var(--brand-light)]"
                        : "border-white/[0.10] bg-transparent text-[#94A3B8]",
                      otherTables.length === 0 && "opacity-30 cursor-not-allowed",
                    )}
                  >
                    <Link2 size={12} strokeWidth={1.5} />
                  </button>
                  <button
                    type="button"
                    title="Remove column"
                    onClick={() => removeColumn(ci)}
                    disabled={columns.length <= 1}
                    className={cn(
                      "w-7 h-7 flex items-center justify-center rounded-md border border-red-500/[0.12] bg-red-500/[0.06] transition-colors",
                      columns.length <= 1 ? "text-red-400/30 cursor-not-allowed" : "text-red-400 cursor-pointer hover:bg-red-500/[0.12]",
                    )}
                  >
                    <Trash2 size={12} strokeWidth={1.5} />
                  </button>
                </div>
              </div>

              {col.references && (
                <div className="flex flex-wrap items-center gap-2 mt-2 pl-1">
                  <span className="text-[11px] text-[#94A3B8]">Referencia</span>
                  <Select
                    value={col.references.table}
                    onValueChange={(val) =>
                      updateColumn(ci, { references: { ...col.references!, table: val, column: "id" } })
                    }
                  >
                    <SelectTrigger className="h-7 w-[140px] text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-2 brand-focus">
                      <SelectValue placeholder="tabela" />
                    </SelectTrigger>
                    <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                      {otherTables.map((t) => (
                        <SelectItem key={t.name} value={t.name} className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
                          {t.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <span className="text-[11px] text-[#94A3B8]">coluna</span>
                  <Select
                    value={col.references.column}
                    onValueChange={(val) => updateColumn(ci, { references: { ...col.references!, column: val } })}
                  >
                    <SelectTrigger className="h-7 w-[110px] text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                      {referenceTargetColumns(col.references.table).map((c) => (
                        <SelectItem key={c} value={c} className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
                          {c}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <span className="text-[11px] text-[#94A3B8]">ao excluir</span>
                  <Select
                    value={col.references.on_delete ?? "no_action"}
                    onValueChange={(val) =>
                      updateColumn(ci, {
                        references: { ...col.references!, on_delete: val as ReferenceDef["on_delete"] },
                      })
                    }
                  >
                    <SelectTrigger className="h-7 w-[170px] text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] rounded-md px-2 brand-focus">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                      {ON_DELETE_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value!} className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
                          {opt.label}
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
          className="flex items-center gap-1.5 text-[12px] font-semibold bg-transparent border border-white/[0.08] rounded-full px-3 py-1.5 cursor-pointer hover:bg-white/[0.06] transition-colors self-start"
          style={{ color: "var(--brand-light)" }}
        >
          <Plus size={11} strokeWidth={2} />
          Adicionar Coluna
        </button>

        <div className="mt-4 pt-4 border-t border-white/[0.06]">
          <p className="text-[11px] text-[#94A3B8] font-semibold mb-2">Índices</p>

          <div className="flex flex-col gap-2 mb-2">
            {indexes.map((idx, ii) => (
              <div
                key={ii}
                className="flex flex-wrap items-center gap-2 bg-white/[0.03] border border-white/[0.06] rounded-lg px-3 py-2"
              >
                <Input
                  value={idx.name}
                  onChange={(e) => updateIndex(ii, { name: e.target.value.toLowerCase().replace(/[\s-]+/g, "_") })}
                  placeholder="nome_indice"
                  className="h-7 w-[160px] px-2 py-1 text-[12px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 brand-focus"
                />
                <div className="flex flex-wrap gap-1">
                  {columns.filter((c) => c.name.trim()).map((c) => (
                    <button
                      key={c.name}
                      type="button"
                      onClick={() => toggleIndexColumn(ii, c.name)}
                      className={cn(
                        "text-[11px] px-2 py-0.5 rounded-full border transition-colors",
                        idx.columns.includes(c.name)
                          ? "border-[var(--brand-light)]/40 bg-[var(--brand-light)]/10 text-[var(--brand-light)]"
                          : "border-white/[0.10] text-[#94A3B8] hover:bg-white/[0.06]",
                      )}
                    >
                      {c.name}
                    </button>
                  ))}
                </div>
                <div className="flex items-center gap-1.5 ml-auto">
                  <span className="text-[11px] text-[#94A3B8]">Único</span>
                  <Switch
                    checked={!!idx.unique}
                    onCheckedChange={(val) => updateIndex(ii, { unique: val })}
                    className="h-5 w-9"
                  />
                </div>
                <button
                  type="button"
                  title="Remove index"
                  onClick={() => removeIndex(ii)}
                  className="w-7 h-7 flex items-center justify-center rounded-md border border-red-500/[0.12] bg-red-500/[0.06] text-red-400 cursor-pointer hover:bg-red-500/[0.12] transition-colors"
                >
                  <Trash2 size={12} strokeWidth={1.5} />
                </button>
              </div>
            ))}
          </div>

          <button
            type="button"
            onClick={addIndex}
            className="flex items-center gap-1.5 text-[12px] font-semibold bg-transparent border border-white/[0.08] rounded-full px-3 py-1.5 cursor-pointer hover:bg-white/[0.06] transition-colors self-start"
            style={{ color: "var(--brand-light)" }}
          >
            <Plus size={11} strokeWidth={2} />
            Adicionar Índice
          </button>
        </div>

        {error && <p className="text-xs text-red-400 mt-3">{error}</p>}

        <div className="flex items-center gap-2 mt-4">
          <button
            type="button"
            onClick={save}
            disabled={saving}
            className="text-[12px] font-semibold px-4 py-1.5 rounded-full text-white cursor-pointer disabled:opacity-50"
            style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}
          >
            {saving ? "Salvando..." : "Salvar tabela"}
          </button>
          <button
            type="button"
            onClick={cancel}
            disabled={saving}
            className="text-[12px] font-medium px-4 py-1.5 rounded-full border border-white/[0.10] bg-transparent text-[#94A3B8] cursor-pointer hover:bg-white/[0.06]"
          >
            Cancelar
          </button>
        </div>
      </div>
    </motion.div>
  );
}
