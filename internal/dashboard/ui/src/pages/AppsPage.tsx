import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Plus, Pencil, Trash2, Table2, Mail, MailX, LayoutGrid } from 'lucide-react'
import { useApps, useDeleteApp, AppDef } from '../lib/api'
import CreateAppModal from '../components/CreateAppModal'
import DeleteConfirmDialog from '../components/DeleteConfirmDialog'

// ── Animation presets ──────────────────────────────────────────────────────────

const ease = [0.32, 0.72, 0, 1] as const

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
}

// ── Skeleton card ──────────────────────────────────────────────────────────────

function SkeletonCard({ wide = false }: { wide?: boolean }) {
  return (
    <div
      style={{
        gridColumn: wide ? 'span 2' : 'span 1',
        background: 'rgba(255,255,255,0.05)',
        border: '1px solid rgba(255,255,255,0.08)',
        borderRadius: 20,
        padding: 6,
      }}
    >
      <div style={{
        background: 'rgba(255,255,255,0.03)',
        borderRadius: 16,
        padding: 20,
        height: 160,
        position: 'relative',
        overflow: 'hidden',
      }}>
        <div style={{
          position: 'absolute', inset: 0,
          background: 'linear-gradient(90deg, transparent 0%, rgba(255,255,255,0.04) 50%, transparent 100%)',
          animation: 'shimmer 1.6s infinite',
        }} />
        <div style={{ width: '60%', height: 18, background: 'rgba(255,255,255,0.07)', borderRadius: 6, marginBottom: 10 }} />
        <div style={{ width: '35%', height: 14, background: 'rgba(255,255,255,0.05)', borderRadius: 5, marginBottom: 6 }} />
        <div style={{ width: '45%', height: 14, background: 'rgba(255,255,255,0.05)', borderRadius: 5 }} />
      </div>
    </div>
  )
}

// ── App card ───────────────────────────────────────────────────────────────────

interface AppCardProps {
  app: AppDef
  wide: boolean
  index: number
  onEdit: (app: AppDef) => void
  onDelete: (app: AppDef) => void
}

function AppCard({ app, wide, index, onEdit, onDelete }: AppCardProps) {
  const createdAt = new Date(app.created_at).toLocaleDateString('pt-BR', {
    day: '2-digit', month: 'short', year: 'numeric',
  })

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease, delay: index * 0.07 }}
      style={{
        gridColumn: wide ? 'span 2' : 'span 1',
        background: 'rgba(255,255,255,0.05)',
        border: '1px solid rgba(255,255,255,0.10)',
        borderRadius: 20,
        padding: 6,
      }}
    >
      <div style={{
        background: 'rgba(255,255,255,0.03)',
        boxShadow: 'inset 0 1px 1px rgba(255,255,255,0.08)',
        borderRadius: 16,
        padding: '20px',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        gap: 14,
      }}>
        {/* Top row */}
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <h3 style={{
              fontSize: wide ? 20 : 16, fontWeight: 700,
              whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
              marginBottom: 8,
            }}>
              {app.name}
            </h3>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              {/* Table count badge */}
              <span style={{
                display: 'inline-flex', alignItems: 'center', gap: 4,
                fontSize: 11, fontWeight: 600,
                background: 'rgba(3,71,165,0.15)',
                border: '1px solid rgba(3,71,165,0.3)',
                color: 'var(--accent-light)',
                borderRadius: 20, padding: '3px 9px',
              }}>
                <Table2 size={11} strokeWidth={1.5} />
                {app.tables?.length ?? 0} {app.tables?.length === 1 ? 'tabela' : 'tabelas'}
              </span>
              {/* Auth badge */}
              <span style={{
                display: 'inline-flex', alignItems: 'center', gap: 4,
                fontSize: 11, fontWeight: 600,
                background: app.auth_email_enabled ? 'rgba(124,58,237,0.12)' : 'rgba(255,255,255,0.05)',
                border: `1px solid ${app.auth_email_enabled ? 'rgba(124,58,237,0.3)' : 'rgba(255,255,255,0.10)'}`,
                color: app.auth_email_enabled ? '#A78BFA' : 'var(--text-muted)',
                borderRadius: 20, padding: '3px 9px',
              }}>
                {app.auth_email_enabled
                  ? <><Mail size={11} strokeWidth={1.5} /> Email Auth</>
                  : <><MailX size={11} strokeWidth={1.5} /> Sem Email Auth</>
                }
              </span>
            </div>
          </div>
          {/* Actions */}
          <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
            <motion.button
              whileHover={{ scale: 1.05 }}
              whileTap={{ scale: 0.95 }}
              onClick={() => onEdit(app)}
              title="Editar app"
              style={{
                width: 32, height: 32, borderRadius: 8,
                border: '1px solid rgba(255,255,255,0.10)',
                background: 'rgba(255,255,255,0.05)',
                color: 'var(--text-muted)', cursor: 'pointer',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}
            >
              <Pencil size={14} strokeWidth={1.5} />
            </motion.button>
            <motion.button
              whileHover={{ scale: 1.05 }}
              whileTap={{ scale: 0.95 }}
              onClick={() => onDelete(app)}
              title="Deletar app"
              style={{
                width: 32, height: 32, borderRadius: 8,
                border: '1px solid rgba(239,68,68,0.15)',
                background: 'rgba(239,68,68,0.07)',
                color: '#EF4444', cursor: 'pointer',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}
            >
              <Trash2 size={14} strokeWidth={1.5} />
            </motion.button>
          </div>
        </div>

        {/* Created at */}
        <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 'auto' }}>
          Criado em {createdAt}
        </p>
      </div>
    </motion.div>
  )
}

