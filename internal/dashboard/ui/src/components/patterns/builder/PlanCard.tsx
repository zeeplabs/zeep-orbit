import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { Textarea } from '@/components/ui/textarea'
import {
  BUILDER_FEEDBACK_MAX_CHARS,
  useApproveBuilderPlan,
  useBuilderPlanEstimate,
  useReviseBuilderPlan,
  type BuilderEstimateRange,
  type BuilderPlan,
  type BuilderProposal,
  type BuilderTurn,
} from '@/lib/api'
import { formatMoney, formatTokens } from '@/lib/money'
import { parsePlan, planAccess, type ApprovedPlan } from './builderModel'

interface PlanCardProps {
  sessionId: string
  turn: BuilderTurn
  proposal: BuilderProposal
  /** The builder cannot take actions right now (unavailable / archived). */
  disabled?: boolean
}

function PlanSection({ title, icon, children }: { title: string; icon: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <h4 className="flex items-center gap-1.5 text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
        <Icon name={icon} size={14} />
        {title}
      </h4>
      {children}
    </div>
  )
}

/** One table of the plan, in words. A public table, or one with no declared access, is drawn as a warning. */
function DataRow({ entry }: { entry: BuilderPlan['data'][number] }) {
  const { t } = useTranslation()
  const access = planAccess(entry.access)
  const warn = access === 'public' || access === 'unknown'
  return (
    <li className="flex flex-col gap-1 rounded-[10px] border p-2.5" style={{ borderColor: warn ? 'var(--warning-text)' : 'var(--border)' }}>
      <div className="break-words">
        <span className="font-medium">{entry.table}</span>
        {entry.holds && <span style={{ color: 'var(--text-secondary)' }}> — {entry.holds}</span>}
      </div>
      <div className="flex flex-col gap-0.5 text-[12px]" style={{ color: 'var(--text-secondary)' }}>
        <div className="flex items-center gap-1.5 font-semibold" style={{ color: warn ? 'var(--warning-text)' : 'var(--text-secondary)' }}>
          <Icon name={access === 'public' ? 'public' : access === 'unknown' ? 'help' : 'lock'} size={14} />
          <span>
            {t('builder.page.plan.accessLabel')}: {t(`builder.page.plan.access.${access}`)}
          </span>
        </div>
        {access === 'public' && <span>{t('builder.page.plan.publicWarning')}</span>}
        {entry.access_note && <span className="break-words">{entry.access_note}</span>}
      </div>
    </li>
  )
}

/**
 * The plan in sections, read-only: shared by the card and the approved plan in the history. Every
 * text is the model's, rendered as plain text.
 */
