import { useState, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { X, Plus, Trash2, ChevronDown, ChevronUp, Table2 } from "lucide-react";
import {
  useCreateApp,
  useUpdateApp,
  AppDef,
  TableDef,
  ColumnDef,
} from "../lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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

const emptyTable = (): TableDef => ({
  name: "",
  rls: "disabled",
  columns: [emptyColumn()],
});


function validateName(name: string): string | null {
  if (!name.trim()) return "Nome obrigatório";
  if (!/^[a-z][a-z0-9_]*$/.test(name))
    return "Apenas letras minúsculas, números e _ (máx 32), começando com letra";
  if (name.length > 32) return "Máximo de 32 caracteres";
  return null;
}


interface Props {
  open: boolean;
  editTarget?: AppDef | null;
  onClose: () => void;
}

export default function CreateAppModal({ open, editTarget, onClose }: Props) {
  const isEdit = Boolean(editTarget);

  const [appName, setAppName] = useState("");
  const [authEmail, setAuthEmail] = useState(false);
  const [tables, setTables] = useState<TableDef[]>([]);
  const [collapsedTables, setCollapsedTables] = useState<Set<number>>(
    new Set(),
  );
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [submitError, setSubmitError] = useState<string | null>(null);

  const createApp = useCreateApp();
  const updateApp = useUpdateApp();
  const isMutating = createApp.isPending || updateApp.isPending;

  useEffect(() => {
    if (open && editTarget) {
      setAppName(editTarget.name);
      setAuthEmail(editTarget.auth_email_enabled);
      setTables(
        editTarget.tables.map((t) => ({
          ...t,
          columns: t.columns.map((c) => ({ ...c })),
        })),
      );
    } else if (open && !editTarget) {
      setAppName("");
      setAuthEmail(false);
      setTables([]);
      setCollapsedTables(new Set());
    }
    setErrors({});
    setSubmitError(null);
  }, [open, editTarget]);


  const addTable = () => {
    setTables((prev) => [...prev, emptyTable()]);
  };

  const removeTable = (ti: number) => {
    setTables((prev) => prev.filter((_, i) => i !== ti));
    setCollapsedTables((prev) => {
      const next = new Set(prev);
      next.delete(ti);
      return next;
    });
  };

  const updateTable = (ti: number, patch: Partial<TableDef>) => {
    setTables((prev) =>
      prev.map((t, i) => (i === ti ? { ...t, ...patch } : t)),
    );
  };

  const toggleCollapse = (ti: number) => {
    setCollapsedTables((prev) => {
      const next = new Set(prev);
      next.has(ti) ? next.delete(ti) : next.add(ti);
      return next;
    });
  };

  const addColumn = (ti: number) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti ? { ...t, columns: [...t.columns, emptyColumn()] } : t,
      ),
    );
  };

  const removeColumn = (ti: number, ci: number) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti ? { ...t, columns: t.columns.filter((_, j) => j !== ci) } : t,
      ),
    );
  };

  const updateColumn = (ti: number, ci: number, patch: Partial<ColumnDef>) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti
          ? {
              ...t,
              columns: t.columns.map((c, j) =>
                j === ci ? { ...c, ...patch } : c,
              ),
            }
          : t,
      ),
    );
  };


  function validate(): boolean {
    const errs: Record<string, string> = {};

    const nameErr = validateName(appName);
    if (nameErr) errs["appName"] = nameErr;

    tables.forEach((table, ti) => {
      if (!table.name.trim())
        errs[`table_${ti}_name`] = "Nome da tabela obrigatório";
      if (table.columns.length === 0)
        errs[`table_${ti}_cols`] = "Pelo menos 1 coluna";
      table.columns.forEach((col, ci) => {
        if (!col.name.trim()) errs[`col_${ti}_${ci}_name`] = "Nome obrigatório";
        if (!col.type) errs[`col_${ti}_${ci}_type`] = "Tipo obrigatório";
      });
    });

    setErrors(errs);
    return Object.keys(errs).length === 0;
  }


  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!validate()) return;
    setSubmitError(null);

    const payload = { name: appName, auth_email_enabled: authEmail, tables };

    try {
      if (isEdit && editTarget) {
        await updateApp.mutateAsync({ id: editTarget.id, ...payload });
      } else {
        await createApp.mutateAsync(payload);
      }
      onClose();
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Erro inesperado");
    }
  }


  return (
    <AnimatePresence>
      {open && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.2 }}
          onClick={onClose}
          className="fixed inset-0 flex items-center justify-center p-6 bg-black/70 backdrop-blur-sm z-50"
        >
          <motion.div
            initial={{ opacity: 0, scale: 0.97, y: 16 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.97, y: 16 }}
            transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
            onClick={(e) => e.stopPropagation()}
            className="w-full max-w-[680px] max-h-[90vh] flex flex-col bg-[#0D0D14] border border-white/[0.12] rounded-[20px] shadow-[0_24px_80px_rgba(0,0,0,0.6)] overflow-hidden"
          >
            {/* Header */}
            <div className="flex items-center justify-between px-6 pt-6 pb-5 border-b border-white/[0.08] shrink-0">
              <div>
                <span className="block mb-1 text-[10px] font-bold tracking-[0.12em] uppercase text-[#B3D1FF]">
                  {isEdit ? "EDITAR APP" : "NOVO APP"}
                </span>
                <h2 className="text-[17px] font-bold text-[#F8FAFC]">
                  {isEdit ? `Editar "${editTarget?.name}"` : "Criar Aplicativo"}
                </h2>
              </div>
              <motion.button
                type="button"
                whileHover={{ scale: 1.05 }}
                whileTap={{ scale: 0.95 }}
                onClick={onClose}
                className="w-8 h-8 flex items-center justify-center rounded-lg border border-white/[0.10] bg-white/[0.04] text-[#94A3B8] cursor-pointer hover:bg-white/[0.08] hover:text-[#F8FAFC] transition-colors"
              >
                <X size={15} strokeWidth={1.5} />
              </motion.button>
            </div>

            {/* Scrollable body */}
            <form
              onSubmit={handleSubmit}
              className="flex-1 min-h-0 overflow-y-auto px-6 pt-6 pb-5 flex flex-col gap-6"
            >
              {/* App name */}
              <div className="flex flex-col gap-2">
                <Label className="text-[13px] font-semibold text-[#94A3B8]">
                  Nome do App
                </Label>
                <Input
                  value={appName}
                  onChange={(e) => setAppName(e.target.value.toLowerCase().replace(/[\s-]+/g, "_"))}
                  placeholder="meu_app"
                  className={cn(
                    "bg-white/[0.05] border-white/[0.10] rounded-xl text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60 h-10",
                    errors["appName"] &&
                      "border-red-500/50 focus-visible:border-red-500/50",
                  )}
                />
                {errors["appName"] && (
                  <p className="text-xs text-red-400">{errors["appName"]}</p>
                )}
                <p className="text-[11px] text-[#94A3B8]">
                  Apenas minúsculas, números e underscore. Máx 32 chars, começando com letra.
                </p>
              </div>

              {/* Auth email toggle */}
              <div className="flex items-center justify-between bg-white/[0.04] border border-white/[0.08] rounded-xl px-4 py-3.5">
                <div>
                  <p className="text-sm font-semibold text-[#F8FAFC] mb-0.5">
                    Auth por Email
                  </p>
                  <p className="text-xs text-[#94A3B8]">
                    Habilita registro e login via email/senha
                  </p>
                </div>
                <Switch
                  checked={authEmail}
                  onCheckedChange={setAuthEmail}
                  className="data-[state=checked]:bg-[#0347A5] data-[state=unchecked]:bg-white/[0.12] shrink-0"
                />
              </div>

              {/* Tables */}
              <div className="flex flex-col gap-3">
                <div className="flex items-center justify-between">
                  <p className="text-sm font-bold text-[#F8FAFC]">Tabelas</p>
                  <motion.button
                    type="button"
                    whileHover={{ scale: 1.02 }}
                    whileTap={{ scale: 0.98 }}
                    onClick={addTable}
                    className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#F8FAFC] text-[13px] font-medium cursor-pointer hover:bg-white/[0.08] transition-colors"
                  >
                    <Plus size={13} strokeWidth={2} />
                    Adicionar Tabela
                  </motion.button>
                </div>

                {tables.length === 0 && (
                  <div className="text-center py-7 text-[#94A3B8] text-[13px] border border-dashed border-white/[0.08] rounded-xl">
                    Nenhuma tabela. Clique em "Adicionar Tabela" para começar.
                  </div>
                )}

                <div className="flex flex-col gap-3">
                  {tables.map((table, ti) => {
                    const isCollapsed = collapsedTables.has(ti);
                    return (
                      <motion.div
                        key={ti}
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{
                          duration: 0.25,
                          ease: [0.32, 0.72, 0, 1],
                        }}
                        className="bg-white/[0.04] border border-white/[0.08] rounded-xl overflow-hidden"
                      >
                        {/* Table header row */}
                        <div className="flex items-center gap-3 px-4 py-3">
                          <Table2
                            size={15}
                            strokeWidth={1.5}
                            className="text-[#B3D1FF] shrink-0"
                          />
                          <Input
                            value={table.name}
                            onChange={(e) =>
                              updateTable(ti, { name: e.target.value.toLowerCase().replace(/[\s-]+/g, "_") })
                            }
                            placeholder={`tabela_${ti + 1}`}
                            className={cn(
                              "h-8 px-3 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-xl text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60",
                              errors[`table_${ti}_name`] && "border-red-500/50",
                            )}
                          />
                          <div className="flex flex-col gap-1 shrink-0">
                            <span className="text-[10px] font-semibold text-[#94A3B8] uppercase tracking-[0.06em] leading-none">
                              Acesso
                            </span>
                            <span className="text-[9px] text-[#64748B] leading-tight">
                              {table.rls === "enabled"
                                ? "Só o dono vê"
                                : "Todos veem"}
                            </span>
                          </div>
                          <Select
                            value={table.rls}
                            onValueChange={(val) =>
                              updateTable(ti, { rls: val })
                            }
                          >
                            <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] focus:ring-[#0347A5]/40 rounded-xl px-3">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                              <SelectItem
                                value="disabled"
                                className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
                              >
                                Público
                              </SelectItem>
                              <SelectItem
                                value="enabled"
                                className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
                              >
                                Restrito
                              </SelectItem>
                            </SelectContent>
                          </Select>

                          {/* Collapse button */}
                          <button
                            type="button"
                            onClick={() => toggleCollapse(ti)}
                            className="w-7 h-7 shrink-0 flex items-center justify-center rounded-md border border-white/[0.08] bg-transparent text-[#94A3B8] cursor-pointer hover:bg-white/[0.06] transition-colors"
                          >
                            {isCollapsed ? (
                              <ChevronDown size={13} strokeWidth={1.5} />
                            ) : (
                              <ChevronUp size={13} strokeWidth={1.5} />
                            )}
                          </button>

                          {/* Remove table button */}
                          <button
                            type="button"
                            onClick={() => removeTable(ti)}
                            className="w-7 h-7 shrink-0 flex items-center justify-center rounded-md border border-red-500/[0.15] bg-red-500/[0.08] text-red-400 cursor-pointer hover:bg-red-500/[0.14] transition-colors"
                          >
                            <Trash2 size={13} strokeWidth={1.5} />
                          </button>
                        </div>

                        {!isCollapsed && (
                          <div className="border-t border-white/[0.06] px-4 py-3.5 flex flex-col gap-3">
                            {errors[`table_${ti}_cols`] && (
                              <p className="text-xs text-red-400 mb-1">
                                {errors[`table_${ti}_cols`]}
                              </p>
                            )}

                            {/* Column header */}
                            <div
                              className="grid gap-2 items-center"
                              style={{
                                gridTemplateColumns: "1fr 130px 72px 72px 36px",
                              }}
                            >
                              {["Nome", "Tipo", "Req.", "Único", ""].map(
                                (h) => (
                                  <span
                                    key={h}
                                    className="text-[11px] text-[#94A3B8] font-semibold"
                                  >
                                    {h}
                                  </span>
                                ),
                              )}
                            </div>

                            {/* Column rows */}
                            <div className="flex flex-col gap-2 mb-3">
                              {table.columns.map((col, ci) => (
                                <div
                                  key={ci}
                                  className="grid gap-2 items-center"
                                  style={{
                                    gridTemplateColumns:
                                      "1fr 130px 72px 72px 36px",
                                  }}
                                >
                                  <Input
                                    value={col.name}
                                    onChange={(e) =>
                                      updateColumn(ti, ci, {
                                        name: e.target.value.toLowerCase().replace(/[\s-]+/g, "_"),
                                      })
                                    }
                                    placeholder="nome_coluna"
                                    className={cn(
                                      "h-8 px-2.5 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-xl text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60",
                                      errors[`col_${ti}_${ci}_name`] &&
                                        "border-red-500/50",
                                    )}
                                  />

                                  <Select
                                    value={col.type}
                                    onValueChange={(val) =>
                                      updateColumn(ti, ci, { type: val })
                                    }
                                  >
                                    <SelectTrigger className="h-8 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] focus:ring-[#0347A5]/40 rounded-xl px-2">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                                      {COLUMN_TYPES.map((t) => (
                                        <SelectItem
                                          key={t}
                                          value={t}
                                          className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]"
                                        >
                                          {t}
                                        </SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>

                                  {/* Required toggle */}
                                  <div className="flex justify-center">
                                    <Switch
                                      checked={col.required}
                                      onCheckedChange={(val) =>
                                        updateColumn(ti, ci, { required: val })
                                      }
                                      className="data-[state=checked]:bg-[#0347A5] data-[state=unchecked]:bg-white/[0.10] h-5 w-9"
                                    />
                                  </div>

                                  {/* Unique toggle */}
                                  <div className="flex justify-center">
                                    <Switch
                                      checked={col.unique}
                                      onCheckedChange={(val) =>
                                        updateColumn(ti, ci, { unique: val })
                                      }
                                      className="data-[state=checked]:bg-[#7C3AED] data-[state=unchecked]:bg-white/[0.10] h-5 w-9"
                                    />
                                  </div>

                                  {/* Remove column button */}
                                  <button
                                    type="button"
                                    onClick={() => removeColumn(ti, ci)}
                                    disabled={table.columns.length <= 1}
                                    className={cn(
                                      "w-7 h-7 flex items-center justify-center rounded-md border border-red-500/[0.12] bg-red-500/[0.06] transition-colors",
                                      table.columns.length <= 1
                                        ? "text-red-400/30 cursor-not-allowed"
                                        : "text-red-400 cursor-pointer hover:bg-red-500/[0.12]",
                                    )}
                                  >
                                    <Trash2 size={12} strokeWidth={1.5} />
                                  </button>
                                </div>
                              ))}
                            </div>

                            <button
                              type="button"
                              onClick={() => addColumn(ti)}
                              className="flex items-center gap-1.5 text-[12px] font-semibold text-[#B3D1FF] bg-transparent border-none cursor-pointer p-0 hover:text-[#D6E8FF] transition-colors"
                            >
                              <Plus size={12} strokeWidth={2} />
                              Adicionar Coluna
                            </button>
                          </div>
                        )}
                      </motion.div>
                    );
                  })}
                </div>
              </div>

              {/* Submit error */}
              {submitError && (
                <div className="px-4 py-3 rounded-xl bg-red-500/[0.08] border border-red-500/[0.20] text-red-400 text-[13px]">
                  {submitError}
                </div>
              )}

              {/* Submit button */}
              <motion.div
                whileHover={{ scale: 1.01 }}
                whileTap={{ scale: 0.99 }}
              >
                <Button
                  type="submit"
                  disabled={isMutating}
                  className={cn(
                    "w-full h-12 rounded-[14px] text-[15px] font-bold text-white border-none",
                    isMutating
                      ? "bg-[#0347A5]/50 cursor-not-allowed"
                      : "bg-gradient-to-br from-[#0347A5] to-[#7C3AED] cursor-pointer hover:opacity-90",
                  )}
                >
                  {isMutating
                    ? "Salvando..."
                    : isEdit
                      ? "Salvar Alterações"
                      : "Criar App"}
                </Button>
              </motion.div>
            </form>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