// ── Empty state ────────────────────────────────────────────────────────────────

function EmptyState({ onCreateClick }: { onCreateClick: () => void }) {
  return (
    <motion.div
      {...fadeUp}
      style={{
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        minHeight: 360,
      }}
    >
      <div style={{
        background: 'rgba(255,255,255,0.05)',
        border: '1px solid rgba(255,255,255,0.10)',
        borderRadius: 24, padding: 6,
        maxWidth: 380, width: '100%',
        textAlign: 'center',
      }}>
        <div style={{
          background: 'rgba(255,255,255,0.03)',
          boxShadow: 'inset 0 1px 1px rgba(255,255,255,0.08)',
          borderRadius: 20, padding: '40px 32px',
        }}>
          <motion.div
            animate={{ y: [0, -6, 0] }}
            transition={{ duration: 2.5, repeat: Infinity, ease: 'easeInOut' }}
            style={{
              width: 64, height: 64, borderRadius: 18,
              background: 'rgba(3,71,165,0.12)',
              border: '1px solid rgba(3,71,165,0.2)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              margin: '0 auto 20px',
            }}
          >
            <LayoutGrid size={28} strokeWidth={1.5} style={{ color: 'var(--accent-light)' }} />
          </motion.div>
          <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 8 }}>Nenhum app criado</h3>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6, marginBottom: 24 }}>
            Crie seu primeiro app para gerar APIs automaticamente e gerenciar seus dados.
          </p>
          <motion.button
            whileHover={{ scale: 1.02 }}
            whileTap={{ scale: 0.98 }}
            onClick={onCreateClick}
            style={{
              display: 'inline-flex', alignItems: 'center', gap: 8,
              padding: '10px 22px', borderRadius: 24,
              border: 'none', background: 'var(--accent)',
              color: '#fff', fontSize: 14, fontWeight: 600,
              cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            Criar App
            <span style={{
              width: 22, height: 22, borderRadius: 11,
              background: 'rgba(255,255,255,0.15)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <Plus size={12} strokeWidth={2} />
            </span>
          </motion.button>
        </div>
      </div>
    </motion.div>
  )
}

// ── Error state ────────────────────────────────────────────────────────────────

function ErrorState({ message }: { message: string }) {
  return (
    <div style={{
      background: 'rgba(239,68,68,0.06)',
      border: '1px solid rgba(239,68,68,0.18)',
      borderRadius: 16, padding: '20px 24px',
      color: '#EF4444', fontSize: 14,
    }}>
      Erro ao carregar apps: {message}
    </div>
  )
}

// ── Main page ──────────────────────────────────────────────────────────────────

export default function AppsPage() {
  const { data: apps, isLoading, error } = useApps()
  const deleteApp = useDeleteApp()

  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AppDef | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AppDef | null>(null)

  function handleEdit(app: AppDef) {
    setEditTarget(app)
    setCreateOpen(true)
  }

  function handleCloseModal() {
    setCreateOpen(false)
    setEditTarget(null)
  }

  async function handleConfirmDelete() {
    if (!deleteTarget) return
    try {
      await deleteApp.mutateAsync(deleteTarget.id)
      setDeleteTarget(null)
    } catch {
      // error surfaces via deleteApp.error if needed
    }
  }

  return (
    <>
      {/* Mesh orb backdrop */}
      <div style={{
        position: 'fixed', top: 0, left: 240, right: 0, bottom: 0,
        pointerEvents: 'none', zIndex: 0,
        background: `
          radial-gradient(ellipse 60% 50% at 70% 20%, rgba(3,71,165,0.08) 0%, transparent 70%),
          radial-gradient(ellipse 40% 40% at 30% 70%, rgba(124,58,237,0.06) 0%, transparent 70%)
        `,
      }} />

      <div style={{ position: 'relative', zIndex: 1 }}>
        {/* Header */}
        <motion.div {...fadeUp} style={{ marginBottom: 36 }}>
          <span style={{
            display: 'inline-block',
            fontSize: 10, fontWeight: 700, letterSpacing: '0.12em',
            color: 'var(--accent-light)',
            background: 'rgba(3,71,165,0.12)',
            border: '1px solid rgba(3,71,165,0.2)',
            borderRadius: 20, padding: '4px 12px',
            textTransform: 'uppercase',
            marginBottom: 12,
          }}>
            Apps
          </span>
          <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', flexWrap: 'wrap', gap: 16 }}>
            <div>
              <h2 style={{ fontSize: 28, fontWeight: 800, lineHeight: 1.2, marginBottom: 6 }}>
                Seus Aplicativos
              </h2>
              <p style={{ fontSize: 14, color: 'var(--text-muted)' }}>
                Gerencie seus apps e APIs geradas automaticamente
              </p>
            </div>
            <motion.button
              whileHover={{ scale: 1.02 }}
              whileTap={{ scale: 0.98 }}
              onClick={() => { setEditTarget(null); setCreateOpen(true) }}
              style={{
                display: 'flex', alignItems: 'center', gap: 8,
                padding: '10px 20px', borderRadius: 24,
                border: 'none', background: 'var(--accent)',
                color: '#fff', fontSize: 14, fontWeight: 600,
                cursor: 'pointer', fontFamily: 'inherit',
                flexShrink: 0,
              }}
            >
              Criar App
              <span style={{
                width: 24, height: 24, borderRadius: 12,
                background: 'rgba(255,255,255,0.12)',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}>
                <Plus size={12} strokeWidth={2} />
              </span>
            </motion.button>
          </div>
        </motion.div>

        {/* Content */}
        <AnimatePresence mode="wait">
          {isLoading && (
            <motion.div
              key="loading"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(3, 1fr)',
                gap: 16,
              }}
            >
              <style>{`@keyframes shimmer { 0%{transform:translateX(-100%)} 100%{transform:translateX(100%)} }`}</style>
              <SkeletonCard wide />
              <SkeletonCard />
              <SkeletonCard />
              <SkeletonCard />
            </motion.div>
          )}

          {!isLoading && error && (
            <motion.div key="error" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
              <ErrorState message={(error as Error).message} />
            </motion.div>
          )}

          {!isLoading && !error && apps && apps.length === 0 && (
            <motion.div key="empty" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
              <EmptyState onCreateClick={() => { setEditTarget(null); setCreateOpen(true) }} />
            </motion.div>
          )}

          {!isLoading && !error && apps && apps.length > 0 && (
            <motion.div
              key="grid"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(3, 1fr)',
                gap: 16,
              }}
            >
              {apps.map((app, i) => (
                <AppCard
                  key={app.id}
                  app={app}
                  wide={i === 0}
                  index={i}
                  onEdit={handleEdit}
                  onDelete={setDeleteTarget}
                />
              ))}
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* Modals */}
      <CreateAppModal
        open={createOpen}
        editTarget={editTarget}
        onClose={handleCloseModal}
      />
      <DeleteConfirmDialog
        open={Boolean(deleteTarget)}
        appName={deleteTarget?.name ?? ''}
        loading={deleteApp.isPending}
        onConfirm={handleConfirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  )
}