export function PlanBody({ plan }: { plan: BuilderPlan }) {
  const { t } = useTranslation()
  const look = [
    { key: 'direction', value: plan.look.direction },
    { key: 'type', value: plan.look.type },
    { key: 'density', value: plan.look.density },
    { key: 'motion', value: plan.look.motion },
    { key: 'style', value: plan.look.style },
    { key: 'colors', value: plan.look.colors },
    { key: 'uiLanguage', value: plan.look.ui_language },
    { key: 'reference', value: plan.look.reference },
  ].filter((row): row is { key: string; value: string } => Boolean(row.value))

  return (
    <div className="flex flex-col gap-3 text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
      {plan.summary && <p className="break-words">{plan.summary}</p>}

      {plan.screens.length > 0 && (
        <PlanSection title={t('builder.page.plan.screens')} icon="web">
          <ul className="flex flex-col gap-1">
            {plan.screens.map((s, i) => (
              <li key={`${s.name}-${i}`} className="break-words">
                <span className="font-medium">{s.name}</span>
                {s.purpose && <span style={{ color: 'var(--text-secondary)' }}> — {s.purpose}</span>}
              </li>
            ))}
          </ul>
        </PlanSection>
      )}

      {plan.data.length > 0 && (
        <PlanSection title={t('builder.page.plan.data')} icon="database">
          <ul className="flex flex-col gap-1.5">
            {plan.data.map((d, i) => (
              <DataRow key={`${d.table}-${i}`} entry={d} />
            ))}
          </ul>
          <p className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
            {t('builder.page.plan.dataHint')}
          </p>
        </PlanSection>
      )}

      {look.length > 0 && (
        <PlanSection title={t('builder.page.plan.look')} icon="palette">
          <dl className="flex flex-col gap-1">
            {look.map((row) => (
              <div key={row.key} className="flex min-w-0 flex-wrap gap-x-1.5">
                <dt style={{ color: 'var(--text-secondary)' }}>{t(`builder.page.plan.lookField.${row.key}`)}:</dt>
                <dd className="min-w-0 break-words">{row.value}</dd>
              </div>
            ))}
          </dl>
        </PlanSection>
      )}

      {plan.assumptions.length > 0 && (
        <div
          className="flex flex-col gap-1.5 rounded-[10px] border border-dashed p-2.5"
          style={{ borderColor: 'var(--warning-text)', background: 'var(--hover-surface)' }}
        >
          <PlanSection title={t('builder.page.plan.assumptions')} icon="lightbulb">
            <p className="text-[12px]" style={{ color: 'var(--text-secondary)' }}>
              {t('builder.page.plan.assumptionsHint')}
            </p>
            <ul className="flex list-disc flex-col gap-1 pl-5">
              {plan.assumptions.map((a, i) => (
                <li key={`${a}-${i}`} className="break-words">
                  {a}
                </li>
              ))}
            </ul>
          </PlanSection>
        </div>
      )}

      {plan.out_of_scope.length > 0 && (
        <PlanSection title={t('builder.page.plan.outOfScope')} icon="block">
          <ul className="flex list-disc flex-col gap-1 pl-5" style={{ color: 'var(--text-secondary)' }}>
            {plan.out_of_scope.map((o, i) => (
              <li key={`${o}-${i}`} className="break-words">
                {o}
              </li>
            ))}
          </ul>
        </PlanSection>
      )}

      <PlanSection title={t('builder.page.plan.milestones')} icon="flag">
        <ol className="flex flex-col gap-1.5">
          {plan.milestones.map((m, i) => (
            <li key={`${m.title}-${i}`} className="flex gap-2.5 rounded-[10px] border p-2.5" style={{ borderColor: 'var(--border)' }}>
              <span className="shrink-0 font-semibold tabular-nums" style={{ color: 'var(--text-secondary)' }}>
                {i + 1}.
              </span>
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="break-words font-medium">{m.title}</span>
                {m.deliverable && (
                  <span className="break-words text-[12px]" style={{ color: 'var(--text-secondary)' }}>
                    {m.deliverable}
                  </span>
                )}
                {m.done && (
                  <span className="flex items-center gap-1 text-[12px] font-medium" style={{ color: 'var(--success-text)' }}>
                    <Icon name="check_circle" size={14} />
                    {t('builder.page.plan.milestoneDone')}
                  </span>
                )}
              </div>
            </li>
          ))}
        </ol>
      </PlanSection>
    </div>
  )
}

/** "40K–80K tokens · $0.40–$0.80", or tokens only when the sample has no full price. */
function useRangeText(): (range: BuilderEstimateRange, currency: string) => string {
  const { t } = useTranslation()
  return (range, currency) => {
    const tokens = t('builder.page.plan.estimate.tokens', {
      low: formatTokens(range.tokens_low, true),
      high: formatTokens(range.tokens_high, true),
    })
    if (range.cost_micros_low === null || range.cost_micros_high === null) return tokens
    return t('builder.page.plan.estimate.withCost', {
      tokens,
      low: formatMoney(range.cost_micros_low, currency),
      high: formatMoney(range.cost_micros_high, currency),
    })
  }
}

