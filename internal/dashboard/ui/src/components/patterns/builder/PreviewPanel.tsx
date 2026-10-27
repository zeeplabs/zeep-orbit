import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { EmptyState, ErrorState } from '@/components/patterns/states'
import { StatusPill } from '@/components/patterns/StatusPill'
import { useBuilderPreviewToken } from '@/lib/api'
import { isSameOriginPath, type BuildSummary } from './builderModel'

/**
 * The preview runs LLM-generated code. It must stay in an opaque origin so it can never call
 * the dashboard API as the signed-in user (design section 10). Never grant the sandbox its
 * origin back: the token list below is closed on purpose.
 */
export const PREVIEW_SANDBOX = 'allow-scripts allow-forms allow-popups allow-modals'

/** Mint a new preview token this long before the current one (10 minutes) expires. */
const TOKEN_REFRESH_MARGIN_MS = 60_000
/** A cached token with less than this left is not reused for a (re)load. */
const TOKEN_MIN_REMAINING_MS = 90_000
/** Used only if the server's expires_at cannot be parsed. */
const TOKEN_FALLBACK_TTL_MS = 9 * 60_000

interface PreviewToken {
  url: string
  expiresAt: number
}

interface PreviewPanelProps {
  sessionId: string
  archived: boolean
  build: BuildSummary
  /** The event log has not loaded yet, so `build` is not meaningful. */
  eventsLoading: boolean
}

