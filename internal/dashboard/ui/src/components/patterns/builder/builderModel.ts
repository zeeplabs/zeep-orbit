import type { BuilderEvent, BuilderProposal, BuilderTurn } from '@/lib/api'

// Pure view-model helpers of the AI builder page (ai-frontend-builder T36/T37). No React and no
// i18n here: entries carry an i18n key + params and components call t(). Every `type` the
// server may add later is ignored (the switches have no throwing default).

export type ActivityTone = 'neutral' | 'success' | 'danger' | 'warning'

export interface ActivityEntry {
  id: number
  icon: string
  tone: ActivityTone
  /** Key below `builder.page.activity.` */
  labelKey: string
  params?: Record<string, string | number>
  /** Plain text only (build output tail). Never rendered as HTML. */
  detail?: string
}

export type TurnSegment =
  | { kind: 'message'; id: number; content: string }
  | { kind: 'activity'; id: number; entries: ActivityEntry[] }

export interface TurnView {
  turn: BuilderTurn
  segments: TurnSegment[]
  /** limit_reached payload.limit of the turn, when it ended on a limit. */
  limit?: string
  /** turn.error_code, falling back to the turn's last `error` event. */
  errorCode: string | null
}

const DETAIL_MAX_CHARS = 2000

function tail(text: string | undefined): string | undefined {
  if (!text) return undefined
  return text.length > DETAIL_MAX_CHARS ? text.slice(text.length - DETAIL_MAX_CHARS) : text
}

function fileLabel(op: string | undefined, path: string | undefined): Pick<ActivityEntry, 'labelKey' | 'params' | 'icon'> {
  const p = path ? { path } : undefined
  switch (op) {
    case 'delete':
    case 'remove':
      return p
        ? { labelKey: 'fileDeleted', params: p, icon: 'delete' }
        : { labelKey: 'fileDeletedNoPath', icon: 'delete' }
    case 'create':
    case 'add':
    case 'new':
      return p
        ? { labelKey: 'fileCreated', params: p, icon: 'note_add' }
        : { labelKey: 'fileCreatedNoPath', icon: 'note_add' }
    default:
      return p
        ? { labelKey: 'fileEdited', params: p, icon: 'edit_document' }
        : { labelKey: 'fileEditedNoPath', icon: 'edit_document' }
  }
}

/** Turns an event into a timeline entry; null for events that are not timeline material. */
function toEntry(event: BuilderEvent, fixAttempt: number): ActivityEntry | null {
  const base = { id: event.id }
  switch (event.type) {
    case 'queued':
      return { ...base, icon: 'schedule', tone: 'neutral', labelKey: 'queued' }
    case 'turn_started':
      return event.payload.resumed
        ? { ...base, icon: 'play_arrow', tone: 'neutral', labelKey: 'resumed' }
        : null
    case 'tool_call': {
      const what = event.payload.summary || event.payload.name
      return what
        ? { ...base, icon: 'build', tone: 'neutral', labelKey: 'toolCall', params: { what } }
        : { ...base, icon: 'build', tone: 'neutral', labelKey: 'toolCallGeneric' }
    }
    case 'file_changed':
      return { ...base, tone: 'neutral', ...fileLabel(event.payload.op, event.payload.path) }
    case 'skill_loaded':
      return event.payload.name
        ? { ...base, icon: 'menu_book', tone: 'neutral', labelKey: 'skillLoaded', params: { name: event.payload.name } }
        : { ...base, icon: 'menu_book', tone: 'neutral', labelKey: 'skillLoadedGeneric' }
    case 'build_started':
      return fixAttempt > 0
        ? { ...base, icon: 'autorenew', tone: 'warning', labelKey: 'buildFixing', params: { n: fixAttempt } }
        : { ...base, icon: 'deployed_code', tone: 'neutral', labelKey: 'buildStarted' }
    case 'build_succeeded':
      return { ...base, icon: 'check_circle', tone: 'success', labelKey: 'buildSucceeded' }
    case 'build_failed':
      return {
        ...base,
        icon: 'error',
        tone: 'danger',
        labelKey: 'buildFailed',
        detail: tail(event.payload.tail),
      }
    case 'proposal':
      return { ...base, icon: 'rule', tone: 'warning', labelKey: 'proposal' }
    case 'proposal_confirmed':
      return {
        ...base,
        icon: 'check_circle',
        tone: 'success',
        labelKey: event.payload.applied ? 'proposalApplied' : 'proposalConfirmed',
      }
    case 'proposal_rejected':
      return { ...base, icon: 'cancel', tone: 'neutral', labelKey: 'proposalRejected' }
    case 'revert': {
      const seq = event.payload.to_turn_seq
      if (event.payload.changed === false) {
        return { ...base, icon: 'history', tone: 'neutral', labelKey: 'revertNoChange' }
      }
      return seq === 0
        ? { ...base, icon: 'history', tone: 'neutral', labelKey: 'revertTemplate' }
        : { ...base, icon: 'history', tone: 'neutral', labelKey: 'revert', params: { seq: seq ?? 0 } }
    }
    default:
      return null
  }
}

