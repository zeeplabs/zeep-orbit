import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'

/**
 * The backend part of the builder header: a link to the backend app, or, for a project with no
 * backend (a site), a note that says so and a way to start a project that has one. The link goes to
 * the chat home with `?backend=new`, which preselects a new backend there.
 */
export function BackendLink({ backendAppId }: { backendAppId: string | null }) {
  const { t } = useTranslation()
  if (backendAppId) {
    return (
      <Link
        to={`/apps/${encodeURIComponent(backendAppId)}`}
        className="inline-flex items-center gap-1 underline-offset-2 hover:underline"
        style={{ color: 'var(--text-secondary)' }}
      >
        <Icon name="database" size={14} />
        {t('builder.page.backendApp')}
      </Link>
    )
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2" style={{ color: 'var(--text-tertiary)' }}>
      <span className="inline-flex items-center gap-1">
        <Icon name="web" size={14} />
        {t('builder.page.noBackend')}
      </span>
      <span>{t('builder.page.noBackendNeedData')}</span>
      <Link to="/?backend=new" className="underline underline-offset-2" style={{ color: 'var(--text-secondary)' }}>
        {t('builder.page.noBackendStartWith')}
      </Link>
    </span>
  )
}
