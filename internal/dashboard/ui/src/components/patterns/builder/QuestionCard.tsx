import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { Textarea } from '@/components/ui/textarea'
import {
  BUILDER_ANSWER_MAX_CHARS,
  useAnswerBuilderQuestions,
  type BuilderAnswer,
  type BuilderProposal,
  type BuilderQuestion,
  type BuilderTurn,
} from '@/lib/api'
import { parseQuestions, type AnsweredRound } from './builderModel'

interface QuestionCardProps {
  sessionId: string
  turn: BuilderTurn
  proposal: BuilderProposal
  /** The builder cannot take actions right now (unavailable / archived). */
  disabled?: boolean
}

/** What the user has picked or typed for one question, kept until the round is sent. */
interface Draft {
  choices: string[]
  text: string
  decide: boolean
}

const EMPTY_DRAFT: Draft = { choices: [], text: '', decide: false }

/** One answer per question; a question left blank is sent as "decide for me". */
function toAnswers(questions: BuilderQuestion[], drafts: Record<string, Draft>): BuilderAnswer[] {
  return questions.map((q) => {
    const d = drafts[q.id] ?? EMPTY_DRAFT
    const text = d.text.trim()
    if (d.decide || (d.choices.length === 0 && text === '')) {
      return { id: q.id, choices: [], text: '', decide: true }
    }
    return { id: q.id, choices: d.choices, text, decide: false }
  })
}

interface QuestionBlockProps {
  /** Unique per card and question: names the radio group and the ids of the fields. */
  fieldId: string
  question: BuilderQuestion
  draft: Draft
  disabled: boolean
  onChange: (draft: Draft) => void
}

function QuestionBlock({ fieldId, question, draft, disabled, onChange }: QuestionBlockProps) {
  const { t } = useTranslation()
  const multiple = question.multiple === true
  const locked = disabled || draft.decide

  const toggle = (label: string, checked: boolean) => {
    if (!multiple) {
      onChange({ ...draft, choices: checked ? [label] : [] })
      return
    }
    const rest = draft.choices.filter((c) => c !== label)
    onChange({ ...draft, choices: checked ? [...rest, label] : rest })
  }

  return (
    <fieldset
      className="flex min-w-0 flex-col gap-2 rounded-[10px] border p-3"
      style={{ borderColor: 'var(--border)' }}
      aria-describedby={`${fieldId}-question`}
    >
      <legend className="px-1 text-[12.5px] font-semibold" style={{ color: 'var(--text-primary)' }}>
        {question.title}
      </legend>
      <p id={`${fieldId}-question`} className="break-words text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
        {question.question}{' '}
        <span style={{ color: 'var(--text-tertiary)' }}>
          {t(multiple ? 'builder.page.questions.chooseMany' : 'builder.page.questions.chooseOne')}
        </span>
      </p>
      <div className="flex flex-col gap-1.5">
        {question.options.map((option, i) => {
          const checked = draft.choices.includes(option.label)
          return (
            <label
              key={`${option.label}-${i}`}
              className="flex cursor-pointer items-start gap-2.5 rounded-[8px] border px-2.5 py-2 text-[12.5px] has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-60"
              style={{
                borderColor: checked && !draft.decide ? 'var(--primary)' : 'var(--border)',
                background: checked && !draft.decide ? 'var(--hover-surface)' : undefined,
              }}
            >
              <input
                type={multiple ? 'checkbox' : 'radio'}
                name={fieldId}
                value={option.label}
                checked={checked}
                disabled={locked}
                onChange={(e) => toggle(option.label, e.target.checked)}
                className="mt-[3px] shrink-0"
                style={{ accentColor: 'var(--primary)' }}
              />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="break-words font-medium" style={{ color: 'var(--text-primary)' }}>
                  {option.label}
                </span>
                {option.description && (
                  <span className="break-words text-[12px]" style={{ color: 'var(--text-secondary)' }}>
                    {option.description}
                  </span>
                )}
              </span>
            </label>
          )
        })}
      </div>
      <label htmlFor={`${fieldId}-text`} className="text-[12px] font-medium" style={{ color: 'var(--text-secondary)' }}>
        {t('builder.page.questions.ownAnswer')}
      </label>
      <Textarea
        id={`${fieldId}-text`}
        value={draft.text}
        onChange={(e) => onChange({ ...draft, text: e.target.value })}
        placeholder={t('builder.page.questions.ownAnswerPlaceholder')}
        maxLength={BUILDER_ANSWER_MAX_CHARS}
        rows={2}
        disabled={locked}
        className="resize-none text-[12.5px]"
      />
      <label className="flex cursor-pointer items-center gap-2 text-[12.5px] has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-60" style={{ color: 'var(--text-primary)' }}>
        <input
          type="checkbox"
          checked={draft.decide}
          disabled={disabled}
          onChange={(e) => onChange({ ...draft, decide: e.target.checked })}
          style={{ accentColor: 'var(--primary)' }}
        />
        {t('builder.page.questions.decide')}
      </label>
    </fieldset>
  )
}

