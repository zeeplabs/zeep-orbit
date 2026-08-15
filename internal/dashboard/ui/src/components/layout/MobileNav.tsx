import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { useCurrentRole } from '@/lib/useCurrentRole'
import { hasPlatformPermission } from '@/lib/permissions'
import { NAV_SECTIONS, MOBILE_TABS } from './nav'
import { NavRow } from './Sidebar'
import { SidebarFooter } from './SidebarFooter'

function label(key: string, t: (k: string) => string): string {
  return key.includes('.') ? t(key) : key
}

interface MobileNavProps {
  user: { email: string; name?: string; role: string }
  currentLang: string
  onChangePassword: () => void
  onSaveLanguage: (lang: string) => void
  onLogout: () => void
  version?: string
}

/**
 * Shell mobile: bottom tab bar (Apps/Data/Logs) + "More" abrindo bottom-sheet
 * com o resto da nav (role-aware) + footer (usuário/tema/idioma/logout).
 * Não é reflow da sidebar — é experiência de app.
 * Handoff §F1-07: link de Changelog vive dentro do SidebarFooter (não duplicado no sheet).
 */
export function MobileNav({
  user,
  currentLang,
  onChangePassword,
  onSaveLanguage,
  onLogout,
  version,
}: MobileNavProps) {
  const { t } = useTranslation()
  const role = useCurrentRole()
  const [sheetOpen, setSheetOpen] = useState(false)

  const tab = (icon: string, labelKey: string, path: string, onClick?: () => void) => (
    <NavLink
      key={path}
      to={path}
      end={path === '/apps'}
      onClick={onClick}
      className="flex flex-1 flex-col items-center justify-center gap-0.5 no-underline"
      style={({ isActive }) => ({
        color: isActive ? 'var(--primary)' : 'var(--text-tertiary)',
        fontWeight: isActive ? 600 : 400,
      })}
    >
      {({ isActive }) => (
        <>
          <Icon name={icon} size={22} fill={isActive ? 1 : 0} />
          <span className="text-[10px] leading-none">{label(labelKey, t)}</span>
        </>
      )}
    </NavLink>
  )

  return (
    <>
      <nav
        className="fixed inset-x-0 bottom-0 z-40 flex items-center border-t border-[var(--border)] bg-[var(--surface)] md:hidden"
        style={{ height: 60, paddingBottom: 'env(safe-area-inset-bottom, 0px)' }}
      >
        {MOBILE_TABS.map((it) => tab(it.icon, it.labelKey, it.path))}
        <button
          type="button"
          onClick={() => setSheetOpen(true)}
          className="flex flex-1 flex-col items-center justify-center gap-0.5"
          style={{ color: 'var(--text-tertiary)' }}
        >
          <Icon name="menu" size={22} />
          <span className="text-[10px] leading-none">{t('nav.more')}</span>
        </button>
      </nav>

      {sheetOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <div
            className="absolute inset-0"
            style={{ background: 'var(--overlay)' }}
            onClick={() => setSheetOpen(false)}
          />
          <div
            className="absolute inset-x-0 bottom-0 max-h-[80vh] overflow-y-auto rounded-t-2xl border-t border-[var(--border)] bg-[var(--surface)] p-4"
            style={{ paddingBottom: 'calc(16px + env(safe-area-inset-bottom, 0px))' }}
          >
            <div className="mx-auto mb-4 h-1 w-10 rounded-full bg-[var(--border-strong)]" />
            <nav className="flex flex-col gap-4">
              {NAV_SECTIONS.map((section) => {
                const visible = section.items.filter(
                  (it) => !it.platformAction || hasPlatformPermission(role, it.platformAction),
                )
                if (visible.length === 0) return null
                return (
                  <div key={section.titleKey} className="flex flex-col gap-0.5">
                    <span className="mb-1 px-3 text-[11px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
                      {t(section.titleKey)}
                    </span>
                    {visible.map((it) => (
                      <NavRow key={it.path} item={it} onNavigate={() => setSheetOpen(false)} />
                    ))}
                  </div>
                )
              })}
            </nav>
            <SidebarFooter
              className="mt-4"
              user={user}
              currentLang={currentLang}
              onChangePassword={() => { setSheetOpen(false); onChangePassword() }}
              onSaveLanguage={onSaveLanguage}
              onLogout={() => { setSheetOpen(false); onLogout() }}
              version={version}
              onNavigate={() => setSheetOpen(false)}
            />
          </div>
        </div>
      )}
    </>
  )
}