/**
 * Groups the event log by turn and orders it: turns by seq, events by id. Assistant messages
 * become chat bubbles; everything else becomes timeline entries. Consecutive entries share a
 * segment so the timeline stays compact between two messages. Events without a turn (publish
 * events) or for a turn that is not loaded are skipped here.
 */
export function buildTurnViews(turns: BuilderTurn[], events: BuilderEvent[]): TurnView[] {
  const byTurn = new Map<string, BuilderEvent[]>()
  for (const event of events) {
    if (!event.turn_id) continue
    const list = byTurn.get(event.turn_id)
    if (list) list.push(event)
    else byTurn.set(event.turn_id, [event])
  }

  return [...turns]
    .sort((a, b) => a.seq - b.seq)
    .map((turn) => {
      const list = (byTurn.get(turn.id) ?? []).sort((a, b) => a.id - b.id)
      const segments: TurnSegment[] = []
      let limit: string | undefined
      let eventError: string | undefined
      let failedBuilds = 0

      for (const event of list) {
        if (event.type === 'assistant_message') {
          const content = event.payload.content
          if (content && content.trim()) segments.push({ kind: 'message', id: event.id, content })
          continue
        }
        if (event.type === 'limit_reached' && event.payload.limit) limit = event.payload.limit
        if (event.type === 'error' && event.payload.code) eventError = event.payload.code
        if (event.type === 'build_failed') failedBuilds += 1

        const entry = toEntry(event, event.type === 'build_started' ? failedBuilds : 0)
        if (!entry) continue
        const last = segments[segments.length - 1]
        if (last && last.kind === 'activity') last.entries.push(entry)
        else segments.push({ kind: 'activity', id: entry.id, entries: [entry] })
      }

      return { turn, segments, limit, errorCode: turn.error_code ?? eventError ?? null }
    })
}

export type BuildState = 'none' | 'building' | 'succeeded' | 'failed'

export interface BuildSummary {
  state: BuildState
  /**
   * Id of the newest event that produced a new successful build (build_succeeded, or a revert
   * that rebuilt successfully); 0 when there is none. The preview reloads when it changes, so it
   * reloads once per build and never per poll.
   */
  reloadId: number
}

export function summarizeBuild(events: BuilderEvent[]): BuildSummary {
  let state: BuildState = 'none'
  let reloadId = 0
  for (const event of events) {
    switch (event.type) {
      case 'build_started':
        state = 'building'
        break
      case 'build_succeeded':
        state = 'succeeded'
        reloadId = Math.max(reloadId, event.id)
        break
      case 'build_failed':
        state = 'failed'
        break
      case 'revert':
        if (event.payload.build === 'succeeded') {
          state = 'succeeded'
          reloadId = Math.max(reloadId, event.id)
        } else if (event.payload.build === 'failed') {
          state = 'failed'
        }
        break
      default:
        break
    }
  }
  return { state, reloadId }
}

export type PublishEvent = Extract<
  BuilderEvent,
  { type: 'publish_started' | 'publish_succeeded' | 'publish_failed' }
>

/** The newest publish_* event, or undefined. */
export function latestPublishEvent(events: BuilderEvent[]): PublishEvent | undefined {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i]
    if (e.type === 'publish_started' || e.type === 'publish_succeeded' || e.type === 'publish_failed') {
      return e
    }
  }
  return undefined
}

// -- Proposal operation (schema names only, shape under each kind is not fixed) --