/** The estimate of the pending plan. Every figure comes from the server; nothing is computed here. */
function EstimateLine({ sessionId, turnId, toolCallId }: { sessionId: string; turnId: string; toolCallId: string }) {
  const { t } = useTranslation()
  const estimate = useBuilderPlanEstimate(sessionId, turnId, toolCallId)
  const rangeText = useRangeText()
  const data = estimate.data

  let body: ReactNode
  if (estimate.isPending) {
    body = (
      <span className="flex items-center gap-1.5">
        <Icon name="progress_activity" size={14} className="animate-spin" />
        {t('builder.page.plan.estimate.loading')}
      </span>
    )
  } else if (!data || !data.available || !data.milestone) {
    body = <span>{t(data ? 'builder.page.plan.estimate.notEnough' : 'builder.page.plan.estimate.unavailable')}</span>
  } else {
    const priced = data.milestone.cost_micros_low !== null && data.milestone.cost_micros_high !== null
    body = (
      <div className="flex flex-col gap-0.5">
        <span>
          {t('builder.page.plan.estimate.milestone')}: <span className="font-medium">{rangeText(data.milestone, data.currency)}</span>
        </span>
        {data.plan && data.milestones > 1 && (
          <span>
            {t('builder.page.plan.estimate.plan', { count: data.milestones })}:{' '}
            <span className="font-medium">{rangeText(data.plan, data.currency)}</span>
          </span>
        )}
        <span className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
          {t('builder.page.plan.estimate.source', { count: data.sample_turns })}
          {!priced && ` ${t('builder.page.plan.estimate.tokensOnly')}`}
        </span>
      </div>
    )
  }

  return (
    <div
      className="flex gap-2 rounded-[10px] border p-2.5 text-[12.5px]"
      style={{ borderColor: 'var(--border)', color: 'var(--text-secondary)' }}
      aria-live="polite"
    >
      <Icon name="savings" size={16} className="mt-[1px] shrink-0" />
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="font-semibold" style={{ color: 'var(--text-primary)' }}>
          {t('builder.page.plan.estimate.title')}
        </span>
        {body}
      </div>
    </div>
  )
}

/**
 * A turn paused on propose_build_plan. Nothing is built until the user approves: approval stores
 * the plan as the brief and the same turn starts the first milestone. "Ask for changes" sends the
 * user's words back; "Back to questions" grants the agent one more round of questions.
 */
