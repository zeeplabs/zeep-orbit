import { motion } from 'framer-motion'
import { LogOut, Grid, Database, Users, Activity } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'

interface User { id: string; email: string; role: string }

export default function DashboardShell({ user }: { user: User }) {
  const qc = useQueryClient()

  const handleLogout = async () => {
    await fetch('/dashboard/api/logout', { method: 'POST', credentials: 'include' })
    qc.invalidateQueries({ queryKey: ['me'] })
  }

  return (
    <div style={{ display: 'flex', minHeight: '100vh' }}>
      <motion.aside
        initial={{ x: -20, opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        style={{
          width: 240,
          borderRight: '1px solid rgba(255,255,255,0.06)',
          padding: '24px 16px',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{ marginBottom: 32, padding: '0 8px' }}>
          <h1 style={{ fontSize: 18, fontWeight: 700 }}>Zeep</h1>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Dashboard</p>
        </div>
        <nav style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 4 }}>
          {[
            { icon: Grid, label: 'Apps' },
            { icon: Database, label: 'Data Browser' },
            { icon: Users, label: 'Usuários' },
            { icon: Activity, label: 'Logs' },
          ].map(({ icon: Icon, label }) => (
            <button key={label} style={{
              display: 'flex', alignItems: 'center', gap: 10,
              padding: '10px 12px', borderRadius: 8, border: 'none',
              background: label === 'Apps' ? 'rgba(255,255,255,0.08)' : 'transparent',
              color: label === 'Apps' ? 'var(--text)' : 'var(--text-muted)',
              cursor: 'pointer', fontSize: 14, textAlign: 'left', width: '100%',
            }}>
              <Icon size={16} />{label}
            </button>
          ))}
        </nav>
        <div style={{ borderTop: '1px solid rgba(255,255,255,0.06)', paddingTop: 16 }}>
          <div style={{ padding: '0 8px', marginBottom: 12 }}>
            <p style={{ fontSize: 13, fontWeight: 500 }}>{user.email}</p>
            <p style={{ fontSize: 11, color: 'var(--text-muted)' }}>{user.role}</p>
          </div>
          <button onClick={handleLogout} style={{
            display: 'flex', alignItems: 'center', gap: 8,
            padding: '8px 12px', borderRadius: 8, border: 'none',
            background: 'transparent', color: 'var(--text-muted)',
            cursor: 'pointer', fontSize: 13, width: '100%',
          }}>
            <LogOut size={14} />Sair
          </button>
        </div>
      </motion.aside>
      <main style={{ flex: 1, padding: 40 }}>
        <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          <h2 style={{ fontSize: 24, fontWeight: 700, marginBottom: 8 }}>Apps</h2>
          <p style={{ color: 'var(--text-muted)', marginBottom: 32 }}>Gerencie seus aplicativos e APIs</p>
          <div className="glass" style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
            Nenhum app criado. Em breve você poderá criar apps aqui.
          </div>
        </motion.div>
      </main>
    </div>
  )
}
