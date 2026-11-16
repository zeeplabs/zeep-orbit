import { lazy, Suspense, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Navigate, Routes, Route, useParams, useLocation } from 'react-router-dom'
import { safeReturnTo } from '@/lib/returnTo'
import LoginPage from './pages/LoginPage'
import GoogleSetupPage from './pages/GoogleSetupPage'
import DashboardShell from './pages/DashboardShell'
import SimpleShell, { type ShellUser } from './components/layout/SimpleShell'
import HomeChatPage from './pages/HomeChatPage'
import ProjectsPage from './pages/ProjectsPage'
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
import BuilderPage from './pages/BuilderPage'
import BuilderUsagePage from './pages/BuilderUsagePage'
import AccessDenied from './pages/AccessDenied'
import OAuthConsent from './components/OAuthConsent'
import { RequireRole } from './components/patterns/RequireRole'
import { Toaster } from 'sonner'
import { useBootstrapStatus, useBuilderStatus } from './lib/api'
import { readMode, resolveLanding } from './lib/mode'

const ComponentsSandbox = import.meta.env.DEV
  ? lazy(() => import('./pages/dev/ComponentsSandbox'))
  : null

// ResumeReturnTo replaces a client-side <Navigate> for resuming `return_to`
// when the target might not be an SPA route at all — e.g. the OAuth
// authorization endpoint (/dashboard/oauth/authorize) is handled entirely
// by the Go backend and re-evaluates the request once a session cookie is
// present, redirecting on to /oauth/consent itself. <Navigate> only ever
// updates client-side router state, so it can never reach a backend-only
// path; a full document load (window.location.assign) always works for
// both an SPA route (the app just reloads, unaffected) and a backend one.
function ResumeReturnTo({ fallback }: { fallback: string }) {
  const location = useLocation()
  useEffect(() => {
    const returnTo = safeReturnTo(location.search)
    window.location.assign(returnTo ? `/dashboard${returnTo}` : fallback)
  }, [location.search, fallback])
  return <LoadingScreen />
}

// HomeEntry decides where `/` lands (HCH-10): the chat home, inside the simple
// shell, for the simple mode when the builder is available; `/apps` otherwise.
// The decision is made before any shell renders so nobody sees the simple chrome
// flash before a redirect.
function HomeEntry({ user }: { user: ShellUser | null }) {
  const builder = useBuilderStatus()
  const landing = resolveLanding(readMode(), {
    isPending: builder.isPending,
    isError: builder.isError,
    available: builder.data?.available,
  })
  if (!user) return <Navigate to="/login" replace />
  if (landing === 'loading') return <LoadingScreen />
  if (landing === 'apps') return <Navigate to="/apps" replace />
  return (
    <SimpleShell user={user}>
      <HomeChatPage />
    </SimpleShell>
  )
}

// SimpleLayout wraps the simple screens that are reached by their own URL
// (`/projetos`). They need the builder: when it is not available the user goes
// to `/apps` (HCH-20), where a superadmin sees why.
function SimpleLayout({ user }: { user: ShellUser | null }) {
  const builder = useBuilderStatus()
  if (!user) return <Navigate to="/login" replace />
  if (builder.isPending) return <LoadingScreen />
  if (builder.isError || !builder.data?.available) return <Navigate to="/apps" replace />
  return <SimpleShell user={user} />
}

// BuilderLayout hosts `/builder/:sessionId` in the simple shell with the
// collapsed sidebar (HCH-13). Unlike SimpleLayout it does not require the
// builder to be available: a session opened by link keeps rendering its own
// "unavailable" notice instead of bouncing the user away.
function BuilderLayout({ user }: { user: ShellUser | null }) {
  return <SimpleShell user={user} collapsed />
}

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
          user ? <ResumeReturnTo fallback="/dashboard/" /> : <LoginPage />
        } />
        <Route path="/google-setup" element={
          !user ? <Navigate to="/login" replace /> :
          user.needs_setup ? <GoogleSetupPage /> :
          <Navigate to="/apps" replace />
        } />
        <Route path="/oauth/consent" element={
          !user ? <Navigate to={`/login?return_to=${returnTo}`} replace /> : <OAuthConsent />
        } />
        <Route path="/" element={<HomeEntry user={user} />} />
        <Route element={<SimpleLayout user={user} />}>
          <Route path="/projetos" element={<ProjectsPage />} />
        </Route>
        <Route element={<BuilderLayout user={user} />}>
          <Route path="/builder/:sessionId" element={<BuilderPage />} />
        </Route>
        <Route
          element={<DashboardShell user={user} />}
        >
          <Route path="/apps" element={<AppsPage />} />
          <Route path="/apps/new" element={<AppOnboardingPage />} />
          <Route path="/apps/:id" element={<AppDetailsPage />} />
          <Route path="/builder/usage" element={<BuilderUsagePage />} />
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
