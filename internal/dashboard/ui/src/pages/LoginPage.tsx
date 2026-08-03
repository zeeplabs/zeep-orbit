import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { usePublicConfig } from '@/lib/api'
import logo from '@/assets/images/logo/logo.svg'
import logotype from '@/assets/images/logo/logotype.svg'
import pkg from '../../package.json'

export default function LoginPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)

  const { data: config } = usePublicConfig()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/dashboard/api/login', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })

      if (!res.ok) {
        const data = await res.json()
        setError(data.error || t('login.invalid'))
        return
      }

      qc.clear()
      window.location.href = '/dashboard/apps'
    } catch {
      setError(t('common.connectionError'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div
      className="flex min-h-screen"
      style={{ background: 'var(--bg-page)' }}
    >
      {/* Hero (left) — handoff linhas 91-103 */}
      <div
        className="relative hidden overflow-hidden md:flex md:flex-col md:justify-between"
        style={{
          flex: 1.1,
          background: 'var(--bg-surface)',
          borderRight: '1px solid var(--border)',
          padding: '56px',
        }}
      >
        {/* Gradient orbs decorativos (handoff linhas 92-93) */}
        <div
          className="pointer-events-none absolute"
          style={{
            top: '-120px',
            right: '-120px',
            width: '420px',
            height: '420px',
            borderRadius: '50%',
            background: 'radial-gradient(circle, var(--primary-tint) 0%, transparent 70%)',
          }}
        />
        <div
          className="pointer-events-none absolute"
          style={{
            bottom: '-160px',
            left: '-80px',
            width: '360px',
            height: '360px',
            borderRadius: '50%',
            background: 'radial-gradient(circle, var(--accent-tint) 0%, transparent 70%)',
          }}
        />

        {/* Logo (handoff linhas 94-97) */}
        <div className="relative flex items-center gap-2.5">
          <img src={logo} alt="Zeep Orbit" className="h-[34px] w-[34px] object-contain" />
          <img src={logotype} alt="Orbit" className="block h-[19px] w-auto" />
        </div>

        {/* Headline (handoff linhas 98-101) */}
        <div className="relative max-w-[420px]">
          <div
            className="mb-4 text-[38px] font-bold leading-[1.15]"
            style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
          >
            {t('login.title')}
          </div>
          <div
            className="text-[15px] leading-[1.6]"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('login.subtitle')}
          </div>
        </div>

        {/* Footer (handoff linha 102) */}
        <div className="relative text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
          {t('app.productName', { defaultValue: 'Zeep Orbit' })} · v{pkg.version}
        </div>
      </div>

      {/* Form (right) — handoff linhas 104-144 */}
      <div className="flex flex-1 items-center justify-center p-10">
        <div className="w-full max-w-[360px]">
          <div
            className="mb-1.5 text-[26px] font-bold"
            style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
          >
            {t('login.formTitle')}
          </div>
          <div className="mb-8 text-[14px]" style={{ color: 'var(--text-secondary)' }}>
            {t('login.formSubtitle')}
          </div>

          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <div>
              <Label htmlFor="email" className="mb-1.5 block text-[13px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
                {t('login.email')}
              </Label>
              <Input
                id="email"
                type="email"
                placeholder="you@company.com"
                autoComplete="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                className="h-auto rounded-[10px] border px-3.5 py-2.5 text-[14px]"
              />
            </div>
            <div>
              <Label htmlFor="password" className="mb-1.5 block text-[13px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
                {t('login.password')}
              </Label>
              <div className="relative">
                <Input
                  id="password"
                  type={showPassword ? 'text' : 'password'}
                  placeholder="••••••••"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  className="h-auto rounded-[10px] border px-3.5 py-2.5 pr-10 text-[14px]"
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

            {error && (
              <div
                className="rounded-lg border px-3 py-2 text-[13px]"
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
              className="mt-1 h-auto rounded-[10px] py-3 text-[14px] font-bold"
            >
              {loading ? t('login.signingIn') : t('login.signIn')}
            </Button>

            {config?.google_oauth_enabled && (
              <>
                <div className="my-1 flex items-center gap-2.5 text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                  <div className="h-px flex-1" style={{ background: 'var(--border)' }} />
                  <span>or</span>
                  <div className="h-px flex-1" style={{ background: 'var(--border)' }} />
                </div>

                <a
                  href="/dashboard/api/auth/google/login"
                  className="flex h-auto items-center justify-center gap-2.5 rounded-[10px] border py-2.5 text-[14px] font-semibold no-underline transition-colors"
                  style={{
                    borderColor: 'var(--border-strong)',
                    background: 'var(--bg-surface)',
                    color: 'var(--text-primary)',
                  }}
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
                    <path fill="#4285F4" d="M15.68 8.18c0-.58-.05-1.14-.15-1.68H8v3.18h4.3a3.68 3.68 0 0 1-1.6 2.42v2h2.6c1.52-1.4 2.38-3.46 2.38-5.92z" />
                    <path fill="#34A853" d="M8 16c2.16 0 3.97-.72 5.3-1.9l-2.6-2c-.72.48-1.64.77-2.7.77-2.08 0-3.84-1.4-4.47-3.3H.85v2.07A8 8 0 0 0 8 16z" />
                    <path fill="#FBBC05" d="M3.53 9.57A4.8 4.8 0 0 1 3.28 8c0-.55.1-1.08.25-1.57V4.36H.85A8 8 0 0 0 0 8c0 1.29.31 2.5.85 3.64l2.68-2.07z" />
                    <path fill="#EA4335" d="M8 3.18c1.18 0 2.23.4 3.06 1.2l2.3-2.3C11.96.9 10.15.14 8 .14A8 8 0 0 0 .85 4.36l2.68 2.07C4.16 4.53 5.92 3.18 8 3.18z" />
                  </svg>
                  {t('login.google')}
                </a>
              </>
            )}
          </form>
        </div>
      </div>
    </div>
  )
}
