import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import {
  Drawer,
  DrawerBody,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from '@/components/ui/drawer'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ErrorState, LoadingState } from '@/components/patterns/states'
import {
  useBuilderBrief,
  useBuilderBriefVersion,
  type BuilderBrief,
  type BuilderPlan,
  type BuilderSessionDetail,
} from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { PlanBody } from './PlanCard'
import { parsePlan } from './builderModel'

/**
 * "Brief" button of the session header and its drawer: the approved plan, its milestone progress and
 * the stored versions, read-only. Changing the brief is done by asking the agent. Nothing is shown
 * for a session without a brief (old sessions, planning off).
 */
export function BriefButton({ sessionId, session }: { sessionId: string; session: BuilderSessionDetail }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  if (!session.brief_version) return null
  return (
    <BriefDrawer sessionId={sessionId} session={session} open={open} onOpenChange={setOpen}>
      {/* A Radix trigger, so closing the drawer hands focus back to this button. */}
      <Button type="button" variant="outline" className="gap-2">
        <Icon name="description" size={16} />
        {t('builder.page.brief.button', { done: session.milestones_done, total: session.milestones_total })}
      </Button>
    </BriefDrawer>
  )
}

/** The current brief's plan with the done marks of the server's progress, not the plan's own. */
function withProgress(plan: BuilderPlan, brief: BuilderBrief): BuilderPlan {
  const done = new Map(brief.milestones.map((m) => [m.index, m.done]))
  return { ...plan, milestones: plan.milestones.map((m, i) => ({ ...m, done: done.get(i + 1) ?? m.done })) }
}

function BriefDrawer({
  sessionId,
  session,
  open,
  onOpenChange,
  children,
}: {
  sessionId: string
  session: BuilderSessionDetail
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The button that opens the drawer. */
  children: ReactNode
}) {
  const { t } = useTranslation()
  const briefQuery = useBuilderBrief(sessionId, open)
  // null = the current version; a number = an older stored version.
  const [viewing, setViewing] = useState<number | null>(null)
  const versionQuery = useBuilderBriefVersion(sessionId, viewing)

  // The session detail polls; refetch the brief when its version or progress moves while open.
  // Opening fetches on its own (the query is enabled then, and stale).
  const { refetch } = briefQuery
  const progressKey = `${session.brief_version}:${session.milestones_done}`
  const seenProgress = useRef(progressKey)
  useEffect(() => {
    if (seenProgress.current === progressKey) return
    seenProgress.current = progressKey
    if (open) void refetch()
  }, [progressKey, open, refetch])

  const brief = briefQuery.data
  const current = useMemo(() => {
    const plan = brief ? parsePlan(brief.plan) : null
    return plan && brief ? withProgress(plan, brief) : null
  }, [brief])
  const older = useMemo(() => (versionQuery.data ? parsePlan(versionQuery.data.plan) : null), [versionQuery.data])

  const showOlder = viewing !== null && brief !== undefined && viewing !== brief.version
  const close = (next: boolean) => {
    onOpenChange(next)
    if (!next) setViewing(null)
  }

  return (
    <Drawer open={open} onOpenChange={close}>
      <DrawerTrigger asChild>{children}</DrawerTrigger>
      <DrawerContent width={560}>
        <DrawerHeader className="pr-12">
          <DrawerTitle>{t('builder.page.brief.title')}</DrawerTitle>
          <DrawerDescription>{t('builder.page.brief.description')}</DrawerDescription>
        </DrawerHeader>
        <DrawerBody className="flex flex-col gap-4">
          {briefQuery.isLoading && <LoadingState rows={4} />}
          {briefQuery.isError && (
            <ErrorState
              title={t('builder.page.brief.loadError')}
              action={{ label: t('builder.page.retry'), onClick: () => void briefQuery.refetch() }}
            />
          )}
          {brief && brief.version === 0 && (
            <p className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
              {t('builder.page.brief.none')}
            </p>
          )}
          {brief && brief.version > 0 && (
            <>
              <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1 text-[12.5px]" style={{ color: 'var(--text-secondary)' }}>
                  <span className="font-semibold" style={{ color: 'var(--text-primary)' }}>
                    {t('builder.page.brief.progress', {
                      done: brief.milestones.filter((m) => m.done).length,
                      total: brief.milestones.length,
                    })}
                  </span>
                  {brief.created_at && (
                    <span>{t('builder.page.brief.approvedAt', { version: brief.version, date: formatDateTime(brief.created_at) })}</span>
                  )}
                </div>
                {brief.versions.length > 1 && (
                  <div className="flex flex-col gap-1">
                    <label htmlFor={`brief-version-${sessionId}`} className="text-[12px] font-semibold" style={{ color: 'var(--text-secondary)' }}>
                      {t('builder.page.brief.versionLabel')}
                    </label>
                    <Select
                      value={String(viewing ?? brief.version)}
                      onValueChange={(v) => setViewing(Number(v) === brief.version ? null : Number(v))}
                    >
                      <SelectTrigger id={`brief-version-${sessionId}`} className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {brief.versions.map((v) => (
                          <SelectItem key={v.version} value={String(v.version)}>
                            {t(v.version === brief.version ? 'builder.page.brief.versionCurrent' : 'builder.page.brief.versionOption', {
                              version: v.version,
                              date: formatDateTime(v.created_at),
                            })}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}
              </div>

              {showOlder ? (
                <>
                  <div
                    role="status"
                    className="flex items-start gap-2 rounded-[10px] px-3 py-2 text-[12.5px]"
                    style={{ background: 'var(--hover-surface)', color: 'var(--text-secondary)' }}
                  >
                    <Icon name="history" size={16} className="mt-[1px] shrink-0" />
                    <span>{t('builder.page.brief.olderNotice', { version: viewing, current: brief.version })}</span>
                  </div>
                  {versionQuery.isLoading && <LoadingState rows={4} />}
                  {versionQuery.isError && (
                    <ErrorState
                      title={t('builder.page.brief.loadError')}
                      action={{ label: t('builder.page.retry'), onClick: () => void versionQuery.refetch() }}
                    />
                  )}
                  {versionQuery.data &&
                    (older ? <PlanBody plan={older} /> : <Unreadable />)}
                </>
              ) : current ? (
                <PlanBody plan={current} />
              ) : (
                <Unreadable />
              )}

              <p className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                {t('builder.page.brief.changeHint')}
              </p>
            </>
          )}
        </DrawerBody>
      </DrawerContent>
    </Drawer>
  )
}

function Unreadable() {
  const { t } = useTranslation()
  return (
    <p className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
      {t('builder.page.brief.unreadable')}
    </p>
  )
}
