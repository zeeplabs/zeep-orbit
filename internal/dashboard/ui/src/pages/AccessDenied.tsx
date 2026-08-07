import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { Button } from '@/components/ui/button'

export default function AccessDenied() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-3 text-center">
      <div
        className="flex h-14 w-14 items-center justify-center rounded-[14px]"
        style={{ background: 'var(--danger-tint)', color: 'var(--danger)' }}
      >
        <Icon name="block" size={28} />
      </div>
      <h1
        className="text-xl font-bold text-[var(--text-primary)]"
        style={{ fontFamily: 'var(--font-display)' }}
      >
        {t('accessDenied.title')}
      </h1>
      <p className="max-w-sm text-sm text-[var(--text-secondary)]">{t('accessDenied.desc')}</p>
      <Button className="mt-1" onClick={() => navigate('/apps')}>
        {t('accessDenied.back')}
      </Button>
    </div>
  )
}
