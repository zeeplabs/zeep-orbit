import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import ChangePasswordModal from '@/pages/ChangePasswordModal'
import { ConfirmDialog } from '@/components/patterns/ConfirmDialog'
import {
  confirmLeavingWithUnsavedChanges,
  hasUnsavedChanges,
  resetLeavingConfirmation,
} from '@/lib/useUnsavedChangesGuard'
import { authFetch } from '@/lib/api'
import { setLanguage } from '@/lib/i18n'

/**
 * The session actions both shells (DashboardShell and SimpleShell) offer in
 * their footer: switch language, change password and log out. Keeping them in
 * one place means a fix to the logout flow reaches both interfaces.
 */
export function useShellSession(user: { language?: string } | null) {
  const qc = useQueryClient()
  const { t, i18n } = useTranslation()
  const [showLogoutDialog, setShowLogoutDialog] = useState(false)
  const [loggingOut, setLoggingOut] = useState(false)
  const [showChangePassword, setShowChangePassword] = useState(false)

  useEffect(() => {
    if (user?.language && user.language !== i18n.language) {
      setLanguage(user.language)
    }
  }, [user?.language])

  async function saveLanguage(lang: string) {
    setLanguage(lang)
    try {
      await authFetch('/dashboard/api/me/language', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ language: lang }),
      })
    } catch {
      // best-effort persistence; the language already changed locally
    }
  }

  const handleLogout = async () => {
    setLoggingOut(true)
    // The dialog already warned about unsaved edits (logoutDescUnsaved). Without this the
    // browser's own prompt would show after the session is revoked, and "Stay" would leave
    // the user on a page whose saves can only fail.
    confirmLeavingWithUnsavedChanges()
    try {
      const res = await fetch('/dashboard/api/logout', { method: 'POST', credentials: 'include' })
      // Without this the user would go to /login with the session still alive and come
      // straight back into the app.
      if (!res.ok) throw new Error('logout failed')
      qc.clear()
      window.location.href = '/dashboard/login'
    } catch {
      resetLeavingConfirmation()
      toast.error(t('nav.logoutError'))
    } finally {
      setLoggingOut(false)
    }
  }

  return {
    currentLang: i18n.language,
    saveLanguage,
    openChangePassword: () => setShowChangePassword(true),
    openLogout: () => setShowLogoutDialog(true),
    dialogs: (
      <>
        <ConfirmDialog
          open={showLogoutDialog}
          title={t('nav.logoutConfirm')}
          message={showLogoutDialog && hasUnsavedChanges() ? t('nav.logoutDescUnsaved') : t('nav.logoutDesc')}
          confirmLabel={loggingOut ? t('nav.loggingOut') : t('nav.logoutConfirmBtn')}
          cancelLabel={t('nav.logoutCancel')}
          icon="logout"
          loading={loggingOut}
          onConfirm={handleLogout}
          onCancel={() => setShowLogoutDialog(false)}
        />

        <ChangePasswordModal open={showChangePassword} onClose={() => setShowChangePassword(false)} />
      </>
    ),
  }
}
