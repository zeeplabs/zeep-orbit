import { lazy, Suspense } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Navigate, Routes, Route, useParams, useLocation } from 'react-router-dom'
import { safeReturnTo } from '@/lib/returnTo'
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
import MCPPage from './pages/MCPPage'
import ChangelogPage from './pages/ChangelogPage'
import DataBrowserPage from './pages/DataBrowserPage'
import AccessDenied from './pages/AccessDenied'
import OAuthConsent from './components/OAuthConsent'
import { RequireRole } from './components/patterns/RequireRole'
import { Toaster } from 'sonner'
import { useBootstrapStatus } from './lib/api'

const ComponentsSandbox = import.meta.env.DEV
  ? lazy(() => import('./pages/dev/ComponentsSandbox'))
  : null

function LoadingScreen() {
  const { t } = useTranslation()
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100vh',
        background: 'var(--bg-page)',
      }}
    >
      <div
        style={{
          color: 'var(--text-tertiary)',
          fontFamily: 'var(--font-ui)',
        }}
      >
        {t("app.loading")}
      </div>
    </div>
  )
}

function RedirectToAppDetails({ tab }: { tab?: string }) {
  const { id } = useParams()
  return <Navigate to={tab ? `/apps/${id}?tab=${tab}` : `/apps/${id}`} replace />
}

function App() {
  const qc = useQueryClient()
  const location = useLocation()
  const returnTo = encodeURIComponent(location.pathname + location.search)

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
        {ComponentsSandbox && (
          <Route
            path="/dev/components"
            element={
              <Suspense fallback={null}>
                <ComponentsSandbox />
              </Suspense>
            }
          />
        )}
        <Route path="/login" element={
          user ? <Navigate to={safeReturnTo(location.search) ?? '/apps'} replace /> : <LoginPage />
        } />
        <Route path="/google-setup" element={
          !user ? <Navigate to="/login" replace /> :
          user.needs_setup ? <GoogleSetupPage /> :
          <Navigate to="/apps" replace />
        } />
        <Route path="/oauth/consent" element={
          !user ? <Navigate to={`/login?return_to=${returnTo}`} replace /> : <OAuthConsent />
        } />
        <Route
          element={<DashboardShell user={user} />}
        >
          <Route index element={<Navigate to="/apps" replace />} />
          <Route path="/apps" element={<AppsPage />} />
          <Route path="/apps/new" element={<AppOnboardingPage />} />
          <Route path="/apps/:id" element={<AppDetailsPage />} />
          <Route path="/apps/:id/edit" element={<RedirectToAppDetails />} />
          <Route path="/apps/:id/users" element={<RedirectToAppDetails tab="users" />} />
          <Route
            path="/configuracoes"
            element={<RequireRole allow={['superadmin', 'admin']}><BrandSettingsPage /></RequireRole>}
          />
          <Route path="/data-browser" element={<DataBrowserPage />} />
          <Route
            path="/usuarios"
            element={<RequireRole allow={['superadmin', 'admin']}><UsersPage /></RequireRole>}
          />
          <Route path="/logs" element={<LogsPage />} />
          <Route
            path="/auditoria"
            element={<RequireRole allow={['superadmin', 'auditor']}><AuditLogPage /></RequireRole>}
          />
          <Route
            path="/integracoes/github"
            element={<RequireRole allow={['superadmin']}><GitHubIntegrationPage /></RequireRole>}
          />
          <Route path="/sdks" element={<SdkPage />} />
          <Route path="/mcp-settings" element={<MCPPage />} />
          <Route path="/changelog" element={<ChangelogPage />} />
          <Route path="/403" element={<AccessDenied />} />
        </Route>
        <Route path="*" element={<Navigate to="/apps" replace />} />
      </Routes>
      <Toaster
        position="bottom-right"
        toastOptions={{
          style: {
            background: 'var(--surface-raised)',
            border: '1px solid var(--border)',
            color: 'var(--text-primary)',
          },
        }}
      />
    </>
  )
}

export default App
