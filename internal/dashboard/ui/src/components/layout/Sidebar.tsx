import { ReactNode } from 'react'
import { NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { useCurrentRole } from '@/lib/useCurrentRole'
import { hasPlatformPermission } from '@/lib/permissions'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { Separator } from '@/components/ui/separator'
import { NAV_SECTIONS, NavItemDef } from './nav'
import orbitWordmark from '@/assets/images/logo/orbit-transparent.png'

function label(key: string, t: (k: string) => string): string {
  return key.includes('.') ? t(key) : key
}

export function NavRow({
  item,
  onNavigate,
  alwaysShowLabel = false,
}: {
  item: NavItemDef
  onNavigate?: () => void
  /**
   * MobileNav's "More" sheet is a full-width list (not an icon rail) and
   * must always show labels regardless of viewport width — the `lg:inline`
   * default here is for the Sidebar's own icon-only tablet rail only.
   */
  alwaysShowLabel?: boolean
}) {
  const { t } = useTranslation()
  const itemLabel = label(item.labelKey, t)
  return (
    // Self-contained provider: NavRow is reused by MobileNav.tsx's "More"
    // sheet too, which has no TooltipProvider ancestor of its own — Radix's
    // Tooltip.Trigger requires one to be mounted, or the trigger silently
    // fails to render as an anchor.
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
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
                <span className={alwaysShowLabel ? 'inline' : 'hidden lg:inline'}>{itemLabel}</span>
              </>
            )}
          </NavLink>
        </TooltipTrigger>
        <TooltipContent side="right">{itemLabel}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

interface SidebarProps {
  companyName: string
  banner?: ReactNode
  footer: ReactNode
}

/** Sidebar desktop — logo, nav role-aware (omite via RoleGate), banner, footer. */
export function Sidebar({ companyName, banner, footer }: SidebarProps) {
  const { t } = useTranslation()
  const role = useCurrentRole()

  // SPEC_DEVIATION: design.md didn't cover the logo/company-name block's
  // fate at 72px tablet width. A horizontal wordmark + company name can't
  // fit an icon-only rail, so both are hidden below `lg` (desktop) rather
  // than introducing a new icon-only logo asset not requested in scope.
  return (
    <aside className="sticky top-0 flex h-screen flex-col border-r border-[var(--border)] bg-[var(--surface)] px-3.5 py-5 hidden md:flex md:w-[72px] lg:w-[264px]">
      <div className="mb-4 hidden items-center gap-2.5 px-2 lg:flex">
        <div className="flex items-start min-w-0 flex-col">
          <img src={orbitWordmark} alt="Orbit" className="mb-0.5 h-[20px] w-auto object-contain" />
          <span className="text-[11px] text-[var(--text-tertiary)]">{t('app.subtitle')}</span>
        </div>
      </div>

      <div className="mb-4 hidden truncate px-2 text-[13px] font-semibold text-[var(--text-secondary)] lg:block">
        {companyName}
      </div>
      <div className="mb-4 border-t border-[var(--border)]" />

      <nav className="flex flex-1 flex-col gap-4">
        {NAV_SECTIONS.map((section, index) => {
          const visible = section.items.filter(
            (item) => !item.platformAction || hasPlatformPermission(role, item.platformAction),
          )
          if (visible.length === 0) return null
          return (
            <div key={section.titleKey} className="flex flex-col gap-0.5">
              {index > 0 && (
                <Separator decorative={false} className="mb-1.5 md:block lg:hidden" />
              )}
              <span className="mb-1 hidden px-3 text-[11px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)] lg:block">
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
