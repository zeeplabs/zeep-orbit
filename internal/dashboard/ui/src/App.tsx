import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Navigate, Routes, Route } from 'react-router-dom'
import LoginPage from './pages/LoginPage'
import DashboardShell from './pages/DashboardShell'
import OnboardingPage from './pages/OnboardingPage'
import AppsPage from './pages/AppsPage'
import AppFormPage from './pages/AppFormPage'
import BrandSettingsPage from './pages/BrandSettingsPage'
import UsersPage from './pages/UsersPage'
import LogsPage from './pages/LogsPage'
import AuditLogPage from './pages/AuditLogPage'
import SdkPage from './pages/SdkPage'
import DataBrowserPage from './pages/DataBrowserPage'
import AppUsersPage from './pages/AppUsersPage'
import { useBootstrapStatus } from './lib/api'
import { THEMES, applyTheme } from './lib/themes'

function LoadingScreen() {
  const { t } = useTranslation()
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
        {t("app.loading")}
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
        <Route path="/apps/:id/users" element={<AppUsersPage />} />
        <Route path="/configuracoes" element={<BrandSettingsPage />} />
        <Route path="/data-browser" element={<DataBrowserPage />} />
        <Route path="/usuarios" element={<UsersPage />} />
        <Route path="/logs" element={<LogsPage />} />
        <Route path="/auditoria" element={<AuditLogPage />} />
        <Route path="/sdks" element={<SdkPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/apps" replace />} />
    </Routes>
  )
}

export default App
