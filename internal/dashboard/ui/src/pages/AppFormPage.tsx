import { useState, useEffect } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowLeft, Plus, Trash2, ChevronDown, ChevronUp, Table2 } from 'lucide-react'
import { useCreateApp, useUpdateApp, useApps, AppDef, TableDef, ColumnDef } from '../lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

// ── Constants ──────────────────────────────────────────────────────────────────

const COLUMN_TYPES = [
  'text',
  'integer',
  'bigint',
  'boolean',
  'uuid',
  'timestamptz',
  'numeric',
  'jsonb',
]

const emptyColumn = (): ColumnDef => ({
  name: '',
  type: 'text',
  required: false,
  default: '',
  unique: false,
})

const emptyTable = (): TableDef => ({
  name: '',
  rls: 'disabled',
  columns: [emptyColumn()],
})

// ── Validation ─────────────────────────────────────────────────────────────────

function validateName(name: string): string | null {
  if (!name.trim()) return 'Nome obrigatório'
  if (!/^[a-z][a-z0-9_]*$/.test(name))
    return 'Apenas letras minúsculas, números e _ (máx 32), começando com letra'
  if (name.length > 32) return 'Máximo de 32 caracteres'
  return null
}

// ── Component ──────────────────────────────────────────────────────────────────

export default function AppFormPage() {
  const navigate = useNavigate()
  const { id } = useParams()
  const isEdit = Boolean(id)

  const { data: apps } = useApps()
  const editTarget = isEdit && apps ? apps.find((a) => a.id === id) : null

  const [appName, setAppName] = useState('')
  const [authEmail, setAuthEmail] = useState(false)
  const [tables, setTables] = useState<TableDef[]>([])
  const [collapsedTables, setCollapsedTables] = useState<Set<number>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [submitError, setSubmitError] = useState<string | null>(null)

  const createApp = useCreateApp()
  const updateApp = useUpdateApp()
  const isMutating = createApp.isPending || updateApp.isPending

  // Populate form when editing
  useEffect(() => {
    if (editTarget) {
      setAppName(editTarget.name)
      setAuthEmail(editTarget.auth_email_enabled)
      setTables(
        editTarget.tables.map((t) => ({
          ...t,
          columns: t.columns.map((c) => ({ ...c })),
        })),
      )
    } else if (!isEdit) {
      setAppName('')
      setAuthEmail(false)
      setTables([])
      setCollapsedTables(new Set())
    }
    setErrors({})
    setSubmitError(null)
  }, [editTarget, isEdit])

  // ── Table helpers ────────────────────────────────────────────────────────────

  const addTable = () => setTables((prev) => [...prev, emptyTable()])

  const removeTable = (ti: number) => {
    setTables((prev) => prev.filter((_, i) => i !== ti))
    setCollapsedTables((prev) => {
      const next = new Set(prev)
      next.delete(ti)
      return next
    })
  }

  const updateTable = (ti: number, patch: Partial<TableDef>) => {
    setTables((prev) => prev.map((t, i) => (i === ti ? { ...t, ...patch } : t)))
  }

  const toggleCollapse = (ti: number) => {
    setCollapsedTables((prev) => {
      const next = new Set(prev)
      next.has(ti) ? next.delete(ti) : next.add(ti)
      return next
    })
  }

  const addColumn = (ti: number) => {
    setTables((prev) => prev.map((t, i) => (i === ti ? { ...t, columns: [...t.columns, emptyColumn()] } : t)))
  }

  const removeColumn = (ti: number, ci: number) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti ? { ...t, columns: t.columns.filter((_, j) => j !== ci) } : t,
      ),
    )
  }

  const updateColumn = (ti: number, ci: number, patch: Partial<ColumnDef>) => {
    setTables((prev) =>
      prev.map((t, i) =>
        i === ti
          ? {
              ...t,
              columns: t.columns.map((c, j) => (j === ci ? { ...c, ...patch } : c)),
            }
          : t,
      ),
    )
  }

  // ── Validate ─────────────────────────────────────────────────────────────────

  function validate(): boolean {
    const errs: Record<string, string> = {}

    const nameErr = validateName(appName)
    if (nameErr) errs['appName'] = nameErr

    tables.forEach((table, ti) => {
      if (!table.name.trim()) errs[`table_${ti}_name`] = 'Nome da tabela obrigatório'
      if (table.columns.length === 0) errs[`table_${ti}_cols`] = 'Pelo menos 1 coluna'
      table.columns.forEach((col, ci) => {
        if (!col.name.trim()) errs[`col_${ti}_${ci}_name`] = 'Nome obrigatório'
        if (!col.type) errs[`col_${ti}_${ci}_type`] = 'Tipo obrigatório'
      })
    })

    setErrors(errs)
    return Object.keys(errs).length === 0
  }

  // ── Submit ───────────────────────────────────────────────────────────────────

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return
    setSubmitError(null)

    const payload = { name: appName, auth_email_enabled: authEmail, tables }

    try {
      if (isEdit && editTarget) {
        await updateApp.mutateAsync({ id: editTarget.id, ...payload })
      } else {
        await createApp.mutateAsync(payload)
      }
      navigate('/apps')
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Erro inesperado')
    }
  }

  // ── Render ───────────────────────────────────────────────────────────────────

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
    >
      {/* Back button */}
      <button
        type="button"
        onClick={() => navigate('/apps')}
        className="mb-6 flex items-center gap-2 text-[13px] text-[#94A3B8] hover:text-white transition-colors bg-transparent border-none cursor-pointer"
      >
        <ArrowLeft size={14} strokeWidth={1.5} />
        Voltar para Apps
      </button>

      {/* Header */}
      <div className="mb-8">
        <span className="mb-3 inline-block rounded-full border border-[#0347A5]/20 bg-[#0347A5]/12 px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em] text-[#B3D1FF]">
          {isEdit ? 'EDITAR APP' : 'NOVO APP'}
        </span>
        <h2 className="text-[22px] font-extrabold text-[#F8FAFC]">
          {isEdit ? (editTarget ? `Editar "${editTarget.name}"` : 'App não encontrado') : 'Criar Aplicativo'}
        </h2>
        <p className="mt-1 text-sm text-[#94A3B8]">
          {isEdit
            ? 'Altere as configurações do app e clique em Salvar'
            : 'Configure tabelas, colunas e permissões do seu novo app'}
        </p>
      </div>

      {isEdit && !editTarget ? (
        <div className="rounded-2xl border border-red-500/[0.18] bg-red-500/[0.06] px-6 py-5 text-sm text-red-400">
          App não encontrado.
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col gap-6">
          {/* ── Basic Info Card ── */}
          <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-5 flex flex-col gap-5">
            {/* App name */}
            <div className="flex flex-col gap-2">
              <Label className="text-[13px] font-semibold text-[#94A3B8]">
                Nome do App
              </Label>
              <Input
                value={appName}
                onChange={(e) => setAppName(e.target.value.toLowerCase().replace(/[\s-]+/g, '_'))}
                placeholder="meu_app"
                className={cn(
                  'bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60 h-10',
                  errors['appName'] && 'border-red-500/50 focus-visible:border-red-500/50',
                )}
              />
              {errors['appName'] && (
                <p className="text-xs text-red-400">{errors['appName']}</p>
              )}
              <p className="text-[11px] text-[#94A3B8]">
                Apenas minúsculas, números e underscore. Máx 32 chars, começando com letra.
              </p>
            </div>

            {/* Divider */}
            <div className="border-t border-white/[0.06]" />

            {/* Auth toggle */}
            <div className="flex items-center justify-between">
              <div className="flex flex-col gap-0.5">
                <p className="text-sm font-semibold text-[#F8FAFC]">
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
          </div>

          {/* ── Tables Section ── */}
          <div className="flex flex-col gap-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="h-6 w-1 rounded-full bg-gradient-to-b from-[#0347A5] to-[#7C3AED]" />
                <p className="text-[15px] font-extrabold text-[#F8FAFC]">Tabelas</p>
              </div>
              <button
                type="button"
                onClick={addTable}
                className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#F8FAFC] text-[13px] font-medium cursor-pointer hover:bg-white/[0.08] transition-colors"
              >
                <Plus size={13} strokeWidth={2} />
                Adicionar Tabela
              </button>
            </div>

            {tables.length === 0 && (
              <div className="flex flex-col items-center justify-center gap-3 py-14 text-[#94A3B8] border border-dashed border-white/[0.08] rounded-2xl">
                <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-white/[0.04] border border-white/[0.06]">
                  <Table2 size={18} strokeWidth={1} className="opacity-40" />
                </div>
                <div className="text-center">
                  <p className="text-[13px] font-medium">Nenhuma tabela</p>
                  <p className="text-[12px] text-white/30 mt-1">Adicione tabelas para começar a estruturar seu app</p>
                </div>
              </div>
            )}

            <div className="flex flex-col gap-3">
              {tables.map((table, ti) => {
                const isCollapsed = collapsedTables.has(ti)
                return (
                  <motion.div
                    key={ti}
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
                    className="bg-white/[0.04] border border-white/[0.08] rounded-xl overflow-hidden"
                  >
                    {/* Table header row */}
                    <div className="flex items-center gap-3 px-4 py-3">
                      <Table2 size={15} strokeWidth={1.5} className="text-[#B3D1FF] shrink-0" />
                      <Input
                        value={table.name}
                        onChange={(e) =>
                          updateTable(ti, { name: e.target.value.toLowerCase().replace(/[\s-]+/g, '_') })
                        }
                        placeholder={`tabela_${ti + 1}`}
                        className={cn(
                          'h-8 px-3 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60',
                          errors[`table_${ti}_name`] && 'border-red-500/50',
                        )}
                      />
                      <div className="flex flex-col gap-1 shrink-0">
                        <span className="text-[10px] font-semibold text-[#94A3B8] uppercase tracking-[0.06em] leading-none">
                          Acesso
                        </span>
                        <span className="text-[9px] text-[#64748B] leading-tight">
                          {table.rls === 'enabled' ? 'Só o dono vê' : 'Todos veem'}
                        </span>
                      </div>
                      <Select
                        value={table.rls}
                        onValueChange={(val) => updateTable(ti, { rls: val })}
                      >
                        <SelectTrigger className="h-8 w-[100px] shrink-0 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] focus:ring-[#0347A5]/40 rounded-md px-3">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="bg-[#0D0D14] border-white/[0.10] text-[#F8FAFC]">
                          <SelectItem value="disabled" className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
                            Público
                          </SelectItem>
                          <SelectItem value="enabled" className="text-[12px] focus:bg-white/[0.08] focus:text-[#F8FAFC]">
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
                          <p className="text-xs text-red-400 mb-1">{errors[`table_${ti}_cols`]}</p>
                        )}

                        {/* Column header */}
                        <div
                          className="grid gap-3 items-center"
                          style={{ gridTemplateColumns: '1fr 140px 80px 80px 40px' }}
                        >
                          <span className="text-[11px] text-[#94A3B8] font-semibold">Nome</span>
                          <span className="text-[11px] text-[#94A3B8] font-semibold">Tipo</span>
                          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">Req.</span>
                          <span className="text-[11px] text-[#94A3B8] font-semibold text-center">Único</span>
                          <span />
                        </div>

                        {/* Column rows */}
                        <div className="flex flex-col gap-2.5 mb-3">
                          {table.columns.map((col, ci) => (
                            <div
                              key={ci}
                              className="grid gap-3 items-center"
                              style={{ gridTemplateColumns: '1fr 140px 80px 80px 40px' }}
                            >
                              <Input
                                value={col.name}
                                onChange={(e) =>
                                  updateColumn(ti, ci, {
                                    name: e.target.value.toLowerCase().replace(/[\s-]+/g, '_'),
                                  })
                                }
                                placeholder="nome_coluna"
                                className={cn(
                                  'h-8 px-2.5 py-1.5 text-[13px] bg-white/[0.05] border-white/[0.10] rounded-md text-[#F8FAFC] placeholder:text-white/30 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60',
                                  errors[`col_${ti}_${ci}_name`] && 'border-red-500/50',
                                )}
                              />

                              <Select
                                value={col.type}
                                onValueChange={(val) => updateColumn(ti, ci, { type: val })}
                              >
                                <SelectTrigger className="h-8 text-[12px] bg-white/[0.05] border-white/[0.10] text-[#F8FAFC] focus:ring-[#0347A5]/40 rounded-md px-2">
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

                              {/* Required toggle */}
                              <div className="flex justify-center">
                                <Switch
                                  checked={col.required}
                                  onCheckedChange={(val) => updateColumn(ti, ci, { required: val })}
                                  className="data-[state=checked]:bg-[#0347A5] data-[state=unchecked]:bg-white/[0.10] h-5 w-9"
                                />
                              </div>

                              {/* Unique toggle */}
                              <div className="flex justify-center">
                                <Switch
                                  checked={col.unique}
                                  onCheckedChange={(val) => updateColumn(ti, ci, { unique: val })}
                                  className="data-[state=checked]:bg-[#7C3AED] data-[state=unchecked]:bg-white/[0.10] h-5 w-9"
                                />
                              </div>

                              {/* Remove column button */}
                              <button
                                type="button"
                                onClick={() => removeColumn(ti, ci)}
                                disabled={table.columns.length <= 1}
                                className={cn(
                                  'w-7 h-7 flex items-center justify-center rounded-md border border-red-500/[0.12] bg-red-500/[0.06] transition-colors',
                                  table.columns.length <= 1
                                    ? 'text-red-400/30 cursor-not-allowed'
                                    : 'text-red-400 cursor-pointer hover:bg-red-500/[0.12]',
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
                          className="flex items-center gap-1.5 text-[12px] font-semibold text-[#B3D1FF] bg-transparent border border-white/[0.08] rounded-full px-3 py-1.5 cursor-pointer hover:bg-white/[0.06] hover:text-[#D6E8FF] transition-colors self-start"
                        >
                          <Plus size={11} strokeWidth={2} />
                          Adicionar Coluna
                        </button>
                      </div>
                    )}
                  </motion.div>
                )
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
          <div className="flex items-center gap-3">
            <Button
              type="button"
              variant="outline"
              onClick={() => navigate('/apps')}
              className="rounded-xl px-5 py-2.5 text-sm font-semibold border-white/[0.10] bg-transparent text-[#94A3B8] hover:bg-white/[0.05] hover:text-white"
            >
              Cancelar
            </Button>
            <Button
              type="submit"
              disabled={isMutating}
              className={cn(
                'rounded-xl px-6 py-2.5 text-sm font-bold text-white border-none',
                isMutating
                  ? 'bg-[#0347A5]/50 cursor-not-allowed'
                  : 'bg-gradient-to-br from-[#0347A5] to-[#7C3AED] cursor-pointer hover:opacity-90',
              )}
            >
              {isMutating ? 'Salvando...' : isEdit ? 'Salvar Alterações' : 'Criar App'}
            </Button>
          </div>
        </form>
      )}
    </motion.div>
  )
}
