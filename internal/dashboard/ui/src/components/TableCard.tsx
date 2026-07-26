import { useState } from "react";
import { motion } from "framer-motion";
import { Plus, Trash2, Table2 } from "lucide-react";
import { TableDef, ColumnDef } from "../lib/api";
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

const emptyColumn = (): ColumnDef => ({
  name: "",
  type: "text",
  required: false,
  default: "",
  unique: false,
});

interface TableCardProps {
  table: TableDef;
  authEmailEnabled: boolean;
  locked: boolean;
  startInEdit: boolean;
  onEnterEdit: () => void;
  onExitEdit: () => void;
  onCreate: (input: TableDef) => Promise<TableDef>;
  onUpdate: (input: { rls: string; columns: ColumnDef[] }) => Promise<TableDef>;
  onDelete: () => Promise<void>;
  onSaved: (saved: TableDef) => void;
  onDiscardDraft: () => void;
  onDeleted: () => void;
}

export default function TableCard({
  table,
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
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const enterEdit = () => {
    setName(table.name);
    setRls(table.rls);
    setColumns(table.columns.map((c) => ({ ...c })));
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
    setSaving(true);
    try {
      if (isDraft) {
        const saved = await onCreate({ name, rls, columns });
        onSaved(saved);
      } else {
        const saved = await onUpdate({ rls, columns });
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
    return (
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-xl px-4 py-3 flex items-center justify-between">
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
          As colunas <code>id</code>, <code>created_at</code> e <code>updated_at</code> são criadas automaticamente.
        </p>
        <div className="grid gap-3 mb-1" style={{ gridTemplateColumns: "1fr 140px 80px 80px 40px" }}>
          <span className="text-[11px] text-[#94A3B8] font-semibold">Nome</span>
          <span className="text-[11px] text-[#94A3B8] font-semibold">Tipo</span>
          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">Req.</span>
          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">Único</span>
          <span />
        </div>

        <div className="flex flex-col gap-2.5 mb-3">
          {columns.map((col, ci) => (
            <div
              key={ci}
              className="grid gap-3 items-center max-md:flex max-md:flex-col max-md:gap-2 max-md:p-3 max-md:bg-white/[0.03] max-md:rounded-xl max-md:border max-md:border-white/[0.06]"
              style={{ gridTemplateColumns: "1fr 140px 80px 80px 40px" }}
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
