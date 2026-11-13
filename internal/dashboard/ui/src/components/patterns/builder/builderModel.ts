import {
  BUILDER_ASK_USER,
  BUILDER_PROPOSE_BUILD_PLAN,
  type BuilderEvent,
  type BuilderPlan,
  type BuilderProposal,
  type BuilderQuestion,
  type BuilderTurn,
} from '@/lib/api'

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

/**
 * An ask_user round the user already answered, rebuilt from the `proposal` event (the questions)
 * and the `questions_answered` event (the answers).
 */
export interface AnsweredRound {
  /** Empty when the questions could not be read; the answers are then listed by question id. */
  questions: BuilderQuestion[]
  /** Question id to the answer as the user gave it; '' is left to the assistant ("decide for me" or blank). */
  answers: Record<string, string>
  /** The user chose "Start building now". */
  skipped: boolean
}

/** A build plan the user approved, rebuilt from the `proposal` event and the `plan_approved` event. */
export interface ApprovedPlan {
  /** Null when the plan could not be read; the block then shows the version only. */
  plan: BuilderPlan | null
  version?: number
  /** The milestone the build started on (1-based). */
  milestone?: number
}

export type TurnSegment =
  | { kind: 'message'; id: number; content: string }
  | { kind: 'activity'; id: number; entries: ActivityEntry[] }
  | { kind: 'questions'; id: number; round: AnsweredRound }
  | { kind: 'plan'; id: number; approved: ApprovedPlan }
  /** The user sent a plan back. Their words are chat content, not in the event: only what they asked for. */
  | { kind: 'revision'; id: number; moreQuestions: boolean }

const PLANNING_TOOLS: readonly string[] = [BUILDER_ASK_USER, BUILDER_PROPOSE_BUILD_PLAN]

export interface TurnView {
  turn: BuilderTurn
  segments: TurnSegment[]
  /** limit_reached payload.limit of the turn, when it ended on a limit. */
  limit?: string
  /** budget_exceeded payload.reason of the turn: the cap that stopped it. */
  budgetReason?: string
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
      // A skill load has its own row (skill_loaded, with the skill's name); the raw call adds noise.
      if (event.payload.name === 'load_skill') return null
      // Same for a backend proposal: the `proposal` row that follows says it in words.
      if (event.payload.name === 'propose_backend_change' || event.payload.name === 'propose_backend_plan') return null
      // Planning calls are shown as their cards and, once answered, as read-only blocks.
      if (event.payload.name && PLANNING_TOOLS.includes(event.payload.name)) return null
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
      if (event.payload.name && PLANNING_TOOLS.includes(event.payload.name)) return null
      return { ...base, icon: 'rule', tone: 'warning', labelKey: 'proposal' }
    case 'proposal_confirmed': {
      // A confirmed plan reports how far it got: it stops at the first operation that fails.
      const { applied_count: appliedCount, total } = event.payload
      if (event.payload.kind === 'plan' && typeof appliedCount === 'number' && typeof total === 'number') {
        const complete = appliedCount >= total
        return {
          ...base,
          icon: complete ? 'check_circle' : 'error',
          tone: complete ? 'success' : 'danger',
          labelKey: complete ? 'planApplied' : 'planPartial',
          params: { applied: appliedCount, total },
        }
      }
      return {
        ...base,
        icon: 'check_circle',
        tone: 'success',
        labelKey: event.payload.applied ? 'proposalApplied' : 'proposalConfirmed',
      }
    }
    case 'proposal_rejected':
      return { ...base, icon: 'cancel', tone: 'neutral', labelKey: 'proposalRejected' }
    case 'budget_warning': {
      const { used, limit } = event.payload
      const cap = knownCode(event.payload.cap, BUDGET_CAPS, 'unknown')
      const percent =
        typeof used === 'number' && typeof limit === 'number' && limit > 0 ? Math.floor((used * 100) / limit) : 80
      return { ...base, icon: 'savings', tone: 'warning', labelKey: `budgetWarning.${cap}`, params: { percent } }
    }
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
      let budgetReason: string | undefined
      let eventError: string | undefined
      let failedBuilds = 0
      // The questions of each ask_user call and the plan of each propose_build_plan call, by tool call id.
      const asked = new Map<string, BuilderQuestion[]>()
      const proposed = new Map<string, BuilderPlan | null>()