export function PreviewPanel({ sessionId, archived, build, eventsLoading }: PreviewPanelProps) {
  const { t } = useTranslation()
  const { mutateAsync: mintToken } = useBuilderPreviewToken(sessionId)

  const mintRef = useRef(mintToken)
  useEffect(() => {
    mintRef.current = mintToken
  }, [mintToken])

  const tokenRef = useRef<PreviewToken | null>(null)
  const loadSeq = useRef(0)
  const [expiresAt, setExpiresAt] = useState<number | null>(null)
  const [phase, setPhase] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle')
  // `key` remounts the iframe, which is what forces a reload even when the src is unchanged.
  const [frame, setFrame] = useState<{ src: string; key: number } | null>(null)
  const [loaded, setLoaded] = useState(false)

  const ensureToken = useCallback(async (): Promise<PreviewToken> => {
    const cached = tokenRef.current
    if (cached && cached.expiresAt - Date.now() > TOKEN_MIN_REMAINING_MS) return cached
    const res = await mintRef.current()
    // The server answers a same-origin path; refuse anything else as an iframe src.
    if (!isSameOriginPath(res.url)) throw new Error('unexpected preview url')
    const parsed = Date.parse(res.expires_at)
    const token = {
      url: res.url,
      expiresAt: Number.isNaN(parsed) ? Date.now() + TOKEN_FALLBACK_TTL_MS : parsed,
    }
    tokenRef.current = token
    setExpiresAt(token.expiresAt)
    return token
  }, [])

  const load = useCallback(async () => {
    const seq = ++loadSeq.current
    setPhase('loading')
    try {
      const token = await ensureToken()
      if (seq !== loadSeq.current) return
      setFrame((prev) => ({ src: token.url, key: (prev?.key ?? 0) + 1 }))
      setLoaded(false)
      setPhase('ready')
    } catch {
      // useBuilderPreviewToken already toasted a server failure.
      if (seq === loadSeq.current) setPhase('error')
    }
  }, [ensureToken])

  // Load on the first successful build and reload on every new one. build.reloadId is the id of
  // the newest build event, so a poll that brings nothing new does not touch the iframe.
  const autoLoad = !archived && !eventsLoading && build.reloadId > 0
  useEffect(() => {
    if (autoLoad) void load()
  }, [autoLoad, build.reloadId, load])

  // Keep a fresh token ready so a reload never waits on (or fails with) an expired one. This
  // does not navigate the iframe, so the running preview keeps its state.
  useEffect(() => {
    if (expiresAt === null || archived) return
    const delay = Math.max(5_000, expiresAt - Date.now() - TOKEN_REFRESH_MARGIN_MS)
    const id = window.setTimeout(() => {
      tokenRef.current = null
      // A hidden tab does not need a token; the next (re)load mints one.
      if (document.visibilityState !== 'visible') {
        setExpiresAt(null)
        return
      }
      ensureToken().catch(() => setExpiresAt(null))
    }, delay)
    return () => window.clearTimeout(id)
  }, [expiresAt, archived, ensureToken])

  const visibleFrame = !archived && phase !== 'error' ? frame : null
  const showFrame = visibleFrame !== null
  const showOverlay = phase === 'loading' || (showFrame && !loaded)
  // Between the render that sees the first build and the effect that starts loading it.
  const starting = autoLoad && phase === 'idle'

  return (
    <section
      aria-label={t('builder.page.preview.title')}
      className="flex h-[65vh] min-h-[420px] flex-col overflow-hidden rounded-[14px] border lg:h-full"
      style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
    >
      <div className="flex items-center gap-2 border-b px-3 py-2" style={{ borderColor: 'var(--border)' }}>
        <Icon name="preview" size={16} style={{ color: 'var(--text-tertiary)' }} />
        <h2 className="text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
          {t('builder.page.preview.title')}
        </h2>
        {build.state === 'building' && (
          <StatusPill label={t('builder.page.preview.building')} tone="primary" />
        )}
        {build.state === 'failed' && (
          <StatusPill label={t('builder.page.preview.buildFailedPill')} tone="danger" />
        )}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="ml-auto gap-1.5"
          disabled={archived || phase === 'loading'}
          onClick={() => void load()}
        >
          <Icon name="refresh" size={15} />
          {t('builder.page.preview.reload')}
        </Button>
      </div>

      {build.state === 'failed' && !archived && (
        <div
          className="flex items-start gap-2 px-3 py-2 text-[12.5px]"
          style={{ background: 'var(--danger-tint)', color: 'var(--danger-text)' }}
        >
          <Icon name="error" size={15} className="mt-[1px] shrink-0" />
          {showFrame ? t('builder.page.preview.buildFailed') : t('builder.page.preview.buildFailedNoPreview')}
        </div>
      )}

      <div className="relative min-h-0 flex-1">
        {archived ? (
          <EmptyState icon="inventory_2" title={t('builder.page.preview.archived')} />
        ) : phase === 'error' ? (
          <ErrorState
            title={t('builder.page.preview.error')}
            action={{ label: t('builder.page.retry'), onClick: () => void load() }}
          />
        ) : visibleFrame ? (
          <iframe
            key={visibleFrame.key}
            src={visibleFrame.src}
            title={t('builder.page.preview.frameTitle')}
            sandbox={PREVIEW_SANDBOX}
            referrerPolicy="no-referrer"
            onLoad={() => setLoaded(true)}
            className="absolute inset-0 h-full w-full border-0 bg-white"
          />
        ) : !eventsLoading && phase !== 'loading' && !starting ? (
          <EmptyState
            icon="web"
            title={t('builder.page.preview.emptyTitle')}
            description={t('builder.page.preview.emptyDesc')}
            action={{ label: t('builder.page.preview.load'), onClick: () => void load() }}
          />
        ) : null}

        {!archived && (showOverlay || (eventsLoading && !showFrame)) && (
          <div
            role="status"
            className="pointer-events-none absolute inset-0 flex items-center justify-center gap-2 text-[13px]"
            style={{ background: 'color-mix(in srgb, var(--surface) 70%, transparent)', color: 'var(--text-secondary)' }}
          >
            <Icon name="progress_activity" size={16} className="animate-spin" />
            {t('builder.page.preview.loading')}
          </div>
        )}
      </div>
    </section>
  )
}
