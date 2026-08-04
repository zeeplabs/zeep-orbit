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
      className="flex flex-1 items-center justify-center rounded-[8px] border border-[var(--border-strong)] bg-transparent py-2 text-[var(--text-secondary)] transition-colors hover:bg-[var(--hover-surface)] hover:text-[var(--text-primary)]"
    >
      {children ?? (icon && <Icon name={icon} size={17} />)}
    </button>
  )
}

const GITHUB_URL = 'https://github.com/zeeplabs/zeep-orbit'

function GitHubBtn() {
  return (
    <a
      href={GITHUB_URL}
      target="_blank"
      rel="noopener noreferrer"
      title="GitHub"
      className="flex flex-1 items-center justify-center rounded-[8px] border border-[var(--border-strong)] bg-transparent py-2 text-[var(--text-secondary)] transition-colors hover:bg-[var(--hover-surface)] hover:text-[var(--text-primary)]"
    >
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
      </svg>
    </a>
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
      <div className="mb-2.5 flex items-center gap-2.5 rounded-[10px] bg-[var(--sunken)] p-2.5">
        <div
          className="h-8 w-8 shrink-0 rounded-full"
          style={{ background: 'linear-gradient(135deg, var(--primary), var(--accent))' }}
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-bold text-[var(--text-primary)]">
            {user.name ? firstLastName(user.name) : user.email}
          </p>
          <p className="text-[11px] capitalize text-[var(--text-tertiary)]">{user.role}</p>
        </div>
      </div>
      <div className="flex items-center gap-1.5 pb-3">
        <GitHubBtn />
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