/**
 * A turn paused on ask_user. The agent waits for the answers; nothing is written meanwhile.
 * What the user picks stays in this component until it is sent, so a failed request (toast)
 * keeps it. "Start building now" ends the questions: the agent proposes the plan with what it has.
 */
export function QuestionCard({ sessionId, turn, proposal, disabled }: QuestionCardProps) {
  const { t } = useTranslation()
  const questions = useMemo(() => parseQuestions(proposal.operation), [proposal.operation])
  const answer = useAnswerBuilderQuestions(sessionId)
  const [drafts, setDrafts] = useState<Record<string, Draft>>({})
  // After a success the turn resumes and the next refresh removes the card: keep it inert until then.
  const busy = answer.isPending || answer.isSuccess
  const skipping = answer.isPending && answer.variables?.input.skip === true
  const headingId = `questions-${turn.id}`

  const submit = (skip: boolean) => {
    if (busy || disabled) return
    answer.mutate({
      turn: turn.id,
      input: { tool_call_id: proposal.tool_call_id, skip, answers: questions ? toAnswers(questions, drafts) : [] },
    })
  }

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-3 rounded-[14px] border p-4"
      style={{ background: 'var(--surface)', borderColor: 'var(--primary)' }}
    >
      <div className="flex items-center gap-2">
        <Icon name="quiz" size={16} style={{ color: 'var(--primary)' }} />
        <h3 id={headingId} className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
          {t('builder.page.questions.title')}
        </h3>
      </div>
      <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
        {t(questions ? 'builder.page.questions.description' : 'builder.page.questions.unreadable')}
      </p>
      {questions && (
        <div className="flex flex-col gap-2.5">
          {questions.map((q) => (
            <QuestionBlock
              key={q.id}
              fieldId={`q-${turn.id}-${q.id}`}
              question={q}
              draft={drafts[q.id] ?? EMPTY_DRAFT}
              disabled={busy || !!disabled}
              onChange={(draft) => setDrafts((prev) => ({ ...prev, [q.id]: draft }))}
            />
          ))}
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        {questions && (
          <Button size="sm" className="gap-2" disabled={busy || disabled} onClick={() => submit(false)}>
            {answer.isPending && !skipping ? (
              <>
                <Icon name="progress_activity" size={14} className="animate-spin" />
                {t('builder.page.questions.sending')}
              </>
            ) : (
              <>
                <Icon name="send" size={14} />
                {t('builder.page.questions.send')}
              </>
            )}
          </Button>
        )}
        <Button
          size="sm"
          variant={questions ? 'outline' : 'default'}
          className="gap-2"
          disabled={busy || disabled}
          onClick={() => submit(true)}
        >
          {skipping ? (
            <>
              <Icon name="progress_activity" size={14} className="animate-spin" />
              {t('builder.page.questions.skipping')}
            </>
          ) : (
            <>
              <Icon name="rocket_launch" size={14} />
              {t('builder.page.questions.skip')}
            </>
          )}
        </Button>
      </div>
      <p className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
        {t('builder.page.questions.skipHint')}
      </p>
    </section>
  )
}

/**
 * An answered round in the history, read-only. The answers come from the event log, already
 * capped by the server; they are plain text. When the questions could not be read, the answers
 * are listed by question id.
 */
export function AnsweredRoundBlock({ round }: { round: AnsweredRound }) {
  const { t } = useTranslation()
  const rows = round.questions.length > 0
    ? round.questions.map((q) => ({ id: q.id, title: q.title, answer: round.answers[q.id] ?? '' }))
    : Object.entries(round.answers).map(([id, answer]) => ({ id, title: id, answer }))
  // '' is "decide for me" or a question left blank; on a skip too, the assistant decides it.
  const blank = t('builder.page.questions.decided')

  return (
    <div className="flex flex-col items-end gap-1">
      <span className="px-1 text-[11px] font-medium" style={{ color: 'var(--text-tertiary)' }}>
        {t('builder.page.chat.you')}
      </span>
      <div
        className="flex max-w-[92%] flex-col gap-2 rounded-[12px] border px-3.5 py-2.5 text-[12.5px]"
        style={{ borderColor: 'var(--border)', background: 'var(--hover-surface)', color: 'var(--text-primary)' }}
      >
        <div className="flex items-center gap-1.5 font-semibold">
          <Icon name="quiz" size={14} style={{ color: 'var(--text-secondary)' }} />
          {t('builder.page.questions.answeredTitle')}
        </div>
        {rows.length > 0 && (
          <dl className="flex flex-col gap-1.5">
            {rows.map((row) => (
              <div key={row.id} className="flex min-w-0 flex-col">
                <dt className="break-words text-[12px]" style={{ color: 'var(--text-secondary)' }}>
                  {row.title}
                </dt>
                <dd className="break-words" style={row.answer ? undefined : { color: 'var(--text-tertiary)' }}>
                  {row.answer || blank}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {round.skipped && (
          <div className="flex items-center gap-1.5 text-[12px]" style={{ color: 'var(--text-secondary)' }}>
            <Icon name="rocket_launch" size={14} />
            {t('builder.page.questions.skippedNote')}
          </div>
        )}
      </div>
    </div>
  )
}