      for (const event of list) {
        if (event.type === 'assistant_message') {
          const content = event.payload.content
          if (content && content.trim()) segments.push({ kind: 'message', id: event.id, content })
          continue
        }
        if (event.type === 'proposal' && event.payload.name === BUILDER_ASK_USER && event.payload.tool_call_id) {
          asked.set(event.payload.tool_call_id, parseQuestions(event.payload.operation) ?? [])
          continue
        }
        if (event.type === 'proposal' && event.payload.name === BUILDER_PROPOSE_BUILD_PLAN && event.payload.tool_call_id) {
          proposed.set(event.payload.tool_call_id, parsePlan(event.payload.operation))
          continue
        }
        if (event.type === 'plan_approved') {
          const id = event.payload.tool_call_id
          const approved: ApprovedPlan = {
            plan: (id && proposed.get(id)) || null,
            version: event.payload.version,
            milestone: event.payload.milestone,
          }
          segments.push({ kind: 'plan', id: event.id, approved })
          continue
        }
        if (event.type === 'plan_revision_requested') {
          segments.push({ kind: 'revision', id: event.id, moreQuestions: event.payload.more_questions === true })
          continue
        }
        if (event.type === 'questions_answered') {
          const round: AnsweredRound = {
            questions: (event.payload.tool_call_id && asked.get(event.payload.tool_call_id)) || [],
            answers: event.payload.answers ?? {},
            skipped: event.payload.skipped === true,
          }
          segments.push({ kind: 'questions', id: event.id, round })
          continue
        }
        if (event.type === 'limit_reached' && event.payload.limit) limit = event.payload.limit
        if (event.type === 'budget_exceeded' && event.payload.reason) budgetReason = event.payload.reason
        if (event.type === 'error' && event.payload.code) eventError = event.payload.code
        if (event.type === 'build_failed') failedBuilds += 1

        const entry = toEntry(event, event.type === 'build_started' ? failedBuilds : 0)
        if (!entry) continue
        const last = segments[segments.length - 1]
        if (last && last.kind === 'activity') last.entries.push(entry)
        else segments.push({ kind: 'activity', id: entry.id, entries: [entry] })
      }

      return { turn, segments, limit, budgetReason, errorCode: turn.error_code ?? eventError ?? null }
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
  return summarizeOperation(record(record(proposal.operation).operation))
}

/** How a new table is reached, as declared by the agent in a plan. */
export type PlanAccess = 'public' | 'owner' | 'policy'

export interface PlanOperationSummary extends ProposalSummary {
  /** Only on add_table: the access the agent declared, and its one-line reason. */
  access?: PlanAccess
  accessReason?: string
  isTable: boolean
}

export interface PlanSummary {
  summary: string
  operations: PlanOperationSummary[]
}

/**
 * Reads propose_backend_plan arguments ({summary, operations: [...]}) defensively. Returns null
 * when the proposal is not a plan, so the caller falls back to the single-change card.
 */
export function summarizePlan(proposal: BuilderProposal): PlanSummary | null {
  if (proposal.name !== 'propose_backend_plan') return null
  const args = record(proposal.operation)
  const ops = Array.isArray(args.operations) ? args.operations : []
  return {
    summary: str(args.summary),
    operations: ops.map((raw) => {
      const op = record(raw)
      const base = summarizeOperation(op)
      if (str(op.kind) !== 'add_table') return { ...base, isTable: false }
      const body = record(op.add_table)
      const access = str(body.access)
      return {
        ...base,
        isTable: true,
        access: access === 'public' || access === 'owner' || access === 'policy' ? access : undefined,
        accessReason: str(body.access_reason),
      }
    }),
  }
}

