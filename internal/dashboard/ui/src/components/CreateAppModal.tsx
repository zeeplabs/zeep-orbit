import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { X, Plus, Trash2, ChevronDown, ChevronUp, Table2 } from 'lucide-react'
import { useCreateApp, useUpdateApp, AppDef, TableDef, ColumnDef } from '../lib/api'

// ── Constants ──────────────────────────────────────────────────────────────────

const COLUMN_TYPES = [
  'text', 'integer', 'bigint', 'boolean', 'uuid', 'timestamptz', 'numeric', 'jsonb',
]

const emptyColumn = (): ColumnDef => ({
  name: '', type: 'text', required: false, default: '', unique: false,
})

const emptyTable = (): TableDef => ({
  name: '', rls: 'disabled', columns: [emptyColumn()],
})

// ── Shared input style ─────────────────────────────────────────────────────────

const inputStyle: React.CSSProperties = {
  background: 'rgba(255,255,255,0.05)',
  border: '1px solid rgba(255,255,255,0.10)',
  borderRadius: 10,
  padding: '10px 14px',
  color: '#F8FAFC',
  outline: 'none',
  fontSize: 14,
  width: '100%',
  fontFamily: 'inherit',
  transition: 'border-color 0.2s',
}

const selectStyle: React.CSSProperties = {
  ...inputStyle,
  cursor: 'pointer',
  appearance: 'none' as React.CSSProperties['appearance'],
}

// ── Validation ─────────────────────────────────────────────────────────────────

function validateName(name: string): string | null {
  if (!name.trim()) return 'Nome obrigatório'
  if (!/^[a-z0-9-]+$/.test(name)) return 'Apenas letras minúsculas, números e hífens'
  if (name.length > 32) return 'Máximo de 32 caracteres'
  return null
}

// ── Component ──────────────────────────────────────────────────────────────────

interface Props {
  open: boolean
  editTarget?: AppDef | null
  onClose: () => void
}

