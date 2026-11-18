import { useRef, type ReactNode } from 'react'
import { Navigate, NavLink, Outlet } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { UnsavedChangesBlocker } from '@/components/patterns/UnsavedChangesBlocker'
import { SidebarFooter } from './SidebarFooter'
import { useShellSession } from './useShellSession'
import { SimpleSidebar } from './SimpleSidebar'
import { SimpleMobileNav } from './SimpleMobileNav'
import { ModeSwitch } from './ModeSwitch'
import pkg from '../../../package.json'

export interface ShellUser {
  id: string
  email: string
  name?: string
  role: string
  language?: string
}

const TAB_CLASS =
  'rounded-[8px] px-3.5 py-1.5 text-sm text-[var(--text-secondary)] no-underline transition-colors hover:bg-[var(--hover-surface)] aria-[current=page]:bg-[var(--primary-tint)] aria-[current=page]:font-semibold aria-[current=page]:text-[var(--text-primary)]'

/**
 * Shell of the simple interface: `/`, `/projetos` and `/builder/:sessionId`
 * (home-chat spec, assumption 2). The logout, language and change-password
 * flows are shared with DashboardShell through `useShellSession`.
 * `children` replaces the <Outlet/> for a route that is not a layout route.
 */
export default function SimpleShell({
  user,
  children,
  collapsed = false,
}: {
  user: ShellUser | null
  children?: ReactNode
  /** Icon-only sidebar (68 px), used by the builder route (HCH-13). */
  collapsed?: boolean
}) {
  const { t } = useTranslation()
  const mainRef = useRef<HTMLElement>(null)
  const session = useShellSession(user)

  if (!user) return <Navigate to="/login" replace />

  const footer = (
    <SidebarFooter
      user={user}
      currentLang={session.currentLang}
      onChangePassword={session.openChangePassword}
      onSaveLanguage={session.saveLanguage}
      onLogout={session.openLogout}
      version={pkg.version}
      collapsed={collapsed}
    />
  )

  return (
    <div className="flex min-h-screen" style={{ background: 'var(--bg-page)' }}>
      <a
        href="#main-content"
        onClick={(e) => {
          e.preventDefault()
          mainRef.current?.focus()
        }}
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[60] focus:rounded-[8px] focus:bg-[var(--surface-raised)] focus:px-4 focus:py-2 focus:text-sm focus:font-semibold focus:text-[var(--text-primary)] focus:shadow-[var(--shadow-md)] focus:outline-none focus:ring-2 focus:ring-ring"
      >
        {t('nav.skipToContent')}
      </a>

      <UnsavedChangesBlocker />

      <SimpleSidebar footer={footer} collapsed={collapsed} />

      <div className="flex min-h-screen min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center justify-between gap-4 max-md:justify-end border-b border-[var(--border)] px-6 max-md:px-4">
          {/* Route links, not ARIA tabs: each one navigates to a page, and the
              WAI-ARIA tabs pattern needs a tabpanel in the same view. */}
          <nav aria-label={t('nav.simple.mainNav')} className="flex items-center gap-1 max-md:hidden">
            <NavLink to="/" end className={TAB_CLASS}>
              {t('nav.simple.chat')}
            </NavLink>
            <NavLink to="/projetos" className={TAB_CLASS}>
              {t('nav.simple.projects')}
            </NavLink>
          </nav>
          <ModeSwitch advanced={false} />
        </header>

        <main id="main-content" ref={mainRef} tabIndex={-1} className="min-w-0 flex-1 focus:outline-none max-md:pb-[65px]">
          {children ?? <Outlet />}
        </main>
      </div>

      <SimpleMobileNav
        user={user}
        currentLang={session.currentLang}
        onChangePassword={session.openChangePassword}
        onSaveLanguage={session.saveLanguage}
        onLogout={session.openLogout}
        version={pkg.version}
      />

      {session.dialogs}
    </div>
  )
}