function summarizeOperation(op: Record<string, unknown>): ProposalSummary {
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
    case 'add_policy':
      // Table and template only: a policy's value is data, and the card shows names, never values.
      return { key: 'addPolicy', params: { table: str(body.table), template: str(body.template) }, lines: [] }
    default:
      return { key: 'unknown', params: { kind: kind || '?' }, lines: [] }
  }
}

function columnNamesOrStrings(value: unknown): string {
  if (!Array.isArray(value)) return ''
  const strings = value.filter((v): v is string => typeof v === 'string')
  return (strings.length > 0 ? strings : columnNames(value)).join(', ')
}

// -- Planning proposals (ai-builder-planning) --

/** Which card a paused turn needs: the planning tools have their own, every other name is a backend change. */
export type ProposalCardKind = 'questions' | 'plan' | 'backend'

export function proposalCardKind(proposal: BuilderProposal): ProposalCardKind {
  if (proposal.name === BUILDER_ASK_USER) return 'questions'
  if (proposal.name === BUILDER_PROPOSE_BUILD_PLAN) return 'plan'
  return 'backend'
}

/** The proposal the session is paused on, from the turn awaiting confirmation; undefined when none. */
export function pendingProposal(turns: BuilderTurn[]): BuilderProposal | undefined {
  return turns.find((t) => t.status === 'awaiting_confirmation' && t.pending_proposal)?.pending_proposal
}

/**
 * Reads ask_user arguments ({questions: [...]}) defensively. The server validated them, but the
 * stored event is still treated as untrusted: a question without an id, a text or two options is
 * dropped. Returns null when nothing usable is left.
 */
export function parseQuestions(args: unknown): BuilderQuestion[] | null {
  const raw = record(args).questions
  if (!Array.isArray(raw)) return null
  const questions: BuilderQuestion[] = []
  for (const item of raw) {
    const q = record(item)
    const id = str(q.id)
    const question = str(q.question)
    const options = Array.isArray(q.options)
      ? q.options
          .map((o) => ({ label: str(record(o).label), description: str(record(o).description) || undefined }))
          .filter((o) => o.label !== '')
      : []
    if (!id || !question || options.length < 2) continue
    questions.push({ id, title: str(q.title) || id, question, options, multiple: q.multiple === true })
  }
  return questions.length > 0 ? questions : null
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string' && v.trim() !== '') : []
}

/**
 * Reads propose_build_plan arguments defensively, like parseQuestions: an item without its name
 * (screen, table, milestone title) is dropped. Returns null when there is no milestone left, since
 * the user would be approving a plan they cannot see.
 */
export function parsePlan(args: unknown): BuilderPlan | null {
  const a = record(args)
  const list = (value: unknown) => (Array.isArray(value) ? value.map(record) : [])
  const milestones = list(a.milestones)
    .map((m) => ({ title: str(m.title), deliverable: str(m.deliverable), done: m.done === true }))
    .filter((m) => m.title !== '')
  if (milestones.length === 0) return null
  const look = record(a.look)
  return {
    summary: str(a.summary),
    screens: list(a.screens)
      .map((s) => ({ name: str(s.name), purpose: str(s.purpose) }))
      .filter((s) => s.name !== ''),
    data: list(a.data)
      .map((d) => ({ table: str(d.table), holds: str(d.holds), access: str(d.access), access_note: str(d.access_note) || undefined }))
      .filter((d) => d.table !== ''),
    look: {
      style: str(look.style) || undefined,
      colors: str(look.colors) || undefined,
      ui_language: str(look.ui_language) || undefined,
      reference: str(look.reference) || undefined,
    },
    assumptions: strings(a.assumptions),
    out_of_scope: strings(a.out_of_scope),
    milestones,
  }
}

const PLAN_ACCESS = ['public', 'signed_in', 'owner', 'role'] as const

/** The access of a plan table as a known code, or 'unknown' (drawn as a warning, like 'public'). */
export function planAccess(access: string): string {
  return knownCode(access, PLAN_ACCESS, 'unknown')
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

/** The caps of the budget_warning and budget_exceeded events (store.BudgetCap*). */
export const BUDGET_CAPS = ['session_tokens', 'month_tokens', 'session_cost', 'month_cost'] as const

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
