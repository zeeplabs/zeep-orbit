import { ReactNode } from 'react'
import { NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { useCurrentRole } from '@/lib/useCurrentRole'
import { hasPlatformPermission } from '@/lib/permissions'
import { NAV_SECTIONS, NavItemDef } from './nav'
import logoType from '@/assets/images/logo/logotype.svg'

function label(key: string, t: (k: string) => string): string {
  return key.includes('.') ? t(key) : key
}

export function NavRow({ item, onNavigate }: { item: NavItemDef; onNavigate?: () => void }) {
  const { t } = useTranslation()
  return (
    <NavLink
      to={item.path}
      end={item.path === '/apps'}
      onClick={onNavigate}
      className="group relative flex items-center gap-2.5 rounded-[8px] px-3 py-2 text-sm no-underline transition-colors"
      style={({ isActive }) => ({
        background: isActive ? 'var(--primary-tint)' : 'transparent',
        color: isActive ? 'var(--text-primary)' : 'var(--text-secondary)',
        fontWeight: isActive ? 600 : 400,
      })}
    >
      {({ isActive }) => (
        <>
          {isActive && (
            <span
              className="absolute left-0 top-1/2 h-4 w-0.5 -translate-y-1/2 rounded-full"
              style={{ background: 'var(--primary)' }}
            />
          )}
          <Icon name={item.icon} size={18} fill={isActive ? 1 : 0} />
          {label(item.labelKey, t)}
        </>
      )}
    </NavLink>
  )
}

interface SidebarProps {
  companyName: string
  banner?: ReactNode
  footer: ReactNode
}

/** Sidebar desktop — logo, nav role-aware (omite via RoleGate), banner, footer.
 *  Handoff §F1-07: o link de Changelog vive no footer (SidebarFooter), não no nav. */
export function Sidebar({ companyName, banner, footer }: SidebarProps) {
  const { t } = useTranslation()
  const role = useCurrentRole()

  return (
    <aside
      className="sticky top-0 flex h-screen flex-col border-r border-[var(--border)] bg-[var(--surface)] px-3 py-6 max-md:hidden"
      style={{ width: 240 }}
    >
      {/* Logo */}
      <div className="mb-8 flex items-center gap-2.5 px-2">
        <img
          src={logoType}
          alt="Zeep Orbit"
          className="h-[38px] w-[38px] rounded-[10px] border border-[var(--border)] object-cover"
        />
        <div className="flex min-w-0 flex-col">
          <span
            className="truncate text-[15px] font-bold leading-tight text-[var(--text-primary)]"
            style={{ fontFamily: 'var(--font-display)' }}
          >
            {companyName}
          </span>
          <span className="text-[11px] text-[var(--text-tertiary)]">{t('app.subtitle')}</span>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex flex-1 flex-col gap-4">
        {NAV_SECTIONS.map((section) => {
          const visible = section.items.filter(
            (item) => !item.platformAction || hasPlatformPermission(role, item.platformAction),
          )
          if (visible.length === 0) return null
          return (
            <div key={section.titleKey} className="flex flex-col gap-0.5">
              <span className="mb-1 px-3 text-[11px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
                {t(section.titleKey)}
              </span>
              {visible.map((item) => (
                <NavRow key={item.path} item={item} />
              ))}
            </div>
          )
        })}
      </nav>

      {banner}

      {footer}
    </aside>
  )
}
