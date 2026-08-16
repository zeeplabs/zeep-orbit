import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'
import { safeReturnTo } from '@/lib/returnTo'
import logo from '@/assets/images/logo/logo.svg'

export default function GoogleSetupPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const location = useLocation()
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [done, setDone] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (password !== confirmPassword) {
      setError(t('googleSetup.passwordsDontMatch'))
      return
    }

    setLoading(true)
    try {
      const res = await fetch('/dashboard/api/me/google-setup', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, password, confirm_password: confirmPassword }),
      })

      if (!res.ok) {
        const data = await res.json()
        setError(data.error || t('common.errorSaving'))
        return
      }

      setDone(true)
      qc.invalidateQueries({ queryKey: ['me'] })
      qc.clear()
      const returnTo = safeReturnTo(location.search)
      setTimeout(() => {
        window.location.href = returnTo ? `/dashboard${returnTo}` : '/dashboard/apps'
      }, 1500)
    } catch {
      setError(t('common.connectionError'))
    } finally {
      setLoading(false)
    }
  }

  const inputClass = 'h-auto rounded-[9px] border px-3 py-2.5 text-[13px]'

  if (done) {
    return (
      <div
        className="flex min-h-screen items-center justify-center p-10"
        style={{ background: 'var(--bg-page)' }}
      >
        <div className="flex flex-col items-center gap-4">
          <div
            className="flex h-14 w-14 items-center justify-center rounded-full"
            style={{ background: 'var(--success-tint)' }}
          >
            <Icon name="check" size={28} style={{ color: 'var(--success)' }} />
          </div>
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
            {t('googleSetup.redirecting')}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div
      className="flex min-h-screen items-center justify-center p-10"
      style={{ background: 'var(--bg-page)' }}
    >
      <div
        className="w-full max-w-[460px] rounded-2xl border p-10"
        style={{
          background: 'var(--surface)',
          borderColor: 'var(--border)',
          boxShadow: 'var(--shadow-md)',
        }}
      >
        <div className="mb-8 flex flex-col items-center">
          <img
            src={logo}
            alt="Zeep Orbit"
            className="mb-4 h-[42px] w-[42px] object-contain"
          />
          <h1
            className="mb-1.5 text-center text-lg font-bold"
            style={{ color: 'var(--text-primary)' }}
          >
            {t('googleSetup.title')}
          </h1>
          <p
            className="text-center text-[13px]"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('googleSetup.description')}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="googleSetup-name" className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
              {t('googleSetup.name')}
            </Label>
            <Input
              id="googleSetup-name"
              type="text"
              placeholder="Ada Lovelace"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              autoComplete="name"
              className={cn(inputClass, 'bg-[var(--bg-page)]')}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="googleSetup-password" className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
              {t('googleSetup.password')}
            </Label>
            <div className="relative">
              <Input
                id="googleSetup-password"
                type={showPassword ? 'text' : 'password'}
                placeholder={t('googleSetup.passwordPlaceholder')}
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={8}
                className={cn(inputClass, 'w-full pr-10 bg-[var(--bg-page)]')}
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                aria-label={showPassword ? t('login.hidePassword') : t('login.showPassword')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 flex items-center justify-center"
                style={{ color: 'var(--text-tertiary)' }}
              >
                <Icon name={showPassword ? 'visibility_off' : 'visibility'} size={18} />
              </button>
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="googleSetup-confirm" className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
              {t('googleSetup.confirmPassword')}
            </Label>
            <Input
              id="googleSetup-confirm"
              type="password"
              placeholder={t('googleSetup.confirmPassword')}
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
              minLength={8}
              className={cn(inputClass, 'bg-[var(--bg-page)]')}
            />
          </div>

          {error && (
            <div
              className="rounded-[10px] border px-3 py-2 text-[13px]"
              style={{
                background: 'var(--danger-tint)',
                borderColor: 'var(--danger)',
                color: 'var(--danger)',
              }}
            >
              {error}
            </div>
          )}

          <Button
            type="submit"
            disabled={loading}
            className="mt-1 h-auto w-full rounded-[10px] py-3 text-[14px] font-bold"
          >
            {loading ? (
              <span className="flex items-center justify-center gap-2">
                <Icon name="progress_activity" size={14} className="animate-spin" />
                {t('googleSetup.saving')}
              </span>
            ) : (
              t('googleSetup.save')
            )}
          </Button>
        </form>
      </div>
    </div>
  )
}