export function PlanCard({ sessionId, turn, proposal, disabled }: PlanCardProps) {
  const { t } = useTranslation()
  const plan = useMemo(() => parsePlan(proposal.operation), [proposal.operation])
  const approve = useApproveBuilderPlan(sessionId)
  const revise = useReviseBuilderPlan(sessionId)
  const [editing, setEditing] = useState(false)
  const [feedback, setFeedback] = useState('')
  // After a success the turn resumes and the next refresh removes the card: keep it inert until then.
  const busy = approve.isPending || revise.isPending || approve.isSuccess || revise.isSuccess
  const locked = busy || !!disabled
  const moreQuestionsPending = revise.isPending && revise.variables?.input.more_questions === true
  const feedbackPending = revise.isPending && !moreQuestionsPending
  const headingId = `plan-${turn.id}`
  const feedbackId = `plan-feedback-${turn.id}`

  const sendRevision = (moreQuestions: boolean) => {
    const text = feedback.trim()
    if (locked || (!moreQuestions && text === '')) return
    revise.mutate({
      turn: turn.id,
      input: { tool_call_id: proposal.tool_call_id, feedback: moreQuestions ? '' : text, more_questions: moreQuestions },
    })
  }

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-3 rounded-[14px] border p-4"
      style={{ background: 'var(--surface)', borderColor: 'var(--primary)' }}
    >
      <div className="flex items-center gap-2">
        <Icon name="map" size={16} style={{ color: 'var(--primary)' }} />
        <h3 id={headingId} className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
          {t('builder.page.plan.title')}
        </h3>
      </div>
      <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
        {t(plan ? 'builder.page.plan.description' : 'builder.page.plan.unreadable')}
      </p>

      {plan && <PlanBody plan={plan} />}
      {plan && <EstimateLine sessionId={sessionId} turnId={turn.id} toolCallId={proposal.tool_call_id} />}

      {editing && (
        <div className="flex flex-col gap-1.5">
          <label htmlFor={feedbackId} className="text-[12px] font-medium" style={{ color: 'var(--text-secondary)' }}>
            {t('builder.page.plan.feedbackLabel')}
          </label>
          <Textarea
            id={feedbackId}
            autoFocus
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            placeholder={t('builder.page.plan.feedbackPlaceholder')}
            maxLength={BUILDER_FEEDBACK_MAX_CHARS}
            rows={3}
            disabled={locked}
            className="resize-none text-[12.5px]"
          />
          <div className="flex flex-wrap gap-2">
            <Button size="sm" className="gap-2" disabled={locked || feedback.trim() === ''} onClick={() => sendRevision(false)}>
              {feedbackPending ? (
                <>
                  <Icon name="progress_activity" size={14} className="animate-spin" />
                  {t('builder.page.plan.sendingChanges')}
                </>
              ) : (
                <>
                  <Icon name="send" size={14} />
                  {t('builder.page.plan.sendChanges')}
                </>
              )}
            </Button>
            <Button size="sm" variant="ghost" disabled={locked} onClick={() => setEditing(false)}>
              {t('builder.page.plan.cancelChanges')}
            </Button>
          </div>
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        {plan && (
          <Button
            size="sm"
            className="gap-2"
            disabled={locked}
            onClick={() => approve.mutate({ turn: turn.id, input: { tool_call_id: proposal.tool_call_id } })}
          >
            {approve.isPending ? (
              <>
                <Icon name="progress_activity" size={14} className="animate-spin" />
                {t('builder.page.plan.approving')}
              </>
            ) : (
              <>
                <Icon name="rocket_launch" size={14} />
                {t('builder.page.plan.approve')}
              </>
            )}
          </Button>
        )}
        {!editing && (
          <Button size="sm" variant={plan ? 'outline' : 'default'} className="gap-2" disabled={locked} onClick={() => setEditing(true)}>
            <Icon name="edit_note" size={14} />
            {t('builder.page.plan.askChanges')}
          </Button>
        )}
        <Button size="sm" variant="outline" className="gap-2" disabled={locked} onClick={() => sendRevision(true)}>
          {moreQuestionsPending ? (
            <>
              <Icon name="progress_activity" size={14} className="animate-spin" />
              {t('builder.page.plan.backToQuestionsSending')}
            </>
          ) : (
            <>
              <Icon name="quiz" size={14} />
              {t('builder.page.plan.backToQuestions')}
            </>
          )}
        </Button>
      </div>
      {plan && (
        <p className="text-[11.5px]" style={{ color: 'var(--text-tertiary)' }}>
          {t('builder.page.plan.approveHint')}
        </p>
      )}
    </section>
  )
}

/** A plan the user sent back, in the history: changes asked in words, or more questions first. */
export function PlanRevisionNote({ moreQuestions }: { moreQuestions: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="flex justify-end">
      <span
        className="inline-flex max-w-[92%] items-center gap-1.5 rounded-[10px] border px-2.5 py-1.5 text-[12px]"
        style={{ borderColor: 'var(--border)', background: 'var(--hover-surface)', color: 'var(--text-secondary)' }}
      >
        <Icon name={moreQuestions ? 'quiz' : 'edit_note'} size={14} />
        {t(moreQuestions ? 'builder.page.plan.moreQuestionsNote' : 'builder.page.plan.revisionNote')}
      </span>
    </div>
  )
}

/**
 * An approved plan in the history, read-only: the version, the milestone the build started on and
 * the plan itself behind a disclosure. The plan comes from the turn's `proposal` event.
 */
export function ApprovedPlanBlock({ approved }: { approved: ApprovedPlan }) {
  const { t } = useTranslation()
  const { plan, version, milestone } = approved
  const started = plan && milestone ? plan.milestones[milestone - 1] : undefined

  return (
    <div className="flex flex-col items-end gap-1">
      <span className="px-1 text-[11px] font-medium" style={{ color: 'var(--text-tertiary)' }}>
        {t('builder.page.chat.you')}
      </span>
      <div
        className="flex w-full max-w-[92%] flex-col gap-2 rounded-[12px] border px-3.5 py-2.5 text-[12.5px]"
        style={{ borderColor: 'var(--border)', background: 'var(--hover-surface)', color: 'var(--text-primary)' }}
      >
        <div className="flex items-center gap-1.5 font-semibold">
          <Icon name="task_alt" size={14} style={{ color: 'var(--success-text)' }} />
          {version ? t('builder.page.plan.approvedTitle', { version }) : t('builder.page.plan.approvedTitleNoVersion')}
        </div>
        {plan && started && milestone && (
          <div className="break-words" style={{ color: 'var(--text-secondary)' }}>
            {t('builder.page.plan.approvedStarted', { n: milestone, total: plan.milestones.length, title: started.title })}
          </div>
        )}
        {plan && (
          <details className="group">
            <summary
              className="cursor-pointer select-none rounded-[6px] text-[12px] font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              style={{ color: 'var(--text-secondary)' }}
            >
              {t('builder.page.plan.showPlan')}
            </summary>
            <div className="pt-2">
              <PlanBody plan={plan} />
            </div>
          </details>
        )}
      </div>
    </div>
  )
}
