import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, Routes, Route } from 'react-router-dom'
import LoginPage from './pages/LoginPage'
import DashboardShell from './pages/DashboardShell'
import OnboardingPage from './pages/OnboardingPage'
import AppsPage from './pages/AppsPage'
import AppFormPage from './pages/AppFormPage'
import BrandSettingsPage from './pages/BrandSettingsPage'
import UsersPage from './pages/UsersPage'
import { useBootstrapStatus } from './lib/api'
import { THEMES, applyTheme } from './lib/themes'

function LoadingScreen() {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100vh',
        background: 'var(--bg)',
      }}
    >
      <div
        style={{
          color: 'rgba(255,255,255,0.4)',
          fontFamily: 'Plus Jakarta Sans, sans-serif',
        }}
      >
        Carregando...
      </div>
    </div>
  )
}

function useTheme() {
  useEffect(() => {
    fetch('/dashboard/api/config', { cache: 'no-cache' })
      .then((res) => res.json())
      .then((config) => {
        const theme = THEMES[config.theme] || THEMES.azure
        applyTheme(theme)
      })
      .catch(() => {})
  }, [])
}

function Placeholder({ label }: { label: string }) {
  return (
    <div
      style={{
        background: 'rgba(255,255,255,0.04)',
        border: '1px solid rgba(255,255,255,0.08)',
        borderRadius: 16,
        padding: 40,
        textAlign: 'center',
        color: 'var(--text-muted)',
        fontSize: 14,
      }}
    >
      {label} — em breve
    </div>
  )
}

function App() {
  const qc = useQueryClient()

  useTheme()

  const { data: status, isLoading: statusLoading } = useBootstrapStatus()

  const { data: user, isLoading: userLoading } = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/me', { credentials: 'include' })
      if (!res.ok) return null
      return res.json()
    },
    retry: false,
    enabled: status?.bootstrapped === true,
  })

  if (statusLoading || (status?.bootstrapped && userLoading)) {
    return <LoadingScreen />
  }

  if (!status?.bootstrapped) {
    return (
      <OnboardingPage
        onComplete={() => qc.invalidateQueries({ queryKey: ['bootstrap-status'] })}
      />
    )
  }

  return (
    <Routes>
      <Route path="/login" element={user ? <Navigate to="/apps" replace /> : <LoginPage />} />
      <Route
        element={<DashboardShell user={user} />}
      >
        <Route index element={<Navigate to="/apps" replace />} />
        <Route path="/apps" element={<AppsPage />} />
        <Route path="/apps/new" element={<AppFormPage />} />
        <Route path="/apps/:id/edit" element={<AppFormPage />} />
        <Route path="/configuracoes" element={<BrandSettingsPage />} />
        <Route path="/data-browser" element={<Placeholder label="Data Browser" />} />
        <Route path="/usuarios" element={<UsersPage />} />
        <Route path="/logs" element={<Placeholder label="Logs" />} />
      </Route>
      <Route path="*" element={<Navigate to="/apps" replace />} />
    </Routes>
  )
}

export default App
