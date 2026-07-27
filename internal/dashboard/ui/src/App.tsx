import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Navigate, Routes, Route, useParams } from 'react-router-dom'
import LoginPage from './pages/LoginPage'
import GoogleSetupPage from './pages/GoogleSetupPage'
import DashboardShell from './pages/DashboardShell'
import OnboardingPage from './pages/OnboardingPage'
import AppsPage from './pages/AppsPage'
import AppOnboardingPage from './pages/AppOnboardingPage'
import AppDetailsPage from './pages/AppDetailsPage'
import BrandSettingsPage from './pages/BrandSettingsPage'
import UsersPage from './pages/UsersPage'
import LogsPage from './pages/LogsPage'
import AuditLogPage from './pages/AuditLogPage'
import GitHubIntegrationPage from './pages/GitHubIntegrationPage'
import SdkPage from './pages/SdkPage'
import ChangelogPage from './pages/ChangelogPage'
import AppUsersPage from './pages/AppUsersPage'
import DataBrowserPage from './pages/DataBrowserPage'
import { Toaster } from 'sonner'
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

function RedirectToAppDetails() {
  const { id } = useParams()
  return <Navigate to={`/apps/${id}`} replace />
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
    <>
      <Routes>
        <Route path="/login" element={user ? <Navigate to="/apps" replace /> : <LoginPage />} />
        <Route path="/google-setup" element={
          !user ? <Navigate to="/login" replace /> :
          user.needs_setup ? <GoogleSetupPage /> :
          <Navigate to="/apps" replace />
        } />
        <Route
          element={<DashboardShell user={user} />}
        >
          <Route index element={<Navigate to="/apps" replace />} />
          <Route path="/apps" element={<AppsPage />} />
          <Route path="/apps/new" element={<AppOnboardingPage />} />
          <Route path="/apps/:id" element={<AppDetailsPage />} />
          <Route path="/apps/:id/edit" element={<RedirectToAppDetails />} />
          <Route path="/apps/:id/users" element={<AppUsersPage />} />
          <Route path="/configuracoes" element={<BrandSettingsPage />} />
          <Route path="/data-browser" element={<DataBrowserPage />} />
          <Route path="/usuarios" element={<UsersPage />} />
          <Route path="/logs" element={<LogsPage />} />
          <Route path="/auditoria" element={<AuditLogPage />} />
          <Route path="/integracoes/github" element={<GitHubIntegrationPage />} />
          <Route path="/sdks" element={<SdkPage />} />
          <Route path="/changelog" element={<ChangelogPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/apps" replace />} />
      </Routes>
      <Toaster
        position="bottom-right"
        theme="dark"
        toastOptions={{
          style: {
            background: '#1A1A24',
            border: '1px solid rgba(255,255,255,0.10)',
            color: '#F8FAFC',
          },
        }}
      />
    </>
  )
}

export default App
