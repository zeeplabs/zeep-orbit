import { useSyncExternalStore } from 'react'
import { useBlocker } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ConfirmDialog } from '@/components/patterns/ConfirmDialog'
import { hasUnsavedChanges, subscribeUnsavedChanges } from '@/lib/useUnsavedChangesGuard'

/**
 * Pergunta antes de sair da tela quando algum formulário tem edição não salva
 * (ver `useUnsavedChangesGuard`). Precisa estar dentro de um data router.
 * Bloqueia qualquer mudança de path ou query string, porque trocar a aba `?tab=`
 * desmonta o formulário sujo.
 */
export function UnsavedChangesBlocker() {
  const { t } = useTranslation()
  useSyncExternalStore(subscribeUnsavedChanges, hasUnsavedChanges)
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      hasUnsavedChanges() &&
      currentLocation.pathname + currentLocation.search !== nextLocation.pathname + nextLocation.search,
  )

  return (
    <ConfirmDialog
      open={blocker.state === 'blocked'}
      destructive
      icon="edit_off"
      title={t('unsaved.title')}
      message={t('unsaved.message')}
      confirmLabel={t('unsaved.leave')}
      cancelLabel={t('unsaved.stay')}
      onConfirm={() => blocker.state === 'blocked' && blocker.proceed()}
      onCancel={() => blocker.state === 'blocked' && blocker.reset()}
    />
  )
}
