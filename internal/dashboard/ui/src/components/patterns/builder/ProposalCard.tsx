import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import {
  useConfirmBuilderTurn,
  useRejectBuilderTurn,
  type BuilderProposal,
  type BuilderTurn,
} from '@/lib/api'
import { summarizePlan, summarizeProposal, type PlanOperationSummary, type ProposalSummary } from './builderModel'

interface ProposalCardProps {
  sessionId: string
  turn: BuilderTurn
  proposal: BuilderProposal
  /** The builder cannot take actions right now (unavailable / archived). */
  disabled?: boolean
}

/** One backend change: what it does, plus its extra lines (e.g. the columns of a new table). */
function OperationLines({ summary }: { summary: ProposalSummary }) {
  const { t } = useTranslation()
  return (
    <>
      <div className="break-words font-medium">{t(`builder.page.proposal.op.${summary.key}`, summary.params)}</div>
      {summary.lines.length > 0 && (
        <ul className="flex flex-col gap-1">
          {summary.lines.map((line, i) => (
            <li
              key={`${line}-${i}`}
              className="break-words rounded-[8px] px-2.5 py-1 text-[12px]"
              style={{ background: 'var(--hover-surface)', color: 'var(--text-secondary)' }}
            >
              {line}
            </li>
          ))}
        </ul>
      )}
    </>
  )
}

/**
 * The access the agent declared for a new table, in plain words, with its reason. A public table
 * is the one decision the user must not miss, so it is the only one drawn as a warning.
 */
function AccessLine({ op }: { op: PlanOperationSummary }) {
  const { t } = useTranslation()
  const isPublic = op.access === 'public'
  const tone = isPublic || !op.access ? 'var(--warning-text)' : 'var(--text-secondary)'
  return (
    <div className="flex flex-col gap-0.5 text-[12px]" style={{ color: 'var(--text-secondary)' }}>
      <div className="flex items-center gap-1.5 font-semibold" style={{ color: tone }}>
        <Icon name={isPublic ? 'public' : op.access ? 'lock' : 'help'} size={14} />
        <span>
          {t('builder.page.proposal.accessLabel')}: {t(`builder.page.proposal.access.${op.access ?? 'unknown'}`)}
        </span>
      </div>
      {isPublic && <span>{t('builder.page.proposal.publicWarning')}</span>}
      {op.accessReason && <span className="break-words">{op.accessReason}</span>}
    </div>
  )
}

/**
 * A turn paused on propose_backend_change or propose_backend_plan. Nothing touches the backend
 * app until the user confirms. The proposal carries schema names only, rendered as plain text.
 */
export function ProposalCard({ sessionId, turn, proposal, disabled }: ProposalCardProps) {
  const { t } = useTranslation()
  const confirm = useConfirmBuilderTurn(sessionId)
  const reject = useRejectBuilderTurn(sessionId)
  const busy = confirm.isPending || reject.isPending
  const plan = summarizePlan(proposal)

  return (
    <section
      aria-labelledby={`proposal-${turn.id}`}
      className="flex flex-col gap-3 rounded-[14px] border p-4"
      style={{ background: 'var(--surface)', borderColor: 'var(--primary)' }}
    >
      <div className="flex items-center gap-2">
        <Icon name="rule" size={16} style={{ color: 'var(--primary)' }} />
        <h3 id={`proposal-${turn.id}`} className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
          {t(plan ? 'builder.page.proposal.planTitle' : 'builder.page.proposal.title')}
        </h3>
      </div>
      <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
        {t(plan ? 'builder.page.proposal.planDescription' : 'builder.page.proposal.description')}
      </p>
      {plan ? (
        <div className="flex flex-col gap-2.5 text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
          {plan.summary && <p className="break-words">{plan.summary}</p>}
          <ol className="flex flex-col gap-2">
            {plan.operations.map((op, i) => (
              <li
                key={i}
                className="flex gap-2.5 rounded-[10px] border p-2.5"
                style={{ borderColor: 'var(--border)' }}
              >
                <span className="shrink-0 font-semibold tabular-nums" style={{ color: 'var(--text-secondary)' }}>
                  {i + 1}.
                </span>
                <div className="flex min-w-0 flex-col gap-1.5">
                  <OperationLines summary={op} />
                  {op.isTable && <AccessLine op={op} />}
                </div>
              </li>
            ))}
          </ol>
        </div>
      ) : (
        <div className="flex flex-col gap-1.5 text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
          <OperationLines summary={summarizeProposal(proposal)} />
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          className="gap-2"
          disabled={busy || disabled}
          onClick={() => confirm.mutate(turn.id)}
        >
          {confirm.isPending ? (
            <>
              <Icon name="progress_activity" size={14} className="animate-spin" />
              {t('builder.page.proposal.confirming')}
            </>
          ) : (
            <>
              <Icon name="check" size={14} />
              {t('builder.page.proposal.confirm')}
            </>
          )}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="gap-2"
          disabled={busy || disabled}
          onClick={() => reject.mutate(turn.id)}
        >
          {reject.isPending ? (
            <>
              <Icon name="progress_activity" size={14} className="animate-spin" />
              {t('builder.page.proposal.rejecting')}
            </>
          ) : (
            <>
              <Icon name="close" size={14} />
              {t('builder.page.proposal.reject')}
            </>
          )}
        </Button>
      </div>
    </section>
  )
}
