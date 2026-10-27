# AI Frontend Builder — Orbit-Native Agent Specification

## Problem Statement

Most Orbit users are non-technical. Today they can create a backend app by chat ("Build with AI" / "Edit with AI"), but a frontend app only exists as a GitHub repo generated from a template (or a ZIP upload) that someone still has to write code for — locally, with their own tools, then push via the deploy key (`sync-local-repo`). There is no way to *build* the frontend inside Orbit. The goal is a Lovable-style experience fully internal to the Orbit installation: the user chats, an Orbit-owned agent writes the frontend, the user watches a live preview, and the result is already wired to the user's Orbit backend app. The installing company only plugs in its own LLM credentials; Orbit orchestrates the agent and the whole harness. Nothing leaves the installation except calls to the LLM provider the company configured.

This spec covers the **first deliverable: a time-boxed PoC/MVP** that proves the architecture (own agent loop in the control plane + isolated builder service) and measures generation quality before committing to the full product.

## Goals

- [ ] A non-technical user can go from a plain-language description to a running preview of a frontend wired to an existing Orbit backend app, without touching code, git, GitHub or a terminal.
- [ ] The LLM API key never enters the environment where generated code runs (builder/sandbox), in any form.
- [ ] The same topology runs unchanged on docker-compose, Docker Swarm and Kubernetes, without mounting the Docker socket anywhere.
- [ ] Any agent session survives an Orbit replica restart or a request landing on a different replica, and is never run by two replicas at once.
- [ ] Generation quality is measured, not judged by impression: a repo-versioned eval suite runs against at least two models, with a pass threshold fixed in `design.md` before implementation starts.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Using Lovable (or any third-party builder) as the engine | Decided: code, preview and LLM traffic would live in a third-party cloud — contradicts the self-hosted positioning and the "everything inside Orbit" goal; Lovable's MCP is OAuth-per-user and in research preview. |
| Running a third-party agent CLI/harness (Claude Code, OpenHands, opencode…) inside the sandbox as the product engine | Decided: would require the LLM key inside the sandbox and lock provider choice to the harness's dialect. May still be used **once, offline**, as a quality baseline for the eval suite (design decision, not product surface). |
| Arbitrary shell command tool for the agent | Narrow, fixed tool set reduces attack surface for a non-technical audience. Revisit only with per-session isolation (see P3). |
| Per-session containers / pods, gVisor or Firecracker runtimes | Requires orchestrator-specific drivers (and on compose/swarm, Docker socket access). The `Sandbox` interface must allow it later; the PoC uses one long-lived builder service. |
| Hot module reload (Vite HMR over WebSocket) in the preview | Needs WebSocket proxying through a non-sticky LB and often wildcard DNS/TLS. PoC preview = rebuilt static output, reloaded on build-complete event. |
| Token-level streaming (SSE/WebSocket) to the UI | No streaming precedent in this codebase. PoC uses a persisted event log polled with a cursor; SSE can be layered on the same log later. |
| stdio MCP servers, and per-user OAuth for external MCP servers | stdio runs arbitrary binaries inside Orbit replicas; per-user OAuth needs a token store per user per server. First version: remote servers with a shared, superadmin-configured credential. |
| Subagents, RAG / vector memory, multi-agent planning | No concrete need before the PoC measures single-agent quality. |
| Choosing the frontend stack per session | One fixed, versioned base template (React + Vite + TS + Orbit TS SDK). Constraining the stack is a quality lever, not a limitation to remove in the PoC. |
| Multi-tenant hostile isolation between users of the same installation | Orbit is single-tenant per installation (`ai-build-chat` spec). Threat model: prompt injection, malicious/compromised npm packages, lateral movement to Orbit's database/network — not one employee attacking another. |
| Publishing to providers other than the existing frontend-app pipeline | Publishing reuses what already exists (frontend app + GitHub + Render). New publish targets (incl. publishing without GitHub) are a separate spec. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Where the agent loop runs | In Orbit's control plane (Go, `internal/...`), not inside the builder. The builder is a tool executor only. | Keeps the LLM key out of the code-execution environment; provider-agnostic; budget/approval/audit/events in Orbit's own model. | y (2026-10-26) |
| Implementation language | Go for the agent loop **and** the builder daemon. Node/npm exist in the builder image only as executed tooling. | One runtime, one binary family, one release flow; reuses `internal/crypto`, `*ForUser` handlers, audit, rate limiting. Maintainer is fluent in Go and TS, so this is a maintenance choice, not a skills constraint. | y (2026-10-26) |
| Who maintains | Single maintainer initially; design must stay extensible (interfaces at Provider / Tool / Sandbox / Event log). | Stated by the user. | y |
| Provider surface in PoC | Reuse the existing `zeep_system.ai_providers` row model. PoC supports `openai` plus an **OpenAI-compatible base URL** (covers Azure OpenAI, Ollama, gateways). Native Anthropic / Gemini adapters are P2. | Existing `internal/dashboard/ai` client is plain `net/http` against Chat Completions and was built to have sibling clients. Eval needs ≥2 models — an OpenAI-compatible endpoint provides the second without a new adapter. | y (2026-10-26) |
| Tool set | Fixed: `list_dir`, `read_file`, `write_file`, `edit_file` (exact old/new string; replaced `apply_patch` in design), `run_build` (fixed commands only: install, typecheck, build, lint), `get_build_logs`, plus scoped Orbit read tools (`get_app_schema`, list auth providers). Schema **mutations** are proposals requiring user confirmation. | Narrow surface; mutation-with-confirmation matches `ai-build-chat`/`ai-edit-chat` (no auto-execution). | y (2026-10-26, spec approved) |
| Where the code lives before publish | Working copy + git repo per session on the builder volume (fast, used by builds). After every turn, an incremental `git bundle` of that turn's commit is persisted **outside** the builder: in Postgres (`builder_snapshots`, zero extra config). S3-compatible storage for snapshots is a follow-up (design: `internal/storage` lacks Get/List and the global S3 secret is stored unencrypted today). The builder volume is a cache; the snapshots are the durable copy. `node_modules`/`dist` are never committed. GitHub is **not** required to build or preview — only to publish. | A pod/volume can be lost (swarm node down, k8s reschedule); without an external copy the user's work would vanish. Source of a small frontend without dependencies is small, so Postgres is acceptable as default; S3 offloads it on larger installs. | y (2026-10-26) |
| Relationship with GitHub after publish | First publish creates the frontend app (existing pipeline: GitHub repo + Render) and pushes the session history. Later turns do **not** push automatically; each "Publish" pushes the current HEAD, which triggers the existing deploy. | Avoids deploying half-finished work on every turn; the user decides when it goes live. | y (2026-10-26, spec approved) |
| Preview | Builder serves the output of the last successful `run_build build` (no watch process — design decision); Orbit reverse-proxies it under a path (`/dashboard/api/builder/sessions/{id}/preview/`). No wildcard DNS. | Works on a small VPS with docker-compose and a single domain. | y (2026-10-26) |
| Event delivery to the UI | Append-only `builder_events` table + `GET .../events?after=<cursor>` polling. | Stateless across replicas; fits AGENTS.md (no in-memory state). | y (2026-10-26, spec approved) |
| Session concurrency across replicas | Per-session lease row (`locked_by`, `lease_until`) or Postgres advisory lock, renewed while a turn runs. | Prevents two replicas running the same session; pattern precedent: `lockEventID` in `internal/server/webhook_handler.go`. | y (2026-10-26, spec approved) |
| Key handling between Orbit and builder | Builder never receives the provider key. Builder has no route to the LLM at all in the PoC (the loop runs in Orbit). | Strongest form of key isolation; gateway-with-session-token is only needed if a future tool runs an LLM inside the sandbox. | y (2026-10-26, spec approved) |
| Builder ↔ Orbit authentication | Orbit → builder calls signed with a shared secret (HMAC, `exp` claim), same pattern as `signState`/`verifyState`. New env var(s) documented in README config table. | Established stateless pattern; no new auth system. | y (2026-10-26, spec approved) |
| Builder network isolation | Builder on a network with no route to Postgres. Egress only through an allowlisting proxy (npm registry + Orbit URL). Compose/swarm: `internal: true` network + proxy service. K8s: NetworkPolicy. | Lateral-movement and exfiltration mitigation for generated code and npm install scripts. | y (2026-10-26, spec approved) |
| Docker socket | Never mounted in any Orbit or builder container. | Socket access is host root; socket proxies filter by endpoint, not by container spec (can't block `Privileged: true`). | y |
| Budget | Per-session and per-user monthly token caps, configured by superadmin, enforced in the loop before each LLM call; usage recorded per call. | Non-technical users can burn the company's API budget unknowingly. | y (2026-10-26, spec approved) |
| Concurrency on the builder | Configurable max concurrent active sessions per builder instance; excess sessions queue with a visible "waiting" state. Default value set after PoC measurement. | npm install + build per session is RAM/CPU heavy; small installs must not OOM. | y (2026-10-26, spec approved) |
| Who can use it | Same gate as creating a frontend app + `CanWrite` on the linked backend app (for proposing schema changes). | Reuses `rbac-per-app` (`ResolveAppRole`) — no new permission model. | y (2026-10-26, spec approved) |
| Audit origin | Mutations from this agent are tagged origin `ai_builder`. | Matches `ai_chat` / `mcp` origin precedent. | y (2026-10-26, spec approved) |
| Company customization model | Layered prompt: **Orbit core layer** (tool contract, safety, Orbit integration — not editable) → **company layer** (persona + rules, editable by superadmin) → session context. The core layer states that company rules cannot override it. | Each company has its own standards, but a company rule must never be able to disable confirmation, key isolation or app scoping. | y (2026-10-26, spec approved) |
| Design system / component library delivery | Primary mechanism: the company's **base template** (reusing `zeep_system.github_templates`) already contains the DS dependency, its tokens/theme and an ESLint config enforcing it. Docs about the DS are **knowledge packs** loaded on demand (P2). | Prompt rules are guidance; a template + lint rules are deterministic — `run_build lint` feeds violations back to the agent. Avoids stuffing large docs into every call. | y (2026-10-26, spec approved) |
| Private npm registry credentials | Injected by the egress proxy as an `Authorization` header for the configured registry host; the builder never holds the token. | Same principle as the LLM key: no secret where generated code runs. | y (2026-10-26, spec approved) |
| External MCP servers | Remote (Streamable HTTP) only, called by the **control plane** MCP client (`modelcontextprotocol/go-sdk`, already a dependency), never from the builder. Static header credential in the first version, encrypted at rest. Each tool is allowlisted by the superadmin and classified read-only or mutating; mutating tools always require user confirmation, like schema proposals. | stdio would mean running arbitrary binaries inside Orbit replicas; credentials stay out of the sandbox; external tool output is untrusted input (prompt injection) and must not trigger side effects unconfirmed. | y (2026-10-26, spec approved) |
| Interface language of agent replies | Agent replies in the user's dashboard language (`en` / `pt-BR`); API errors stay English. | AGENTS.md §4/§5. | y (2026-10-26, spec approved) |

**Resolved (2026-10-26):** publishing is in the MVP, through the existing frontend-app pipeline (GitHub repo + Render service linked to it). Minimum builder hardware is an **output** of the PoC (measured, then documented), not an input.

**Resolved (2026-10-26):** eval pass threshold = **8 of 10 tasks** producing an app that builds and typechecks, **on each** of two reference models.

**Resolved (2026-10-26):** reference models for the eval = **one OpenAI model** (native `openai` provider) and **one Gemini model** through Gemini's OpenAI-compatible endpoint (`https://generativelanguage.googleapis.com/v1beta/openai/`, documented as beta, supports `tools`/`tool_choice`). Exact model IDs are chosen at eval time (`models.list`) and recorded with each eval report. Because the Gemini compatibility layer is beta and documented to silently drop unsupported parameters, the provider contract tests SHALL run against both endpoints before the eval is trusted.

**Open questions:** none.

---

## User Stories

### P1: Superadmin enables the builder and its LLM ⭐ MVP

**User Story**: As a superadmin, I want to point the AI Frontend Builder at our LLM (key + model, optionally an OpenAI-compatible base URL) and set budget limits, so that our users can build frontends without configuring anything.

**Why P1**: Nothing runs without a configured model and limits.

**Acceptance Criteria**:

1. WHEN a superadmin saves provider config with a base URL, key and model THEN the system SHALL encrypt the key with the existing AES-256-GCM pattern and SHALL never return it on read (only `has_key`, `model`, `base_url`, `enabled`).
2. WHEN a superadmin updates the model without sending the key THEN the system SHALL preserve the stored key (merge-on-absent-key, `mergeProviderConfig` pattern).
3. WHEN a superadmin sets per-session and per-user-monthly token caps THEN the system SHALL persist them and SHALL apply them to every subsequent LLM call of the builder.
4. IF a non-superadmin calls any of these config endpoints THEN the system SHALL respond `403` without modifying anything.
5. WHEN the builder service is not reachable (health check fails) THEN the dashboard SHALL show the builder as unavailable and SHALL NOT allow starting a session.

**Independent Test**: Configure an OpenAI-compatible endpoint + caps as superadmin; GET returns no key; health shows "available"; stop the builder container and confirm the UI blocks new sessions.

---

### P1: User builds a frontend by chat and sees a preview ⭐ MVP

**User Story**: As a non-technical user, I want to describe the screen I need, linked to one of my backend apps, and see it running, so that I get a working frontend without writing code.

**Why P1**: This is the product.

**Acceptance Criteria**:

1. WHEN a user starts a builder session choosing a backend app they can access THEN the system SHALL create a session, initialize its repository from the current versioned base template, and pre-configure the Orbit SDK with that app's public API base URL (no secrets in frontend code).
2. WHEN the user sends a message THEN the system SHALL run an agent turn in the control plane: call the model with the fixed tool set, execute tool calls against the builder, loop until the model ends the turn or a limit is hit.
3. WHILE a turn runs THE system SHALL append events (`thinking`, `tool_call`, `file_changed`, `build_started`, `build_succeeded`/`build_failed`, `message`, `turn_finished`) to the session event log, readable via cursor polling.
4. WHEN a build fails during a turn THEN the agent SHALL receive the build/typecheck output as a tool result and SHALL be allowed to attempt fixes, up to a configured maximum of fix iterations per turn.
5. WHEN a turn ends with a successful build THEN the preview URL SHALL serve the new output and the UI SHALL reload the preview on the `build_succeeded` event.
6. WHEN a turn finishes THEN the builder SHALL commit all changes of that turn as one commit referencing the turn id.

**Independent Test**: Start a session against a backend app with a `tasks` table; ask "a page listing tasks with a form to add one"; observe events arriving, a successful build, and a preview listing rows from the real app via the SDK.

---

### P1: User iterates and reverts ⭐ MVP

**User Story**: As a user, I want to keep asking for changes and undo a change I didn't like, so that I can refine the result safely.

**Why P1**: A single-shot generator is not usable; revert is the safety net for non-technical users.

**Acceptance Criteria**:

1. WHEN the user sends a follow-up message THEN the agent SHALL operate on the current repository state and the session's conversation history.
2. WHEN the user reverts to a previous turn THEN the system SHALL restore the repository to that turn's commit, rebuild the preview, and record the revert as an event (history is never rewritten).
3. WHEN the conversation history exceeds the model's context budget THEN the system SHALL compact older turns (summary + file tree) instead of failing the turn.

**Independent Test**: Make three turns, revert to turn 1, confirm the preview matches turn 1 and the event log shows the revert.

---

### P1: Agent uses the real backend, and asks before changing it ⭐ MVP

**User Story**: As a user, I want the agent to build against my actual backend tables, and to ask me before it changes my backend, so that the frontend works and my data model doesn't change behind my back.

**Why P1**: "Already integrated with the Orbit backend" is the differentiator; uncontrolled schema mutation by an LLM is unacceptable.

**Acceptance Criteria**:

1. WHEN the agent needs the backend shape THEN it SHALL read it through scoped read tools limited to the session's linked app, executed via the existing `*ForUser` handlers with the session user's permissions.
2. WHEN the agent decides the backend needs a change (new table/column/policy) THEN it SHALL emit a structured proposal (reusing the `ai-edit-chat` operation shape) and the turn SHALL pause in `awaiting_confirmation`.
3. WHEN the user confirms a proposal THEN the system SHALL apply it through the same path as "Edit with AI" confirm (same validation, RBAC, audit with origin `ai_builder`) and resume the agent with the result.
4. WHEN the user rejects a proposal THEN the agent SHALL be resumed with the rejection and SHALL NOT apply the change.
5. IF the session user lacks `CanWrite` on the linked app THEN proposals SHALL be rejected with a clear message and the agent SHALL continue with read-only access.
6. The agent SHALL NOT have access to any app other than the session's linked app.

**Independent Test**: Ask for a feature needing a new column; confirm the turn pauses with a proposal; reject → no DDL ran; ask again and confirm → column exists, audit row has origin `ai_builder`.

---

### P1: Key isolation, budget and safe execution ⭐ MVP

**User Story**: As the company installing Orbit, I want our LLM key protected and spend bounded, and generated code unable to reach our database, so that adopting the builder doesn't create a new security or cost risk.

**Why P1**: Running LLM-generated code and npm packages is the largest new risk this feature introduces.

**Acceptance Criteria**:

1. The builder SHALL never receive the LLM provider key, in env, files, arguments or tool results.
2. WHEN an LLM call would exceed the session or user-monthly cap THEN the system SHALL NOT make the call, SHALL end the turn with a `budget_exceeded` event, and SHALL show a localized message to the user.
3. The system SHALL record tokens in/out per LLM call, per session and per user.
4. The builder SHALL run commands only from the fixed allowlist, as a non-root user, with CPU/memory/time limits per command.
5. The reference deployment manifests (compose, swarm stack, Helm) SHALL place the builder on a network with no route to Postgres, with egress only through the allowlisting proxy, and SHALL NOT mount the Docker socket.
6. Calls between Orbit and the builder SHALL be authenticated with a signed, expiring token; unauthenticated calls SHALL be rejected.
7. Provider errors SHALL be logged server-side; users SHALL see a generic localized error (no raw provider error text).

**Independent Test**: Inspect the builder container env/filesystem during a turn — no key present; from inside the builder, attempt to connect to Postgres and an arbitrary external host — both fail; lower the cap below one turn's cost — turn stops with `budget_exceeded`.

---

### P1: Sessions are durable across replicas ⭐ MVP

**User Story**: As a user, I want my build session to keep working if Orbit restarts or my requests hit a different server, so that I never lose progress.

**Why P1**: Orbit runs multiple stateless replicas behind a non-sticky load balancer (AGENTS.md §4).

**Acceptance Criteria**:

1. All session state (messages, tool calls/results, events, status, lease) SHALL be persisted in Postgres; no in-memory map SHALL be required to continue a session.
2. WHEN a turn starts THEN the system SHALL acquire an exclusive lease on the session; a second replica attempting to run the same session SHALL be refused.
3. WHEN the replica running a turn dies THEN the lease SHALL expire and the session SHALL become resumable, continuing from the last persisted step (a tool call is never executed twice without its result being checked first).
4. WHEN a builder instance loses its volume THEN the system SHALL be able to restore the session repository from the last persisted commit (mechanism defined in `design.md`).

**Independent Test**: Kill the Orbit replica mid-turn; after lease expiry, resume from another replica; confirm the event log is contiguous and no file change was applied twice.

---

### P1: Quality eval suite (maintainer-facing) ⭐ MVP

**User Story**: As the maintainer, I want a repeatable eval suite for the agent, so that I can tune prompts, compare models and detect regressions with data.

**Why P1**: The PoC's go/no-go decision depends on measured quality.

**Acceptance Criteria**:

1. The repo SHALL contain a versioned set of eval tasks (prompt + backend fixture + checks).
2. WHEN the suite runs against a configured model THEN it SHALL report, per task: build passes, typecheck passes, lint passes, turns used, tokens used, and fix iterations used.
3. The suite SHALL be runnable against at least two different models with the same task set and produce a comparable summary.
4. The suite SHALL NOT run in the default CI test command (`go test ./...`) — it is opt-in (needs real LLM credentials and costs money).

**Independent Test**: Run the suite against two configured models; get a summary table with per-task results and totals.

---

### P1: Company standards — persona, rules and base template ⭐ MVP

**User Story**: As a superadmin, I want to define the agent's persona, our development rules and which base template (with our design system and component library) it must start from, so that every screen generated follows our company standards.

**Why P1**: Each company installing Orbit has its own standards; the eval must measure quality with them applied, not only with Orbit's defaults.

**Acceptance Criteria**:

1. WHEN a superadmin saves the persona and rules texts THEN the system SHALL store them versioned (who, when, previous version kept), audit-log the change, and apply them to sessions started after the change.
2. The company layer SHALL be inserted after the Orbit core layer, and the core layer SHALL take precedence; WHEN company rules contradict core rules (e.g. "apply schema changes without asking") THEN the agent SHALL still follow the core rules.
3. WHEN persona + rules exceed the configured size limit THEN the system SHALL reject the save with a clear message (context budget protection).
4. WHEN a superadmin marks a registered template as a builder base template THEN users starting a session SHALL choose among the enabled builder templates (or get the only one automatically), and the session SHALL record which template and version it started from.
5. WHEN the template contains a lint configuration THEN `run_build lint` SHALL run it and the agent SHALL receive violations as tool results.
6. WHEN a private npm registry is configured THEN installs SHALL authenticate through the egress proxy, and the token SHALL NOT be present in the builder.

**Independent Test**: Register a template that depends on a component library and has a lint rule forbidding raw `<button>`; set a rule "always use the library's Button"; ask for a form; confirm the generated code imports the library's Button and lint passes; check the persona/rules history shows the change with author.

---

### P2: Knowledge packs (design system docs, guidelines)

**User Story**: As a superadmin, I want to upload documentation (design system usage, component catalog, UX guidelines) as packs the agent consults when relevant, so that it uses our components correctly without every doc going into every call.

**Acceptance Criteria**:

1. WHEN a superadmin creates a pack (name, short description, markdown files) THEN the system SHALL store it and only the name + description SHALL be included in the agent prompt.
2. WHEN the agent calls `load_knowledge(pack, file)` THEN the system SHALL return that content as a tool result.
3. WHEN a pack is disabled THEN new turns SHALL no longer see it.
4. Pack content SHALL be treated as reference data, not as instructions that override the core layer.

---

### P2: External MCP servers as agent tools

**User Story**: As a superadmin, I want to connect MCP servers (e.g. our design system docs, Figma, internal APIs) so that the agent can use them while building.

**Acceptance Criteria**:

1. WHEN a superadmin registers a remote MCP server (URL + optional header credential) THEN the system SHALL encrypt the credential, never return it on read, connect from the control plane, and list the server's tools.
2. WHEN the superadmin enables tools THEN only enabled tools SHALL be offered to the agent; each SHALL be marked read-only or mutating.
3. WHEN the agent calls a mutating external tool THEN the turn SHALL pause for user confirmation before the call is made.
4. WHEN the URL resolves to a loopback, link-local or cloud metadata address THEN the system SHALL refuse to connect.
5. WHEN an external tool fails or times out THEN the agent SHALL receive a tool error and the turn SHALL continue.
6. Every external tool call SHALL be recorded in the session event log (server, tool, outcome), without logging credential values.

---

### P2: Native Anthropic and Gemini providers

**User Story**: As a superadmin, I want to use Anthropic or Gemini directly, so that we can use the model we already pay for.

**Acceptance Criteria**:

1. WHEN a superadmin configures `anthropic` or `gemini` THEN the agent loop SHALL work with it through the same `Provider` interface, with no change to tools or loop logic.
2. Each adapter SHALL pass the same provider contract test suite as the OpenAI adapter.

---

### P1: Publish the built frontend ⭐ MVP

**User Story**: As a user, I want to publish what I built, so that people can use it.

**Why P1**: Confirmed by the user — a frontend that only exists as a preview has no value to a non-technical user; the existing frontend-app pipeline already does the hard part.

**Acceptance Criteria**:

1. WHEN the user publishes a session for the first time THEN the system SHALL create a frontend app through the existing pipeline (private GitHub repo + Render service linked to it, linked to the session's backend app) and push the session's git history to it, without the user touching GitHub or Render.
2. WHEN the user publishes again THEN the system SHALL push the current HEAD to the same repo, triggering the existing deploy; turns between publishes SHALL NOT be pushed automatically.
3. WHEN publish fails at any step (repo creation, push, deploy) THEN the session SHALL keep its code intact, the frontend app SHALL follow its existing `failed` + retry behavior, and the user SHALL see a localized error.
4. IF GitHub or the deploy provider is not configured THEN the publish action SHALL be disabled with a message saying what the admin must configure, and SHALL offer a ZIP download of the source.

**Independent Test**: Publish a session → frontend app appears with repo + Render service and a live URL; make another turn, confirm nothing deploys; publish again → new deploy with the change.

---

### P3: Stronger isolation and live reload

**User Story**: As an installer on Kubernetes, I want one isolated sandbox per session and live reload, so that the experience and isolation match hosted builders.

**Acceptance Criteria**:

1. WHERE the deployment is Kubernetes THEN a `Sandbox` driver SHALL run each session in its own pod (optionally with a restricted `RuntimeClass`), selected by config, with no change to the agent loop.
2. The preview SHALL support HMR through Orbit's proxy.

---

## Edge Cases

- WHEN the model returns a malformed tool call THEN the system SHALL return a tool error to the model (not crash the turn) and count it toward the iteration limit.
- WHEN the agent hits the max tool-call iterations or max wall-clock per turn THEN the turn SHALL end with a `limit_reached` event and a user-facing explanation; partial changes SHALL still be committed.
- WHEN the agent writes outside the project directory (path traversal, symlinks) THEN the builder SHALL reject the operation.
- WHEN the agent tries to add an npm dependency not in the allowlist (if an allowlist is configured) THEN `run_build install` SHALL fail with a clear tool error the model can react to.
- WHEN the linked backend app is deleted or the user loses access mid-session THEN the next turn SHALL fail closed with a clear message and SHALL NOT expose schema data.
- WHEN the builder is at max concurrency THEN new turns SHALL queue with a visible `queued` status instead of failing.
- WHEN the user sends a message while a turn is running THEN the system SHALL reject it (one turn at a time per session) with a clear localized message.
- WHEN the base template version changes THEN existing sessions SHALL keep their own template version; only new sessions use the new one.
- WHEN the provider returns rate-limit / quota errors THEN the system SHALL retry with backoff a bounded number of times, then end the turn with a generic error.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AFB-01 | P1: Superadmin enables builder & LLM — provider config, encrypted key, merge-on-absent | Design | Pending |
| AFB-02 | P1: Superadmin enables builder & LLM — budget caps | Design | Pending |
| AFB-03 | P1: Superadmin enables builder & LLM — builder health / availability gate | Design | Pending |
| AFB-04 | P1: Build by chat — session creation from versioned base template + SDK wiring | Design | Pending |
| AFB-05 | P1: Build by chat — agent loop in control plane with fixed tool set | Design | Pending |
| AFB-06 | P1: Build by chat — persisted event log + cursor polling | Design | Pending |
| AFB-07 | P1: Build by chat — build-error feedback loop with fix limit | Design | Pending |
| AFB-08 | P1: Build by chat — path-proxied static preview + reload on build event | Design | Pending |
| AFB-09 | P1: Build by chat — one commit per turn | Design | Pending |
| AFB-10 | P1: Iterate & revert — follow-ups, revert to turn, history compaction | Design | Pending |
| AFB-11 | P1: Real backend — scoped read tools via `*ForUser` | Design | Pending |
| AFB-12 | P1: Real backend — schema proposals with confirm/reject, origin `ai_builder` | Design | Pending |
| AFB-13 | P1: Safety — key never in builder | Design | Pending |
| AFB-14 | P1: Safety — budget enforcement + usage ledger | Design | Pending |
| AFB-15 | P1: Safety — command allowlist, non-root, resource limits | Design | Pending |
| AFB-16 | P1: Safety — network isolation, egress proxy, no Docker socket (compose/swarm/Helm) | Design | Pending |
| AFB-17 | P1: Safety — signed Orbit↔builder auth | Design | Pending |
| AFB-18 | P1: Durability — Postgres-persisted state, session lease, resumable turns | Design | Pending |
| AFB-19 | P1: Durability — repository recovery after builder volume loss | Design | Pending |
| AFB-20 | P1: Eval suite — versioned tasks, per-model report, opt-in | Design | Pending |
| AFB-21 | P2: Native Anthropic/Gemini adapters + provider contract tests | - | Pending |
| AFB-22 | P1: Publish via existing frontend-app pipeline (first publish + republish) / ZIP fallback | Design | Pending |
| AFB-23 | P3: Per-session sandbox driver (Kubernetes) | - | Pending |
| AFB-24 | P3: HMR preview | - | Pending |
| AFB-25 | P1: Company standards — versioned persona + rules, layered under the core prompt | Design | Pending |
| AFB-26 | P1: Company standards — builder base templates (reuse `github_templates`) + lint feedback | Design | Pending |
| AFB-27 | P1: Company standards — private registry auth via egress proxy | Design | Pending |
| AFB-28 | P2: Knowledge packs loaded on demand | - | Pending |
| AFB-29 | P2: External MCP servers — registration, encrypted credential, tool allowlist | - | Pending |
| AFB-30 | P2: External MCP servers — confirmation for mutating tools, SSRF guard, event logging | - | Pending |

**Coverage:** 30 total, 0 mapped to tasks, 30 unmapped ⚠️ (expected — design not started)

---

## Success Criteria

- [ ] Eval suite passes the threshold fixed in `design.md` with at least two different models (threshold and models chosen by the user before implementation).
- [ ] The same reference deployment (Orbit + builder + egress proxy) runs on docker-compose, Docker Swarm and Kubernetes, verified by a manual run of the P1 independent tests on each.
- [ ] All P1 independent tests pass, including the key-isolation and network-isolation checks.
- [ ] Zero LLM provider error text reaches a non-superadmin user (verified by forcing auth/quota errors).
- [ ] Go/no-go decision on the full product recorded in `.specs/project/STATE.md` with the eval numbers.
