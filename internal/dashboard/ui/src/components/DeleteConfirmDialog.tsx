import { motion, AnimatePresence } from 'framer-motion'
import { AlertTriangle } from 'lucide-react'

interface Props {
  open: boolean
  appName: string
  loading: boolean
  onConfirm: () => void
  onCancel: () => void
}

export default function DeleteConfirmDialog({ open, appName, loading, onConfirm, onCancel }: Props) {
  return (
    <AnimatePresence>
      {open && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.2 }}
          onClick={onCancel}
          style={{
            position: 'fixed', inset: 0,
            background: 'rgba(0,0,0,0.65)',
            backdropFilter: 'blur(4px)',
            zIndex: 60,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            padding: 24,
          }}
        >
          <motion.div
            initial={{ opacity: 0, scale: 0.92, y: 8 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.92, y: 8 }}
            transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
            onClick={e => e.stopPropagation()}
            style={{
              background: 'rgba(255,255,255,0.05)',
              border: '1px solid rgba(255,255,255,0.10)',
              borderRadius: 20,
              padding: 6,
              width: '100%',
              maxWidth: 420,
            }}
          >
            {/* inner bezel */}
            <div style={{
              background: 'rgba(255,255,255,0.03)',
              boxShadow: 'inset 0 1px 1px rgba(255,255,255,0.08)',
              borderRadius: 16,
              padding: '28px 28px 24px',
            }}>
              {/* icon */}
              <div style={{
                width: 44, height: 44, borderRadius: 12,
                background: 'rgba(239,68,68,0.12)',
                border: '1px solid rgba(239,68,68,0.2)',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                marginBottom: 18,
              }}>
                <AlertTriangle size={20} strokeWidth={1.5} style={{ color: '#EF4444' }} />
              </div>

              <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 8 }}>
                Deletar app "{appName}"?
              </h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6, marginBottom: 24 }}>
                Esta ação remove o app do dashboard. As tabelas no banco{' '}
                <strong style={{ color: 'var(--text)' }}>NÃO serão deletadas</strong>.
              </p>

              <div style={{ display: 'flex', gap: 10 }}>
                <button
                  onClick={onCancel}
                  disabled={loading}
                  style={{
                    flex: 1, padding: '10px 0', borderRadius: 12,
                    border: '1px solid rgba(255,255,255,0.10)',
                    background: 'rgba(255,255,255,0.05)',
                    color: 'var(--text-muted)', fontSize: 14, fontWeight: 500,
                    cursor: 'pointer', fontFamily: 'inherit',
                    transition: 'background 0.15s',
                  }}
                >
                  Cancelar
                </button>
                <motion.button
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  onClick={onConfirm}
                  disabled={loading}
                  style={{
                    flex: 1, padding: '10px 0', borderRadius: 12,
                    border: 'none',
                    background: loading ? 'rgba(239,68,68,0.4)' : '#EF4444',
                    color: '#fff', fontSize: 14, fontWeight: 600,
                    cursor: loading ? 'not-allowed' : 'pointer',
                    fontFamily: 'inherit',
                  }}
                >
                  {loading ? 'Deletando...' : 'Deletar'}
                </motion.button>
              </div>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
