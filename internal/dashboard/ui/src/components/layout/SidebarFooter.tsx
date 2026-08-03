import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router-dom'
import { Icon } from '@/components/ui/icon'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

function firstLastName(name: string): string {
  const parts = name.trim().split(/\s+/)
  if (parts.length <= 1) return parts[0] || ''
  return `${parts[0]} ${parts[parts.length - 1]}`
}

interface FooterUser {
  email: string
  name?: string
  role: string
}

function IconBtn({
  icon,
  title,
  onClick,
  children,
}: {
  icon?: string
  title: string
  onClick: () => void
  children?: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-label={title}
      className="flex h-9 w-9 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--surface)] text-[var(--text-secondary)] transition-colors hover:bg-[var(--hover-surface)] hover:text-[var(--text-primary)]"
    >
      {children ?? (icon && <Icon name={icon} size={17} />)}
    </button>
  )
}

interface SidebarFooterProps {
  user: FooterUser
  currentLang: string
  onChangePassword: () => void
  onSaveLanguage: (lang: string) => void
  onLogout: () => void
  className?: string
  /** Versão do app exibida no rodapé (handoff linha 267). */
  version?: string
  /** Callback executado quando o usuário fecha o bottom-sheet (mobile only). */
  onNavigate?: () => void
}

/** Rodapé da sidebar: usuário + toggles de tema/idioma + change-pw + github + logout.
 *  Handoff §F1-07: renderiza também a versão (`Zeep Orbit · v{version}`) e o link
 *  de Changelog, que o protótipo posiciona no footer (linhas 249-251 e 267). */
export function SidebarFooter({
  user,
  currentLang,
  onChangePassword,
  onSaveLanguage,
  onLogout,
  className,
  version,
  onNavigate,
}: SidebarFooterProps) {
  const { t } = useTranslation()
  const { mode, toggle } = useTheme()
  const isPt = currentLang === 'pt-BR'

  return (
    <div className={cn('border-t border-[var(--border)] pt-3.5', className)}>
      <NavLink
        to="/changelog"
        onClick={onNavigate}
        className="mb-2 flex items-center gap-2 px-2 text-[13px] no-underline transition-colors"
        style={{ color: 'var(--text-secondary)' }}
      >
        <Icon name="campaign" size={17} />
        <span>{t('nav.changelog')}</span>
      </NavLink>
      <div className="mb-2.5 px-2 text-center">
        <p className="truncate text-[13px] font-semibold text-[var(--text-primary)]">
          {user.name ? firstLastName(user.name) : user.email}
        </p>
        <p className="mt-0.5 text-[11px] capitalize text-[var(--text-tertiary)]">{user.role}</p>
      </div>
      <div className="flex items-center justify-center gap-2 pb-3">
        <IconBtn
          icon={mode === 'dark' ? 'light_mode' : 'dark_mode'}
          title={mode === 'dark' ? t('theme.light') : t('theme.dark')}
          onClick={toggle}
        />
        <IconBtn
          title={isPt ? t('language.ptBR') : t('language.en')}
          onClick={() => onSaveLanguage(isPt ? 'en' : 'pt-BR')}
        >
          <span className="text-[15px] leading-none">{isPt ? '🇧🇷' : '🇺🇸'}</span>
        </IconBtn>
        <IconBtn icon="lock" title={t('nav.changePassword')} onClick={onChangePassword} />
        <a
          href="https://github.com/zeeplabs/zeep-orbit"
          target="_blank"
          rel="noopener noreferrer"
          title="GitHub"
          className="flex h-9 w-9 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--surface)] text-[var(--text-secondary)] transition-colors hover:bg-[var(--hover-surface)] hover:text-[var(--text-primary)]"
        >
          <Icon name="code_blocks" size={17} />
        </a>
        <IconBtn icon="logout" title={t('nav.logout')} onClick={onLogout} />
      </div>
      {version && (
        <p
          className="pb-1 text-center text-[10.5px]"
          style={{ color: 'var(--text-tertiary)' }}
        >
          {t('app.productName', { defaultValue: 'Zeep Orbit' })} · v{version}
        </p>
      )}
    </div>
  )
}
