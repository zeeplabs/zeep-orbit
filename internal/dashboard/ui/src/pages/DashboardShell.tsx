import { useState } from 'react'
import { motion } from 'framer-motion'
import { LogOut, Grid, Database, Users, Activity } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import AppsPage from './AppsPage'

interface User { id: string; email: string; role: string }

type NavItem = 'Apps' | 'Data Browser' | 'Usuários' | 'Logs'

const NAV_ITEMS: { icon: typeof Grid; label: NavItem }[] = [
  { icon: Grid, label: 'Apps' },
  { icon: Database, label: 'Data Browser' },
  { icon: Users, label: 'Usuários' },
  { icon: Activity, label: 'Logs' },
]

function Placeholder({ label }: { label: string }) {
  return (
    <div style={{
      background: 'rgba(255,255,255,0.04)',
      border: '1px solid rgba(255,255,255,0.08)',
      borderRadius: 16, padding: 40,
      textAlign: 'center', color: 'var(--text-muted)', fontSize: 14,
    }}>
      {label} — em breve
    </div>
  )
}

export default function DashboardShell({ user }: { user: User }) {
  const qc = useQueryClient()
  const [active, setActive] = useState<NavItem>('Apps')

  const handleLogout = async () => {
    await fetch('/dashboard/api/logout', { method: 'POST', credentials: 'include' })
    qc.invalidateQueries({ queryKey: ['me'] })
  }

  return (
    <div style={{ display: 'flex', minHeight: '100vh' }}>
      {/* Sidebar */}
      <motion.aside
        initial={{ x: -20, opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
        style={{
          width: 240,
          borderRight: '1px solid rgba(255,255,255,0.06)',
          padding: '24px 12px',
          display: 'flex',
          flexDirection: 'column',
          flexShrink: 0,
          position: 'sticky',
          top: 0,
          height: '100vh',
        }}
      >
        {/* Logo */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '0 8px', marginBottom: 32 }}>
          <div style={{
            width: 32, height: 32, borderRadius: 10,
            background: 'rgba(3,71,165,0.15)',
            border: '1px solid rgba(3,71,165,0.25)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 15, fontWeight: 800, color: 'var(--accent-light)',
          }}>
            Z
          </div>
          <div>
            <p style={{ fontSize: 14, fontWeight: 700, lineHeight: 1 }}>zeep</p>
            <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>dashboard</p>
          </div>
        </div>

        {/* Nav */}
        <nav style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {NAV_ITEMS.map(({ icon: Icon, label }) => {
            const isActive = active === label
            return (
              <button
                key={label}
                onClick={() => setActive(label)}
                style={{
                  display: 'flex', alignItems: 'center', gap: 10,
                  padding: '9px 12px', borderRadius: 10, border: 'none',
                  background: isActive ? 'rgba(3,71,165,0.12)' : 'transparent',
                  color: isActive ? 'var(--text)' : 'var(--text-muted)',
                  cursor: 'pointer', fontSize: 14, textAlign: 'left', width: '100%',
                  fontFamily: 'inherit', fontWeight: isActive ? 600 : 400,
                  position: 'relative',
                  transition: 'background 0.15s, color 0.15s',
                }}
              >
                {isActive && (
                  <motion.div
                    layoutId="nav-active-indicator"
                    style={{
                      position: 'absolute', left: 0, top: '20%', bottom: '20%',
                      width: 3, borderRadius: 2,
                      background: 'var(--accent)',
                    }}
                    transition={{ duration: 0.3, ease: [0.32, 0.72, 0, 1] }}
                  />
                )}
                <Icon size={15} strokeWidth={1.5} />
                {label}
              </button>
            )
          })}
        </nav>

        {/* User */}
        <div style={{ borderTop: '1px solid rgba(255,255,255,0.06)', paddingTop: 14 }}>
          <div style={{ padding: '0 8px', marginBottom: 10 }}>
            <p style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {user.email}
            </p>
            <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{user.role}</p>
          </div>
          <button
            onClick={handleLogout}
            style={{
              display: 'flex', alignItems: 'center', gap: 8,
              padding: '8px 12px', borderRadius: 10, border: 'none',
              background: 'transparent', color: 'var(--text-muted)',
              cursor: 'pointer', fontSize: 13, width: '100%',
              fontFamily: 'inherit',
              transition: 'color 0.15s',
            }}
          >
            <LogOut size={14} strokeWidth={1.5} /> Sair
          </button>
        </div>
      </motion.aside>

      {/* Main content */}
      <main style={{ flex: 1, padding: 40, overflowY: 'auto', minHeight: '100vh' }}>
        <motion.div
          key={active}
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.35, ease: [0.32, 0.72, 0, 1] }}
        >
          {active === 'Apps' && <AppsPage />}
          {active === 'Data Browser' && <Placeholder label="Data Browser" />}
          {active === 'Usuários' && <Placeholder label="Usuários" />}
          {active === 'Logs' && <Placeholder label="Logs" />}
        </motion.div>
      </main>
    </div>
  )
}