export interface ProposalSummary {
  /** Key below `builder.page.proposal.op.` */
  key: string
  params: Record<string, string>
  /** Extra lines, e.g. the columns of a new table. */
  lines: string[]
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' ? (value as Record<string, unknown>) : {}
}

function columnNames(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map((c) => str(record(c).name)).filter(Boolean)
}

/** Reads propose_backend_change arguments defensively: any field may be missing or mistyped. */
export function summarizeProposal(proposal: BuilderProposal): ProposalSummary {
  const op = record(record(proposal.operation).operation)
  const kind = str(op.kind)
  const body = record(op[kind])
  switch (kind) {
    case 'add_table': {
      const cols = Array.isArray(body.columns)
        ? body.columns.map((c) => {
            const col = record(c)
            return `${str(col.name)} (${str(col.type)})`
          })
        : []
      return { key: 'addTable', params: { table: str(body.name) }, lines: cols }
    }
    case 'add_column': {
      const col = record(body.column)
      const allowed = Array.isArray(col.allowed_values)
        ? col.allowed_values.filter((v): v is string => typeof v === 'string')
        : []
      return {
        key: 'addColumn',
        params: { table: str(body.table), column: str(col.name), type: str(col.type) },
        lines: allowed,
      }
    }
    case 'add_index':
      return {
        key: 'addIndex',
        params: { table: str(body.table), name: str(body.name), columns: columnNamesOrStrings(body.columns) },
        lines: [],
      }
    case 'add_reference': {
      const col = record(body.column)
      return {
        key: 'addReference',
        params: {
          table: str(body.table),
          column: str(col.name),
          refTable: str(body.ref_table),
          refColumn: str(body.ref_column),
        },
        lines: [],
      }
    }
    case 'add_foreign_key':
      return {
        key: 'addForeignKey',
        params: {
          table: str(body.table),
          column: str(body.column),
          refTable: str(body.ref_table),
          refColumn: str(body.ref_column),
        },
        lines: [],
      }
    case 'remove_foreign_key':
      return { key: 'removeForeignKey', params: { table: str(body.table), column: str(body.column) }, lines: [] }
    case 'set_rls_mode':
      return { key: 'setRlsMode', params: { table: str(body.table), mode: str(body.mode) }, lines: [] }
    case 'toggle_auth':
      return { key: body.email_enabled ? 'enableAuth' : 'disableAuth', params: {}, lines: [] }
    default:
      return { key: 'unknown', params: { kind: kind || '?' }, lines: [] }
  }
}

function columnNamesOrStrings(value: unknown): string {
  if (!Array.isArray(value)) return ''
  const strings = value.filter((v): v is string => typeof v === 'string')
  return (strings.length > 0 ? strings : columnNames(value)).join(', ')
}

/** Only https URLs from server data become links (never javascript:/data:). */
export function safeHttpsUrl(url: string | undefined | null): string | null {
  if (!url) return null
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' ? parsed.toString() : null
  } catch {
    return null
  }
}

/** True for an iframe src that is a same-origin absolute path (the preview-token `url`). */
export function isSameOriginPath(url: string): boolean {
  return url.startsWith('/') && !url.startsWith('//') && !url.includes('\\')
}

export const TURN_ERROR_CODES = [
  'provider_unavailable',
  'context_too_long',
  'workspace_lost',
  'tool_failed',
  'commit_failed',
  'internal_error',
  'unsupported_turn_kind',
  'revert_failed',
] as const

export const TURN_LIMITS = ['max_steps', 'max_fix_iterations', 'max_turn_duration'] as const

export const PUBLISH_REASONS = [
  'github_not_configured',
  'deploy_not_configured',
  'template_not_deployable',
  'template_unavailable',
  'nothing_to_publish',
  'turn_in_progress',
  'publish_in_progress',
  'remote_moved',
] as const

export const UNAVAILABLE_REASONS = [
  'not_configured',
  'disabled',
  'provider_not_configured',
  'budget_not_configured',
  'builder_unreachable',
] as const

/** Maps a server code to a known i18n suffix, or `fallback` for an unknown/new code. */
export function knownCode<T extends readonly string[]>(
  code: string | null | undefined,
  known: T,
  fallback: string,
): string {
  return code && (known as readonly string[]).includes(code) ? code : fallback
}