export default function CreateAppModal({ open, editTarget, onClose }: Props) {
  const isEdit = Boolean(editTarget)

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
    if (open && editTarget) {
      setAppName(editTarget.name)
      setAuthEmail(editTarget.auth_email_enabled)
      setTables(editTarget.tables.map(t => ({
        ...t,
        columns: t.columns.map(c => ({ ...c })),
      })))
    } else if (open && !editTarget) {
      setAppName('')
      setAuthEmail(false)
      setTables([])
      setCollapsedTables(new Set())
    }
    setErrors({})
    setSubmitError(null)
  }, [open, editTarget])

  // ── Table helpers ────────────────────────────────────────────────────────────

  const addTable = () => {
    setTables(prev => [...prev, emptyTable()])
  }

  const removeTable = (ti: number) => {
    setTables(prev => prev.filter((_, i) => i !== ti))
    setCollapsedTables(prev => {
      const next = new Set(prev)
      next.delete(ti)
      return next
    })
  }

  const updateTable = (ti: number, patch: Partial<TableDef>) => {
    setTables(prev => prev.map((t, i) => i === ti ? { ...t, ...patch } : t))
  }

  const toggleCollapse = (ti: number) => {
    setCollapsedTables(prev => {
      const next = new Set(prev)
      next.has(ti) ? next.delete(ti) : next.add(ti)
      return next
    })
  }

  const addColumn = (ti: number) => {
    setTables(prev => prev.map((t, i) =>
      i === ti ? { ...t, columns: [...t.columns, emptyColumn()] } : t
    ))
  }

  const removeColumn = (ti: number, ci: number) => {
    setTables(prev => prev.map((t, i) =>
      i === ti ? { ...t, columns: t.columns.filter((_, j) => j !== ci) } : t
    ))
  }

  const updateColumn = (ti: number, ci: number, patch: Partial<ColumnDef>) => {
    setTables(prev => prev.map((t, i) =>
      i === ti
        ? { ...t, columns: t.columns.map((c, j) => j === ci ? { ...c, ...patch } : c) }
        : t
    ))
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
      onClose()
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Erro inesperado')
    }
  }

  // ── Render ───────────────────────────────────────────────────────────────────

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.2 }}
          onClick={onClose}
          style={{
            position: 'fixed', inset: 0,
            background: 'rgba(0,0,0,0.60)',
            backdropFilter: 'blur(6px)',
            zIndex: 50,
            display: 'flex', alignItems: 'flex-end', justifyContent: 'center',
            padding: '0 0 0 0',
          }}
        >
          <motion.div
            initial={{ opacity: 0, y: 40 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 40 }}
            transition={{ duration: 0.4, ease: [0.32, 0.72, 0, 1] }}
            onClick={e => e.stopPropagation()}
            style={{
              width: '100%',
              maxWidth: 720,
              maxHeight: '92vh',
              margin: '0 auto',
              background: 'rgba(255,255,255,0.05)',
              border: '1px solid rgba(255,255,255,0.10)',
              borderRadius: '20px 20px 0 0',
              padding: 6,
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            {/* inner bezel */}
            <div style={{
              background: 'rgba(255,255,255,0.03)',
              boxShadow: 'inset 0 1px 1px rgba(255,255,255,0.08)',
              borderRadius: '16px 16px 0 0',
              display: 'flex',
              flexDirection: 'column',
              flex: 1,
              overflow: 'hidden',
            }}>
              {/* Header */}
              <div style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                padding: '20px 24px 16px',
                borderBottom: '1px solid rgba(255,255,255,0.06)',
                flexShrink: 0,
              }}>
                <div>
                  <span style={{
                    fontSize: 10, fontWeight: 700, letterSpacing: '0.1em',
                    color: 'var(--accent-light)', textTransform: 'uppercase',
                    display: 'block', marginBottom: 4,
                  }}>
                    {isEdit ? 'EDITAR APP' : 'NOVO APP'}
                  </span>
                  <h2 style={{ fontSize: 18, fontWeight: 700 }}>
                    {isEdit ? `Editar "${editTarget?.name}"` : 'Criar Aplicativo'}
                  </h2>
                </div>
                <motion.button
                  whileHover={{ scale: 1.05 }}
                  whileTap={{ scale: 0.95 }}
                  onClick={onClose}
                  style={{
                    width: 32, height: 32, borderRadius: 8,
                    border: '1px solid rgba(255,255,255,0.10)',
                    background: 'rgba(255,255,255,0.05)',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    cursor: 'pointer', color: 'var(--text-muted)',
                  }}
                >
                  <X size={16} strokeWidth={1.5} />
                </motion.button>
              </div>

              {/* Scrollable body */}
              <form
                onSubmit={handleSubmit}
                style={{ flex: 1, overflowY: 'auto', padding: '24px' }}
              >
                {/* App name */}
                <div style={{ marginBottom: 20 }}>
                  <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 8, color: 'var(--text-muted)' }}>
                    Nome do App
                  </label>
                  <input
                    value={appName}
                    onChange={e => setAppName(e.target.value)}
                    placeholder="meu-app"
                    style={{
                      ...inputStyle,
                      borderColor: errors['appName'] ? 'rgba(239,68,68,0.5)' : 'rgba(255,255,255,0.10)',
                    }}
                    onFocus={e => (e.target.style.borderColor = 'rgba(3,71,165,0.6)')}
                    onBlur={e => (e.target.style.borderColor = errors['appName'] ? 'rgba(239,68,68,0.5)' : 'rgba(255,255,255,0.10)')}
                  />
                  {errors['appName'] && (
                    <p style={{ fontSize: 12, color: '#EF4444', marginTop: 4 }}>{errors['appName']}</p>
                  )}
                  <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
                    Apenas minúsculas, números e hífens. Máx 32 chars.
                  </p>
                </div>

                {/* Auth email toggle */}
                <div style={{
                  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                  background: 'rgba(255,255,255,0.04)',
                  border: '1px solid rgba(255,255,255,0.08)',
                  borderRadius: 12, padding: '14px 16px',
                  marginBottom: 28,
                }}>
                  <div>
                    <p style={{ fontSize: 14, fontWeight: 600, marginBottom: 2 }}>Auth por Email</p>
                    <p style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                      Habilita registro e login via email/senha
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setAuthEmail(v => !v)}
                    style={{
                      width: 44, height: 24, borderRadius: 12,
                      background: authEmail ? 'var(--accent)' : 'rgba(255,255,255,0.12)',
                      border: 'none', cursor: 'pointer', padding: 2,
                      transition: 'background 0.2s',
                      flexShrink: 0,
                    }}
                  >
                    <motion.div
                      animate={{ x: authEmail ? 20 : 0 }}
                      transition={{ duration: 0.2 }}
                      style={{
                        width: 20, height: 20, borderRadius: 10,
                        background: '#fff',
                      }}
                    />
                  </button>
                </div>

                {/* Tables */}
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
                    <p style={{ fontSize: 14, fontWeight: 700 }}>Tabelas</p>
                    <motion.button
                      type="button"
                      whileHover={{ scale: 1.02 }}
                      whileTap={{ scale: 0.98 }}
                      onClick={addTable}
                      style={{
                        display: 'flex', alignItems: 'center', gap: 6,
                        padding: '6px 14px', borderRadius: 20,
                        border: '1px solid rgba(255,255,255,0.12)',
                        background: 'rgba(255,255,255,0.05)',
                        color: 'var(--text)', fontSize: 13, fontWeight: 500,
                        cursor: 'pointer', fontFamily: 'inherit',
                      }}
                    >
                      <Plus size={13} strokeWidth={2} /> Adicionar Tabela
                    </motion.button>
                  </div>

                  {tables.length === 0 && (
                    <div style={{
                      textAlign: 'center', padding: '28px 0',
                      color: 'var(--text-muted)', fontSize: 13,
                      border: '1px dashed rgba(255,255,255,0.08)',
                      borderRadius: 12,
                    }}>
                      Nenhuma tabela. Clique em "Adicionar Tabela" para começar.
                    </div>
                  )}

                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {tables.map((table, ti) => {
                      const isCollapsed = collapsedTables.has(ti)
                      return (
                        <motion.div
                          key={ti}
                          initial={{ opacity: 0, y: 8 }}
                          animate={{ opacity: 1, y: 0 }}
                          transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
                          style={{
                            background: 'rgba(255,255,255,0.04)',
                            border: '1px solid rgba(255,255,255,0.08)',
                            borderRadius: 14,
                            overflow: 'hidden',
                          }}
                        >
                          {/* Table header row */}
                          <div style={{
                            display: 'flex', alignItems: 'center', gap: 10,
                            padding: '12px 14px',
                          }}>
                            <Table2 size={15} strokeWidth={1.5} style={{ color: 'var(--accent-light)', flexShrink: 0 }} />
                            <input
                              value={table.name}
                              onChange={e => updateTable(ti, { name: e.target.value })}
                              placeholder={`tabela_${ti + 1}`}
                              style={{
                                ...inputStyle,
                                padding: '7px 12px',
                                fontSize: 13,
                                borderColor: errors[`table_${ti}_name`] ? 'rgba(239,68,68,0.5)' : 'rgba(255,255,255,0.10)',
                              }}
                              onFocus={e => (e.target.style.borderColor = 'rgba(3,71,165,0.6)')}
                              onBlur={e => (e.target.style.borderColor = errors[`table_${ti}_name`] ? 'rgba(239,68,68,0.5)' : 'rgba(255,255,255,0.10)')}
                            />
                            <select
                              value={table.rls}
                              onChange={e => updateTable(ti, { rls: e.target.value })}
                              style={{ ...selectStyle, width: 120, padding: '7px 12px', fontSize: 12, flexShrink: 0 }}
                            >
                              <option value="disabled">RLS off</option>
                              <option value="enabled">RLS on</option>
                            </select>
                            <button
                              type="button"
                              onClick={() => toggleCollapse(ti)}
                              style={{
                                width: 28, height: 28, borderRadius: 6, flexShrink: 0,
                                border: '1px solid rgba(255,255,255,0.08)',
                                background: 'transparent', color: 'var(--text-muted)',
                                cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center',
                              }}
                            >
                              {isCollapsed
                                ? <ChevronDown size={13} strokeWidth={1.5} />
                                : <ChevronUp size={13} strokeWidth={1.5} />}
                            </button>
                            <button
                              type="button"
                              onClick={() => removeTable(ti)}
                              style={{
                                width: 28, height: 28, borderRadius: 6, flexShrink: 0,
                                border: '1px solid rgba(239,68,68,0.15)',
                                background: 'rgba(239,68,68,0.08)', color: '#EF4444',
                                cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center',
                              }}
                            >
                              <Trash2 size={13} strokeWidth={1.5} />
                            </button>
                          </div>

                          {!isCollapsed && (
                            <div style={{ borderTop: '1px solid rgba(255,255,255,0.06)', padding: '12px 14px' }}>
                              {errors[`table_${ti}_cols`] && (
                                <p style={{ fontSize: 12, color: '#EF4444', marginBottom: 8 }}>
                                  {errors[`table_${ti}_cols`]}
                                </p>
                              )}

                              {/* Column header */}
                              <div style={{
                                display: 'grid',
                                gridTemplateColumns: '1fr 110px 60px 60px 28px',
                                gap: 8, alignItems: 'center',
                                marginBottom: 6,
                              }}>
                                {['Nome', 'Tipo', 'Req.', 'Único', ''].map(h => (
                                  <span key={h} style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600 }}>{h}</span>
                                ))}
                              </div>

                              {/* Column rows */}
                              <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 10 }}>
                                {table.columns.map((col, ci) => (
                                  <div
                                    key={ci}
                                    style={{
                                      display: 'grid',
                                      gridTemplateColumns: '1fr 110px 60px 60px 28px',
                                      gap: 8, alignItems: 'center',
                                    }}
                                  >
                                    <input
                                      value={col.name}
                                      onChange={e => updateColumn(ti, ci, { name: e.target.value })}
                                      placeholder="nome_coluna"
                                      style={{
                                        ...inputStyle, padding: '7px 10px', fontSize: 13,
                                        borderColor: errors[`col_${ti}_${ci}_name`] ? 'rgba(239,68,68,0.5)' : 'rgba(255,255,255,0.10)',
                                      }}
                                      onFocus={e => (e.target.style.borderColor = 'rgba(3,71,165,0.6)')}
                                      onBlur={e => (e.target.style.borderColor = 'rgba(255,255,255,0.10)')}
                                    />
                                    <select
                                      value={col.type}
                                      onChange={e => updateColumn(ti, ci, { type: e.target.value })}
                                      style={{ ...selectStyle, padding: '7px 8px', fontSize: 12 }}
                                    >
                                      {COLUMN_TYPES.map(t => (
                                        <option key={t} value={t}>{t}</option>
                                      ))}
                                    </select>
                                    {/* Required toggle */}
                                    <div style={{ display: 'flex', justifyContent: 'center' }}>
                                      <button
                                        type="button"
                                        onClick={() => updateColumn(ti, ci, { required: !col.required })}
                                        style={{
                                          width: 36, height: 20, borderRadius: 10,
                                          background: col.required ? 'var(--accent)' : 'rgba(255,255,255,0.10)',
                                          border: 'none', cursor: 'pointer', padding: 2,
                                          transition: 'background 0.2s',
                                        }}
                                      >
                                        <motion.div
                                          animate={{ x: col.required ? 16 : 0 }}
                                          transition={{ duration: 0.15 }}
                                          style={{ width: 16, height: 16, borderRadius: 8, background: '#fff' }}
                                        />
                                      </button>
                                    </div>
                                    {/* Unique toggle */}
                                    <div style={{ display: 'flex', justifyContent: 'center' }}>
                                      <button
                                        type="button"
                                        onClick={() => updateColumn(ti, ci, { unique: !col.unique })}
                                        style={{
                                          width: 36, height: 20, borderRadius: 10,
                                          background: col.unique ? '#7C3AED' : 'rgba(255,255,255,0.10)',
                                          border: 'none', cursor: 'pointer', padding: 2,
                                          transition: 'background 0.2s',
                                        }}
                                      >
                                        <motion.div
                                          animate={{ x: col.unique ? 16 : 0 }}
                                          transition={{ duration: 0.15 }}
                                          style={{ width: 16, height: 16, borderRadius: 8, background: '#fff' }}
                                        />
                                      </button>
                                    </div>
                                    <button
                                      type="button"
                                      onClick={() => removeColumn(ti, ci)}
                                      disabled={table.columns.length <= 1}
                                      style={{
                                        width: 28, height: 28, borderRadius: 6,
                                        border: '1px solid rgba(239,68,68,0.12)',
                                        background: 'rgba(239,68,68,0.06)', color: table.columns.length <= 1 ? 'rgba(239,68,68,0.3)' : '#EF4444',
                                        cursor: table.columns.length <= 1 ? 'not-allowed' : 'pointer',
                                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                                      }}
                                    >
                                      <Trash2 size={12} strokeWidth={1.5} />
                                    </button>
                                  </div>
                                ))}
                              </div>

                              <button
                                type="button"
                                onClick={() => addColumn(ti)}
                                style={{
                                  display: 'flex', alignItems: 'center', gap: 5,
                                  fontSize: 12, color: 'var(--accent-light)',
                                  background: 'transparent', border: 'none',
                                  cursor: 'pointer', fontFamily: 'inherit', fontWeight: 600,
                                  padding: 0,
                                }}
                              >
                                <Plus size={12} strokeWidth={2} /> Adicionar Coluna
                              </button>
                            </div>
                          )}
                        </motion.div>
                      )
                    })}
                  </div>
                </div>

                {/* Error */}
                {submitError && (
                  <div style={{
                    marginTop: 20, padding: '12px 16px', borderRadius: 10,
                    background: 'rgba(239,68,68,0.08)',
                    border: '1px solid rgba(239,68,68,0.2)',
                    color: '#EF4444', fontSize: 13,
                  }}>
                    {submitError}
                  </div>
                )}

                {/* Submit */}
                <motion.button
                  type="submit"
                  whileHover={{ scale: 1.01 }}
                  whileTap={{ scale: 0.99 }}
                  disabled={isMutating}
                  style={{
                    marginTop: 24,
                    width: '100%', padding: '13px 0', borderRadius: 14,
                    border: 'none',
                    background: isMutating ? 'rgba(3,71,165,0.5)' : 'var(--accent)',
                    color: '#fff', fontSize: 15, fontWeight: 700,
                    cursor: isMutating ? 'not-allowed' : 'pointer',
                    fontFamily: 'inherit',
                  }}
                >
                  {isMutating ? 'Salvando...' : isEdit ? 'Salvar Alterações' : 'Criar App'}
                </motion.button>
              </form>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
