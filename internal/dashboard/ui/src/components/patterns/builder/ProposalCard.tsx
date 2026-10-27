import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import {
  useConfirmBuilderTurn,
  useRejectBuilderTurn,
  type BuilderProposal,
  type BuilderTurn,
} from '@/lib/api'
import { summarizeProposal } from './builderModel'

interface ProposalCardProps {
  sessionId: string
  turn: BuilderTurn
  proposal: BuilderProposal
  /** The builder cannot take actions right now (unavailable / archived). */
  disabled?: boolean
}

/**
 * A turn paused on propose_backend_change. Nothing touches the backend app until the user
 * confirms. The proposal carries schema names only, rendered as plain text.
 */
export function ProposalCard({ sessionId, turn, proposal, disabled }: ProposalCardProps) {
  const { t } = useTranslation()
  const confirm = useConfirmBuilderTurn(sessionId)
  const reject = useRejectBuilderTurn(sessionId)
  const busy = confirm.isPending || reject.isPending
  const summary = summarizeProposal(proposal)

  return (
    <section
      aria-labelledby={`proposal-${turn.id}`}
      className="flex flex-col gap-3 rounded-[14px] border p-4"
      style={{ background: 'var(--surface)', borderColor: 'var(--primary)' }}
    >
      <div className="flex items-center gap-2">
        <Icon name="rule" size={16} style={{ color: 'var(--primary)' }} />
        <h3 id={`proposal-${turn.id}`} className="text-[13px] font-bold" style={{ color: 'var(--text-primary)' }}>
          {t('builder.page.proposal.title')}
        </h3>
      </div>
      <p className="text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
        {t('builder.page.proposal.description')}
      </p>
      <div className="flex flex-col gap-1.5 text-[12.5px]" style={{ color: 'var(--text-primary)' }}>
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
      </div>
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
