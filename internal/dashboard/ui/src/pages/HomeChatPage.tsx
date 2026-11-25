import { useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/ui/icon'
import { useApps, useBuilderSessions, useStartBuilderProject, type BuilderBackendChoice } from '@/lib/api'
import { recentSessions } from '@/components/layout/SimpleSidebar'

const NEW_BACKEND = '__new__'
const NO_BACKEND = '__none__'
// Each chip shows a short title and fills the composer with the full example prompt, so the user
// sees how much detail to give: who uses it, what it stores, who signs in, the look.
const SUGGESTIONS = [
  { title: 'home.suggestion1', prompt: 'home.suggestion1Prompt' },
  { title: 'home.suggestion2', prompt: 'home.suggestion2Prompt' },
  { title: 'home.suggestion3', prompt: 'home.suggestion3Prompt' },
  { title: 'home.suggestion4', prompt: 'home.suggestion4Prompt' },
] as const
const CONTINUE_LIMIT = 3

/**
 * Chat home (`/`, simple shell): describe an app, get a project. Creates the session and
 * posts the prompt through useStartBuilderProject, then opens the builder. On a failure the
 * text stays in the box and the API error is shown by the hook (HCH-02 to HCH-07).
 */
export default function HomeChatPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const start = useStartBuilderProject()
  const apps = useApps().data ?? []
  const sessions = useBuilderSessions().data ?? []
  const recents = recentSessions(sessions, CONTINUE_LIMIT)

  const [prompt, setPrompt] = useState('')
  // "No backend" is the default: most first requests are a page. `?backend=new` (the link in a
  // project with no backend) opens the home with a new backend preselected.
  const [searchParams] = useSearchParams()
  const [backendValue, setBackendValue] = useState(searchParams.get('backend') === 'new' ? NEW_BACKEND : NO_BACKEND)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const pending = start.isPending
  const canSend = prompt.trim() !== '' && !pending

  function submit() {
    if (!canSend) return
    const backend: BuilderBackendChoice =
      backendValue === NO_BACKEND
        ? { mode: 'none' }
        : backendValue === NEW_BACKEND
          ? { mode: 'new' }
          : { mode: 'existing', app_id: backendValue }
    start.mutate(
      { prompt, backend, locale: i18n.language },
      { onSuccess: (session) => navigate(`/builder/${session.id}`) },
    )
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    submit()
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    // Enter sends, Shift+Enter is a newline; never send while an IME is composing a character.
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      submit()
    }
  }

  function fillSuggestion(text: string) {
    setPrompt(text)
    textareaRef.current?.focus()
  }

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col items-center px-6 py-14 max-md:px-4 max-md:py-8">
      <h1 className="m-0 text-center text-[32px] font-bold leading-tight text-[var(--text-primary)] max-md:text-[26px]">
        {t('home.heading')}
      </h1>
      <p className="mb-8 mt-2 text-center text-[15px] text-[var(--text-secondary)]">{t('home.subtitle')}</p>

      <form
        onSubmit={onSubmit}
        aria-busy={pending}
        className="w-full rounded-[16px] border border-[var(--border)] bg-[var(--surface)] p-4 shadow-[var(--shadow-md)]"
      >
        <label htmlFor="home-prompt" className="sr-only">
          {t('home.composerLabel')}
        </label>
        <textarea
          id="home-prompt"
          ref={textareaRef}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={onKeyDown}
          readOnly={pending}
          rows={3}
          placeholder={t('home.composerPlaceholder')}
          aria-describedby="home-prompt-hint"
          className="block max-h-64 min-h-[4.5rem] w-full resize-none overflow-y-auto border-0 bg-transparent p-0 text-base [field-sizing:content] leading-relaxed text-[var(--text-primary)] outline-none placeholder:text-[var(--text-tertiary)]"
        />
        <p id="home-prompt-hint" className="sr-only">
          {t('home.composerHint')}
        </p>

        <div className="mt-3 flex items-center justify-between gap-3">
          <select
            aria-label={t('home.backendLabel')}
            value={backendValue}
            onChange={(e) => setBackendValue(e.target.value)}
            disabled={pending}
            className="min-w-0 max-w-[60%] truncate rounded-[8px] border border-[var(--border-strong)] bg-transparent px-2.5 py-1.5 text-[13px] text-[var(--text-secondary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <option value={NO_BACKEND}>{t('home.backendNone')}</option>
            <option value={NEW_BACKEND}>{t('builder.newSession.backendNew')}</option>
            {apps.length > 0 && (
              <optgroup label={t('home.backendExistingGroup')}>
                {apps.map((app) => (
                  <option key={app.id} value={app.id}>
                    {app.name}
                  </option>
                ))}
              </optgroup>
            )}
          </select>

          <button
            type="submit"
            disabled={!canSend}
            aria-label={pending ? t('home.sending') : t('home.send')}
            title={pending ? t('home.sending') : t('home.send')}
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-[var(--primary)] text-white transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-40"
          >
            <Icon name={pending ? 'progress_activity' : 'arrow_upward'} size={20} className={pending ? 'animate-spin' : undefined} />
          </button>
        </div>
      </form>

      <ul aria-label={t('home.suggestionsLabel')} className="m-0 mt-5 flex list-none flex-wrap justify-center gap-2 p-0">
        {SUGGESTIONS.map(({ title, prompt: promptKey }) => (
          <li key={title}>
            <button
              type="button"
              onClick={() => fillSuggestion(t(promptKey))}
              disabled={pending}
              className="rounded-full border border-[var(--border)] bg-transparent px-3.5 py-1.5 text-[13px] text-[var(--text-secondary)] transition-colors hover:bg-[var(--hover-surface)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              {t(title)}
            </button>
          </li>
        ))}
      </ul>

      {recents.length > 0 && (
        <section aria-labelledby="home-continue" className="mt-12 w-full">
          <div className="mb-3 flex items-baseline justify-between">
            <h2 id="home-continue" className="m-0 text-[15px] font-semibold text-[var(--text-primary)]">
              {t('home.continueTitle')}
            </h2>
            <Link to="/projetos" className="text-[13px] text-[var(--primary)] no-underline hover:underline">
              {t('home.viewAll')}
            </Link>
          </div>
          <ul className="m-0 grid list-none grid-cols-3 gap-3 p-0 max-md:grid-cols-1">
            {recents.map((s) => (
              <li key={s.id} className="min-w-0">
                <Link
                  to={`/builder/${s.id}`}
                  className="flex h-full flex-col gap-1 rounded-[12px] border border-[var(--border)] bg-[var(--surface)] p-3.5 no-underline transition-colors hover:bg-[var(--hover-surface)]"
                >
                  <span className="truncate text-sm font-semibold text-[var(--text-primary)]">
                    {s.title || t('nav.simple.untitled')}
                  </span>
                  <span className="text-[12px] text-[var(--text-tertiary)]">
                    {t('home.updatedAt', {
                      date: new Date(s.updated_at).toLocaleDateString(i18n.language, { dateStyle: 'medium' }),
                    })}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
