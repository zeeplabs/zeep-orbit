import { useRef, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Icon } from '@/components/ui/icon'
import { Dialog, DialogTrigger, DialogPortal, DialogOverlay, DialogTitle } from '@/components/ui/dialog'
import { useBuilderSessions } from '@/lib/api'
import { recentSessions } from './SimpleSidebar'
import { SidebarFooter } from './SidebarFooter'

interface SimpleMobileNavProps {
  user: { email: string; name?: string; role: string }
  currentLang: string
  onChangePassword: () => void
  onSaveLanguage: (lang: string) => void
  onLogout: () => void
  version?: string
}

const TAB = 'flex flex-1 flex-col items-center justify-center gap-0.5 no-underline'

/**
 * Phone navigation of the simple shell (HCH-19), below `md` where the sidebar is
 * hidden: a bottom bar with Chat / My projects / More, and a sheet with "New
 * project", the recent projects and the user footer. Same pattern as the
 * advanced MobileNav (modal Radix dialog: focus trapped and returned, Esc closes).
 */
export function SimpleMobileNav({
  user,
  currentLang,
  onChangePassword,
  onSaveLanguage,
  onLogout,
  version,
}: SimpleMobileNavProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  // Password and logout open their own dialog: run them after the sheet has
  // closed so the focus returned to "More" does not fight the new dialog.
  const pendingAction = useRef<(() => void) | null>(null)
  const recents = recentSessions(useBuilderSessions().data ?? [])

  const closeThen = (action: () => void) => {
    pendingAction.current = action
    setOpen(false)
  }

  const tab = (icon: string, label: string, to: string, end = false) => (
    <NavLink
      to={to}
      end={end}
      className={TAB}
      style={({ isActive }) => ({
        color: isActive ? 'var(--primary)' : 'var(--text-tertiary)',
        fontWeight: isActive ? 600 : 400,
      })}
    >
      {({ isActive }) => (
        <>
          <Icon name={icon} size={22} fill={isActive ? 1 : 0} />
          <span className="text-[12px] leading-none">{label}</span>
        </>
      )}
    </NavLink>
  )

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <nav
        aria-label={t('nav.simple.mobileNav')}
        className="fixed inset-x-0 bottom-0 z-40 flex items-center border-t border-[var(--border)] bg-[var(--surface)] md:hidden"
        style={{ height: 60, paddingBottom: 'env(safe-area-inset-bottom, 0px)' }}
      >
        {tab('chat', t('nav.simple.chat'), '/', true)}
        {tab('folder_open', t('nav.simple.projects'), '/projetos')}
        <DialogTrigger asChild>
          <button
            type="button"
            className="flex flex-1 flex-col items-center justify-center gap-0.5"
            style={{ color: 'var(--text-tertiary)' }}
          >
            <Icon name="menu" size={22} />
            <span className="text-[12px] leading-none">{t('nav.more')}</span>
          </button>
        </DialogTrigger>
      </nav>

      <DialogPortal>
        <DialogOverlay className="md:hidden" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          onCloseAutoFocus={(e) => {
            const action = pendingAction.current
            if (!action) return
            pendingAction.current = null
            e.preventDefault()
            action()
          }}
          className="fixed inset-x-0 bottom-0 z-50 max-h-[80vh] overflow-y-auto rounded-t-2xl border-t border-[var(--border)] bg-[var(--surface)] p-4 focus:outline-none md:hidden"
          style={{ paddingBottom: 'calc(16px + env(safe-area-inset-bottom, 0px))' }}
        >
          <DialogTitle className="sr-only">{t('nav.more')}</DialogTitle>
          <div aria-hidden="true" className="mx-auto mb-4 h-1 w-10 rounded-full bg-[var(--border-strong)]" />
          <DialogPrimitive.Close className="sr-only focus-visible:not-sr-only focus-visible:mb-3 focus-visible:block focus-visible:rounded-[8px] focus-visible:border focus-visible:border-[var(--border-strong)] focus-visible:px-3 focus-visible:py-2 focus-visible:text-sm focus-visible:ring-2 focus-visible:ring-ring">
            {t('nav.closeMenu')}
          </DialogPrimitive.Close>

          <Link
            to="/"
            onClick={() => setOpen(false)}
            className="mb-4 flex items-center justify-center gap-2 rounded-[8px] bg-[var(--primary)] px-3 py-2 text-sm font-semibold text-white no-underline"
          >
            <Icon name="add" size={18} />
            {t('nav.simple.newProject')}
          </Link>

          {recents.length > 0 && (
            <nav aria-label={t('nav.simple.recents')} className="flex flex-col gap-0.5">
              <span className="mb-1 px-3 text-[12px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
                {t('nav.simple.recents')}
              </span>
              {recents.map((s) => (
                <NavLink
                  key={s.id}
                  to={`/builder/${s.id}`}
                  onClick={() => setOpen(false)}
                  className="flex items-center gap-2.5 rounded-[8px] px-3 py-2 text-sm text-[var(--text-secondary)] no-underline aria-[current=page]:bg-[var(--primary-tint)] aria-[current=page]:font-semibold aria-[current=page]:text-[var(--text-primary)]"
                >
                  <Icon name="chat_bubble" size={16} className="shrink-0" />
                  <span className="truncate">{s.title || t('nav.simple.untitled')}</span>
                </NavLink>
              ))}
            </nav>
          )}

          <SidebarFooter
            className="mt-4"
            user={user}
            currentLang={currentLang}
            onChangePassword={() => closeThen(onChangePassword)}
            onSaveLanguage={onSaveLanguage}
            onLogout={() => closeThen(onLogout)}
            version={version}
            onNavigate={() => setOpen(false)}
          />
        </DialogPrimitive.Content>
      </DialogPortal>
    </Dialog>
  )
}
