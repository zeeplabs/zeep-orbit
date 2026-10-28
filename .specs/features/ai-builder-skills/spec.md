# AI Builder Skills Specification

> Status: **Open questions answered 2026-10-28, ready for design.** Child of `.specs/features/ai-frontend-builder/` — read its `spec.md` §"Company standards" and §"Knowledge packs" first. This spec **supersedes AFB-28 (knowledge packs)**: see Assumptions row 1.

## Problem Statement

The builder agent today has three knobs: the core prompt (shipped, locked), the company layer (persona + rules, one text, always in the prompt) and the template guide. Everything the agent should know goes into prompts that are paid for on every call, so quality guidance (visual design, accessibility, React patterns, a company's component catalog) has to stay short or it eats the context budget. Test sessions on 2026-10-27 produced generic layouts. The agent needs reusable, on-demand expertise — the "skills" model of Claude Code and Codex: a short description always visible, the full instructions loaded only when the task matches — and Orbit should ship good frontend/design skills out of the box while letting the superadmin add the company's own.

## Goals

- [ ] The agent loads a skill only when the task matches its description: in the eval, the expected skill is loaded in the positive tasks and not loaded in the negative ones (rates reported per model; gate fixed in design after a baseline exists).
- [ ] A fresh Orbit install ships built-in frontend/design skills that the agent can use with zero configuration, and no one can edit or delete them from the dashboard or the API.
- [ ] A superadmin can add, version, disable and remove the company's own skills from the dashboard by uploading `.md` files, and the next agent turn uses them.
- [ ] Zero prompt cost when no skill is enabled; bounded, visible prompt cost when skills exist (index budget).
- [ ] A skill can never widen what the agent is allowed to do (no new tools, no permission change, no override of the core rules).

## Out of Scope

| Feature | Reason |
| --- | --- |
| Import skills from a URL, GitHub repo or public skills registry at runtime | Fetching remote text that becomes agent instructions is a supply-chain risk (same reasoning as the `shadcn`/`web-design-guidelines` local patches in `AGENTS.md` §9). Upload only. |
| Skill scripts, executables, `allowed-tools`, `hooks`, `model` or any frontmatter that changes behavior | The builder has a fixed tool set by design (parent spec, Out of Scope). Such keys are ignored and reported, never honored. |
| Per-user or per-session skills | Skills are company-wide config managed by the superadmin, like persona and rules. |
| Skills bundled inside a template (`orbit-template.json`) | The template already has `ORBIT_AGENT.md` (template guide). Revisit as P3. |
| The end user picking or invoking a skill by name (`/skill`) | Audience is non-technical; the agent decides. The timeline shows which skill was used (SKL-09). |
| The agent creating or editing skills | Skills are trusted config; letting the model write its own instructions is a prompt-injection amplifier. |
| Skill marketplace, ratings, sharing between installations | No demand; single maintainer. |
| Automatic skill selection by embeddings/RAG | Description matching by the model is the mechanism; measured by the eval before adding anything heavier. |

---

## Assumptions & Open Questions

| # | Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | Skills **replace** knowledge packs (AFB-28, tables `builder_knowledge_packs/files`, tool `load_knowledge`, the `PacksIndex` slot in `internal/agent/prompt.go`) | One mechanism: a skill is a named description + instructions (+ extra files in P2). `load_knowledge` is never built; `PacksIndex` carries the skills index. | A pack is "name + description + markdown files loaded on demand" — the same thing. Two mechanisms would double the UI, the prompt section and the eval. Nothing of AFB-28 is implemented yet, so the cost of replacing is zero. | y (2026-10-28) |
| 2 | Skill file format | One `SKILL.md`-style file: YAML frontmatter (`name`, `description`, optional `license`) + markdown body. Same shape as Claude Code/Codex so existing skills port with edits. | Familiar to the maintainer and to anyone who writes skills today. | y (user: "arquivos.md, igual Claude Code/Codex", 2026-10-28) |
| 3 | Discovery mechanism | Index (name + description) in the system prompt under its own header, lower priority than core; new read-only tool `load_skill(name)` returns the body. | Mirrors Claude Code. Reuses the loop's existing tool/Kind model and the `PacksIndex` slot. Cost when unused = index only. | y (intent), design to confirm |
| 4 | Where built-in skills live | In the Go binary (`go:embed`), versioned with the release, not copied to the database. DB stores only per-built-in enabled state. | "Cannot be edited or deleted" is guaranteed by construction (nothing to edit), upgrades update them with no data migration, and replicas of one version agree. | y (intent), design to confirm |
| 5 | Built-in naming | Reserved prefix `orbit-` for built-ins (e.g. `orbit-frontend-design`); custom skills may not use it. | A future built-in can never collide with an existing custom skill name. | y (2026-10-28) |
| 6 | Built-ins authored or adapted by us, not copied as-is from third parties | Each built-in is written for the builder's tool set (React + Vite + TS + plain CSS + HashRouter, `read_file`/`write_file`/`run_build`) and carries provenance + license in its frontmatter. Candidate sources are found with `find-skills` during design; nothing is installed or vendored without a license check and your confirmation. | Existing skills assume a shell, `npx`, Python scripts or remote fetches the agent does not have (several repo-vendored ones, e.g. `ui-ux-pro-max`, `web-design-guidelines`, needed local patches for exactly this), and many assume other stacks (shadcn/Tailwind). Copying verbatim gives the model instructions it cannot follow. | y (2026-10-28) |
| 7 | Can the superadmin **disable** a built-in? | Yes: enable/disable toggle per built-in. Content stays locked; disabling is not editing or deleting. | A company with its own design system may not want "be visually distinctive" guidance competing with its tokens. Without a toggle the only way to stop it would be editing, which is forbidden. | y (2026-10-28) |
| 8 | What the superadmin can do to **custom** skills | Upload new, upload a new version of the same name, disable/enable, delete (soft: versions kept for audit). | Upload mistakes must be recoverable; an "upload-only" rule applied to custom skills would make them permanent. The lock applies to built-ins. | y (2026-10-28: "custom pode ser editadas sim") |
| 9 | Precedence | CORE RULES > company standards > template guide > skills. A skill body is loaded as a tool result labeled "reference guidance". | Skills carry no authority. A built-in design skill must yield to the company's design system. | y |
| 10 | Versioning and propagation | Custom skill content is versioned per upload (who/when/previous kept). The agent resolves the enabled set **per turn**, so disable/delete/new version applies from the next turn of every session. Sessions do not pin skill versions (unlike persona). | A harmful or wrong skill must be stoppable immediately. The cost (a session's guidance can change between turns) is accepted. | y (proposal) |
| 11 | Proposed limits (defaults, final values in design) | `name` ≤ 64 chars `^[a-z0-9]+(-[a-z0-9]+)*$`; `description` ≤ 300 chars, single line; body ≤ 24 KiB (below the 32 KiB tool-result cap); ≤ 5 distinct skill loads per turn; index budget ≈ 8000 chars (today's `MaxPacksIndexChars` is 4000: design raises or splits it). | Keeps the always-on cost predictable and the body inside one tool result. | y (proposal) |
| 12 | Who can manage | Superadmin only, same gate as the builder settings. Users and non-superadmins never see custom skill bodies. | Same as persona/rules. | y |
| 13 | Configuration storage | Postgres (`zeep_system`), no new env var. | AGENTS.md §8; settings already live there. | y |

**Open questions:** none. Native Gemini/Anthropic adapters (AFB-21) are a separate follow-up after the whole builder phase; skill triggering is measured on whatever reference models the eval runs, with no Gemini-specific work in this spec (user, 2026-10-28).

---

## User Stories

### P1: The agent finds and uses the right skill on its own ⭐ MVP

**User Story**: As a non-technical user, I want the builder to apply the right design and frontend expertise without me asking, so my screens are good without me knowing the vocabulary.

**Why P1**: This is the whole point; without reliable triggering the rest is shelf-ware.

**Acceptance Criteria**:

1. WHEN a turn starts and at least one skill is enabled THEN the system prompt SHALL include a skills index (name + description only, no body) under its own header, lower priority than CORE RULES, and the `load_skill` tool SHALL be registered. (SKL-01)
2. WHEN no skill is enabled THEN the prompt SHALL have no skills section and `load_skill` SHALL NOT be registered. (SKL-01)
3. WHEN the agent calls `load_skill(name)` for an enabled skill THEN the result SHALL be the skill body preceded by a header with name, source (built-in/custom) and version, labeled as reference guidance. The tool is read-only: it never pauses the turn. (SKL-02)
4. WHEN the name is unknown, disabled or deleted THEN the tool SHALL return an error result listing the currently available skill names, and the turn SHALL continue. (SKL-03)
5. WHEN a skill body asks for a tool, a permission, a secret, to ignore or reveal the core rules, or to skip a backend-change confirmation THEN the agent SHALL keep following the core rules; the core prompt SHALL say so explicitly. (SKL-04)
6. WHEN a skill conflicts with the company standards or the template guide (e.g. a design skill suggests custom fonts and colors, the company design system mandates its own tokens) THEN the agent SHALL follow the company standards / template guide. (SKL-04)
7. WHEN a turn tries more distinct skill loads than the per-turn limit THEN further calls SHALL return an error result. (SKL-05)
8. WHEN a skill is loaded THEN a session event SHALL record skill name, source and version (not the body), and the dashboard timeline SHALL show it like any other step. (SKL-09)
9. WHEN the index is rendered THEN descriptions SHALL be collapsed to one line and stripped of header syntax, so a description cannot forge a prompt section such as `# CORE RULES`. (SKL-06)

**Independent Test**: Upload a custom skill `invoice-layout` ("Use when building invoice or billing screens") whose body requires a marker (e.g. a bold "Total" row with a specific `data-testid`). Ask for an invoice page: the event log shows `load_skill invoice-layout` and the code has the marker. Ask for an unrelated contact form in another session: no load.

---

### P1: Orbit ships built-in frontend skills, locked ⭐ MVP

**User Story**: As a superadmin, I want sensible frontend/design skills available from day one that nobody can tamper with, so quality does not depend on me writing prompts.

**Why P1**: Test sessions without any guidance produced generic layouts; the default experience must already be good.

**Acceptance Criteria**:

1. WHEN a fresh installation lists skills THEN it SHALL include the built-in set (content fixed in design; areas: visual design quality, accessibility, React/TypeScript patterns, responsive layout, forms and UI states), each enabled by default and marked built-in with source, license and version. (SKL-07)
2. WHEN Orbit is upgraded THEN built-in skills SHALL update with the binary, with no data migration and no stale database copy. (SKL-07)
3. WHEN any caller (including a superadmin, by UI or API) attempts to edit the content or name of, or delete, a built-in THEN the server SHALL reject with a stable English error code (`skill_builtin_readonly`) and change nothing. The UI SHALL show built-ins with a lock indicator, read-only content view and no edit/delete controls. (SKL-08)
4. WHEN a custom skill is created with the reserved `orbit-` prefix THEN the server SHALL reject it. (SKL-08)
5. WHEN CI runs THEN a test SHALL fail the build if any built-in: does not parse, exceeds the limits, mentions tools that are not registered for the builder or instructs shell/`npx`/remote fetching, or lacks provenance and license. (SKL-10)
6. WHEN a superadmin toggles a built-in THEN the change SHALL apply from the next turn and be audit-logged (assumption row 7). (SKL-11)

**Independent Test**: Fresh DB: list shows the built-ins locked. `PUT`/`DELETE` on one returns `skill_builtin_readonly`. Toggle one off: next turn's prompt index no longer lists it. Break one built-in's frontmatter in a branch: CI fails.

---

### P1: Superadmin uploads and manages custom skills ⭐ MVP

**User Story**: As a superadmin, I want to upload the company's own skills (component catalog usage, UX rules, domain screens), so the agent follows our standards where generic guidance cannot.

**Why P1**: Each company has its own design system and conventions; this is how they enter without code changes.

**Acceptance Criteria**:

1. WHEN a superadmin uploads a `.md` file THEN the server SHALL parse the frontmatter, validate `name`, `description` and size limits, and either create the skill or return field-level errors (English codes/messages; the UI localizes). (SKL-12)
2. WHEN the `name` already exists (case-insensitive) THEN the upload SHALL be treated as a **new version** of that skill, not a duplicate: who, when and the previous version kept. (SKL-13)
3. WHEN the frontmatter contains keys that would change behavior (`allowed-tools`, `hooks`, `model`, or any unknown key) THEN the system SHALL ignore them, store nothing from them and show a warning. (SKL-14)
4. WHEN enabling or uploading a skill would push the index past its budget THEN the system SHALL refuse with the current and maximum values; it SHALL NOT silently drop skills from the agent's view. (SKL-15)
5. WHEN a superadmin disables, enables or deletes a custom skill THEN the change SHALL apply from the next turn; delete SHALL ask for confirmation and be soft (versions retained for audit). (SKL-16)
6. WHEN any skill action happens (create, new version, enable, disable, delete) THEN the system SHALL audit-log actor, skill name, version and a content hash — not the body. (SKL-17)
7. WHEN a non-superadmin calls any skill management endpoint THEN the server SHALL return 403. (SKL-17)
8. WHEN uploaded content matches high-confidence credential patterns (private key block, provider API key formats, JWTs) THEN the system SHALL reject it with a message; this is a heuristic, not a guarantee, and the UI SHALL say the content is sent to the configured LLM provider. (SKL-18)
9. All new UI strings SHALL exist in `en` and `pt-BR`; API errors stay English. (SKL-19)

**Independent Test**: Upload a valid file: it appears enabled with version 1 and an audit entry. Upload it again edited: version 2, version 1 retained. Upload a file with `allowed-tools: Bash` : warning shown, no effect. Upload a file with a `-----BEGIN PRIVATE KEY-----` block: rejected. Call the endpoints as a regular user: 403.

---

### P1: Skill triggering is measured ⭐ MVP

**User Story**: As the maintainer, I want the eval to tell me whether each model loads the right skill at the right time, so "it knows when to use skills" is a number, not an impression.

**Why P1**: Description-based triggering varies between models, so it needs a number per model.

**Acceptance Criteria**:

1. WHEN the eval suite (parent spec AFB-20 / T42) runs THEN it SHALL include at least 3 positive tasks (a specific skill is expected to be loaded before the first write) and 2 negative tasks (no load expected), using fixture skills so results do not depend on production content. (SKL-20)
2. WHEN a report is produced THEN it SHALL show per model: expected-load hit rate, unexpected-load rate and the build/typecheck pass rate with skills on. (SKL-21)
3. WHEN no baseline exists yet THEN skill metrics SHALL be informational; the gate (and the numbers) are fixed in `design.md` after the first run. (SKL-21)

**Independent Test**: Run the eval on the reference models; the report lists each with the three metrics.

---

### P2: Multi-file skills (the former knowledge packs)

**User Story**: As a superadmin, I want a skill to carry extra reference files (component catalog, token tables), so large design-system docs are loaded in parts, only when needed.

**Acceptance Criteria**:

1. WHEN a superadmin uploads a skill as one `SKILL.md` plus extra `.md` files (relative paths, no `..`, `.md` only, bounded count and total size) THEN the system SHALL store them as one skill version. (SKL-22)
2. WHEN the agent calls `load_skill(name, file)` THEN the system SHALL return that file; WHEN `file` is omitted THEN it SHALL return `SKILL.md`, which lists the available files. (SKL-23)
3. WHEN a file path escapes the skill (`..`, absolute, symlink-like names) THEN the system SHALL reject it at upload and at load. (SKL-23)

**Independent Test**: A skill with `SKILL.md` + `components/button.md`: the agent loads the index file first, then only the file it needs.

---

### P3: Quality-of-life

1. WHEN a superadmin opens a built-in THEN the UI MAY offer "duplicate as custom" (new name, editable copy) so a company can tailor a locked built-in without touching it. (SKL-24)
2. A template MAY ship skills in its manifest (parent spec: template guide). (SKL-25)

---

## Edge Cases

- WHEN the model emits a malformed `load_skill` call THEN the system SHALL return a tool error and count it toward the turn's iteration limit, like any tool.
- WHEN a skill is disabled or deleted in the middle of a turn THEN a later `load_skill` in that turn SHALL return the unavailable error; content already loaded stays in the conversation.
- WHEN the history is compacted THEN the agent MAY load the skill again; the per-turn load limit still applies.
- WHEN uploaded bytes are not UTF-8, are empty, have a BOM or CRLF line endings, or have no/invalid/duplicate frontmatter keys THEN the system SHALL normalize what is safe (BOM, CRLF) and reject the rest with a field-level error.
- WHEN a skill body contains prompt-injection text THEN it is reference data under the precedence rule; the core prompt already treats tool-result content as data, not instructions.
- WHEN replicas run different Orbit versions during a rolling upgrade THEN a built-in present on one and absent on the other SHALL produce, at worst, the unavailable error on `load_skill` (the model recovers); no cross-replica state is needed.
- WHEN a custom skill is soft-deleted and a new one is uploaded with the same name THEN it SHALL start a new lineage with a new version 1 (history of the old one stays in audit).
- WHEN the builder feature is off (`BUILDER_URL` unset) THEN skill management endpoints behave like the other builder config routes (not gated), and nothing is loaded by any agent.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SKL-01 | P1: Agent uses skills — index in prompt, `load_skill` registered only when skills exist | Design | Pending |
| SKL-02 | P1: Agent uses skills — `load_skill` returns labeled body, read-only | Design | Pending |
| SKL-03 | P1: Agent uses skills — unknown/disabled name returns recoverable error | Design | Pending |
| SKL-04 | P1: Agent uses skills — precedence (core > company > template > skills), no capability widening | Design | Pending |
| SKL-05 | P1: Agent uses skills — per-turn load limit | Design | Pending |
| SKL-06 | P1: Agent uses skills — index sanitization (no forged sections) | Design | Pending |
| SKL-07 | P1: Built-ins — embedded, versioned with release, enabled by default | Design | Pending |
| SKL-08 | P1: Built-ins — immutable server-side (`skill_builtin_readonly`), locked UI, reserved prefix | Design | Pending |
| SKL-09 | P1: Observability — skill load event + timeline step | Design | Pending |
| SKL-10 | P1: Built-ins — CI lint (parse, limits, tool references, provenance/license) | Design | Pending |
| SKL-11 | P1: Built-ins — enable/disable toggle, audited | Design | Pending |
| SKL-12 | P1: Custom — upload, parse, validate, field-level errors | Design | Pending |
| SKL-13 | P1: Custom — versioning on same name | Design | Pending |
| SKL-14 | P1: Custom — behavior-changing frontmatter ignored with warning | Design | Pending |
| SKL-15 | P1: Custom — index budget enforced at enable/upload | Design | Pending |
| SKL-16 | P1: Custom — enable/disable/soft delete, effective next turn | Design | Pending |
| SKL-17 | P1: Custom — audit log + superadmin-only (403) | Design | Pending |
| SKL-18 | P1: Custom — credential-pattern rejection + LLM-provider disclosure in UI | Design | Pending |
| SKL-19 | P1: i18n en/pt-BR, English API errors, CHANGELOG/README/docs | Design | Pending |
| SKL-20 | P1: Eval — positive and negative skill-trigger tasks with fixture skills | Design | Pending |
| SKL-21 | P1: Eval — per-model skill metrics, informational until baseline | Design | Pending |
| SKL-22 | P2: Multi-file skills — upload | - | Pending |
| SKL-23 | P2: Multi-file skills — `load_skill(name, file)` + path safety | - | Pending |
| SKL-24 | P3: Duplicate built-in as custom copy | - | Pending |
| SKL-25 | P3: Template-bundled skills | - | Pending |

**Coverage:** 25 total, 0 mapped to tasks, 25 unmapped ⚠️ (expected — design not started)

---

## Success Criteria

- [ ] The `invoice-layout` independent test passes on the eval reference models, with the load visible in the timeline.
- [ ] 100% of edit/delete attempts on built-ins are rejected by the server (API test), independent of the UI.
- [ ] With no enabled skills, the system prompt has no skills section and the model's tool list has no `load_skill` (regression test; the core prompt text itself changes once, to replace the stale `load_knowledge` line).
- [ ] The CI lint rejects a deliberately broken built-in.
- [ ] The eval report shows skill hit rate and unexpected-load rate per model, recorded in `design.md` as the baseline.
