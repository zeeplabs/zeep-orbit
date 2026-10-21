import { useEffect, useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Icon } from '@/components/ui/icon'
import { Dialog, DialogTrigger, DialogPortal, DialogOverlay, DialogTitle } from '@/components/ui/dialog'
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
 *
 * O sheet é um Radix Dialog (modal): foco preso e devolvido ao botão "More",
 * Esc fecha, scroll da página travado, resto da página `aria-hidden`.
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
  // Ação (trocar senha / logout) que abre outro diálogo: roda só depois que o sheet
  // terminou de fechar, senão o foco devolvido ao "More" disputa com o novo diálogo.
  const pendingAction = useRef<(() => void) | null>(null)

  // O sheet só existe no mobile (`md:hidden`). Se a viewport cruzar o breakpoint
  // com ele aberto (rotação de tablet, resize), fecha — senão o scroll lock do
  // diálogo ficaria preso com o conteúdo escondido.
  useEffect(() => {
    if (!sheetOpen) return
    const mq = window.matchMedia('(min-width: 768px)')
    const closeOnDesktop = (e: MediaQueryListEvent) => {
      if (e.matches) setSheetOpen(false)
    }
    mq.addEventListener('change', closeOnDesktop)
    return () => mq.removeEventListener('change', closeOnDesktop)
  }, [sheetOpen])

  const closeThen = (action: () => void) => {
    pendingAction.current = action
    setSheetOpen(false)
  }

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
          <span className="text-[12px] leading-none">{label(labelKey, t)}</span>
        </>
      )}
    </NavLink>
  )

  return (
    <Dialog open={sheetOpen} onOpenChange={setSheetOpen}>
      <nav
        className="fixed inset-x-0 bottom-0 z-40 flex items-center border-t border-[var(--border)] bg-[var(--surface)] md:hidden"
        style={{ height: 60, paddingBottom: 'env(safe-area-inset-bottom, 0px)' }}
      >
        {MOBILE_TABS.map((it) => tab(it.icon, it.labelKey, it.path))}
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
          // Sem Description: o título já nomeia o diálogo e o resto é navegação.
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
          {/* Alternativa ao toque no overlay/Esc para quem navega por leitor de tela ou teclado:
              invisível até receber foco. */}
          <DialogPrimitive.Close className="sr-only focus-visible:not-sr-only focus-visible:mb-3 focus-visible:block focus-visible:rounded-[8px] focus-visible:border focus-visible:border-[var(--border-strong)] focus-visible:px-3 focus-visible:py-2 focus-visible:text-sm focus-visible:ring-2 focus-visible:ring-ring">
            {t('nav.closeMenu')}
          </DialogPrimitive.Close>
          <nav data-testid="mobile-nav-sheet" className="flex flex-col gap-4">
            {NAV_SECTIONS.map((section) => {
              const visible = section.items.filter(
                (it) => !it.platformAction || hasPlatformPermission(role, it.platformAction),
              )
              if (visible.length === 0) return null
              return (
                <div key={section.titleKey} className="flex flex-col gap-0.5">
                  <span className="mb-1 px-3 text-[12px] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]">
                    {t(section.titleKey)}
                  </span>
                  {visible.map((it) => (
                    <NavRow
                      key={it.path}
                      item={it}
                      onNavigate={() => setSheetOpen(false)}
                      alwaysShowLabel
                    />
                  ))}
                </div>
              )
            })}
          </nav>
          <SidebarFooter
            className="mt-4"
            user={user}
            currentLang={currentLang}
            onChangePassword={() => closeThen(onChangePassword)}
            onSaveLanguage={onSaveLanguage}
            onLogout={() => closeThen(onLogout)}
            version={version}
            onNavigate={() => setSheetOpen(false)}
          />
        </DialogPrimitive.Content>
      </DialogPortal>
    </Dialog>
  )
}
