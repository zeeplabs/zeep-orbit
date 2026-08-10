import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useBootstrap } from '../lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

export interface OnboardingPageProps {
  onComplete: () => void
}

type Step = 'welcome' | 'create-superadmin' | 'done'

const STEPS: Step[] = ['welcome', 'create-superadmin', 'done']

/** Step indicator: 3 círculos numerados, check quando done, primary quando current. */
function StepIndicator({ current }: { current: Step }) {
  const currentIdx = STEPS.indexOf(current)
  return (
    <div className="mb-8 flex items-center justify-center gap-0">
      {STEPS.map((s, i) => {
        const isDone = i < currentIdx
        const isCurrent = i === currentIdx
        return (
          <div key={s} className="flex items-center">
            <div
              className="flex h-7 w-7 items-center justify-center rounded-full text-[12px] font-bold"
              style={{
                background: isDone || isCurrent ? 'var(--primary)' : 'var(--bg-sunken)',
                color: isDone || isCurrent ? '#fff' : 'var(--text-tertiary)',
                border: isDone || isCurrent ? 'none' : '1px solid var(--border-strong)',
              }}
            >
              {isDone ? <Icon name="check" size={14} /> : i + 1}
            </div>
            {i < STEPS.length - 1 && (
              <div
                className="h-0.5 w-10"
                style={{ background: isDone ? 'var(--primary)' : 'var(--border)' }}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}

function WelcomeStep({ onNext }: { onNext: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="text-center">
      <div
        className="mx-auto mb-5 flex h-14 w-14 items-center justify-center rounded-[14px]"
        style={{ background: 'var(--primary-tint)' }}
      >
        <Icon name="rocket_launch" size={28} style={{ color: 'var(--primary)' }} />
      </div>
      <h1
        className="mb-2.5 text-[22px] font-bold"
        style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
      >
        {t('onboarding.welcome')}
      </h1>
      <p
        className="mx-auto mb-7 max-w-[340px] text-[14px] leading-[1.6]"
        style={{ color: 'var(--text-secondary)' }}
      >
        {t('onboarding.welcomeDesc')}
      </p>
      <Button
        onClick={onNext}
        className="h-auto w-full rounded-[10px] py-3 text-[14px] font-bold"
      >
        {t('onboarding.start')}
      </Button>
    </div>
  )
}

function CreateSuperadminStep({ onSuccess }: { onSuccess: () => void }) {
  const { t } = useTranslation()
  const [secret, setSecret] = useState('')
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [validationError, setValidationError] = useState('')

  const bootstrap = useBootstrap()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setValidationError('')

    if (password.length < 8) {
      setValidationError(t('onboarding.passwordMin'))
      return
    }
    if (password !== confirmPassword) {
      setValidationError(t('onboarding.passwordMismatch'))
      return
    }

    bootstrap.mutate(
      { secret, email, name, password },
      {
        onSuccess: () => onSuccess(),
        onError: (err) => setValidationError(err.message),
      },
    )
  }

  const error =
    validationError || (bootstrap.isError ? bootstrap.error?.message : '')

  const inputClass = 'h-auto rounded-[9px] border px-3 py-2.5 text-[13px]'

  return (
    <div>
      <h2
        className="mb-1 text-[20px] font-bold"
        style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
      >
        {t('onboarding.createAdmin')}
      </h2>
      <p
        className="mb-6 text-[13px]"
        style={{ color: 'var(--text-secondary)' }}
      >
        {t('onboarding.adminDesc')}
      </p>
      <form onSubmit={handleSubmit} className="flex flex-col gap-3.5">
        <div className="flex flex-col gap-1.5">
          <Label className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
            {t('onboarding.bootstrapSecret')}
          </Label>
          <Input
            type="password"
            placeholder={t('onboarding.bootstrapSecretPlaceholder')}
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            required
            autoComplete="off"
            className={cn(inputClass, 'bg-[var(--bg-page)]')}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
            {t('onboarding.name')}
          </Label>
          <Input
            type="text"
            placeholder="Ada Lovelace"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoComplete="name"
            className={cn(inputClass, 'bg-[var(--bg-page)]')}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
            {t('onboarding.email')}
          </Label>
          <Input
            type="email"
            placeholder="ada@company.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoComplete="email"
            className={cn(inputClass, 'bg-[var(--bg-page)]')}
          />
        </div>
        <div className="grid grid-cols-2 gap-2.5">
          <div className="flex flex-col gap-1.5">
            <Label className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
              {t('onboarding.password')}
            </Label>
            <Input
              type="password"
              placeholder={t('onboarding.passwordHint')}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              autoComplete="new-password"
              className={cn(inputClass, 'bg-[var(--bg-page)]')}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
              {t('onboarding.confirmPassword')}
            </Label>
            <Input
              type="password"
              placeholder={t('onboarding.confirmPassword')}
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
              autoComplete="new-password"
              className={cn(inputClass, 'bg-[var(--bg-page)]')}
            />
          </div>
        </div>
        {error && (
          <div
            className="rounded-md border px-3 py-2.5 text-[13px]"
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
          disabled={bootstrap.isPending}
          className="mt-1.5 h-auto w-full rounded-[10px] py-3 text-[14px] font-bold"
        >
          {bootstrap.isPending ? t('onboarding.submitting') : t('onboarding.submit')}
        </Button>
      </form>
    </div>
  )
}

function DoneStep({ onComplete }: { onComplete: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="text-center">
      <div
        className="mx-auto mb-5 flex h-14 w-14 items-center justify-center rounded-full"
        style={{ background: 'var(--success-tint)' }}
      >
        <Icon name="check" size={28} style={{ color: 'var(--success)' }} />
      </div>
      <h1
        className="mb-2.5 text-[22px] font-bold"
        style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
      >
        {t('onboarding.done')}
      </h1>
      <p
        className="mx-auto mb-7 max-w-[340px] text-[14px] leading-[1.6]"
        style={{ color: 'var(--text-secondary)' }}
      >
        {t('onboarding.doneDesc')}
      </p>
      <Button
        onClick={onComplete}
        className="h-auto w-full rounded-[10px] py-3 text-[14px] font-bold"
      >
        {t('onboarding.goLogin')}
      </Button>
    </div>
  )
}

/**
 * Tela de Onboarding (3-step superadmin creation, segundo de T2.10).
 * Handoff §F3-10: 3 steps com step indicator numerado (primary bg, check quando done),
 * card bg-surface com border + shadow-md, max-width 460px, padding 40px.
 * Drop framer-motion (AnimatePresence + stepVariants) — renderização condicional é suficiente.
 */
export default function OnboardingPage({ onComplete }: OnboardingPageProps) {
  const [step, setStep] = useState<Step>('welcome')

  const goTo = (next: Step) => setStep(next)

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
        <StepIndicator current={step} />

        {step === 'welcome' && <WelcomeStep onNext={() => goTo('create-superadmin')} />}
        {step === 'create-superadmin' && (
          <CreateSuperadminStep onSuccess={() => goTo('done')} />
        )}
        {step === 'done' && <DoneStep onComplete={onComplete} />}
      </div>
    </div>
  )
}
