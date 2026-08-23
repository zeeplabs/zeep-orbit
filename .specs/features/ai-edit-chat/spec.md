# AI Edit Chat Specification

## Problem Statement

`ai-build-chat` (shipped, not yet released) lets a user create a brand-new backend app via chat, but stops there — the chat has no way to change an app after it exists. Today, adding a column, an index, a foreign key, a new table, or flipping RLS/auth on an already-created app requires the manual dashboard form (`AppDetailsPage`/`TableCard`), even though the backend already exposes safe, RBAC-gated handlers for most of these operations (`AddTableColumnForUser`, `AddTableIndexForUser`, `CreateAppTableForUser`, `UpdateTableRLSModeForUser`). This spec extends the AI chat to cover incremental edits on an existing app, reusing those handlers instead of inventing new mutation logic.

## Goals

- [ ] User can open an "Edit with AI" chat scoped to one existing app and describe a change in natural language.
- [ ] Each change the AI proposes is applied immediately on user confirmation — one operation per confirmation, never a batched multi-step plan.
- [ ] Every mutation reuses an existing `*ForUser` handler (or a new one added in this spec, `UpdateAppForUser`) — no new schema-mutation logic is written for this feature.
- [ ] Chat session persists across drawer close/reopen, scoped per (user, app) — matches the persistence guarantee already shipped for `ai-build-chat`.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Rename column, alter column type, drop column, drop index | No safe `*ForUser` handler exists for any of these today (only the internal provisioner supports rename/type-change, with no RBAC/audit-parity wrapper; drop-column/drop-index have zero implementation anywhere). Building them is materially riskier (destructive, can lose data or fail mid-migration) and was explicitly declined for this round — see Assumptions. |
| Add/remove a foreign key on a column that already exists | `References` can only be set when a column is created (`AddTableColumnForUser` rejects existing columns outright). Adding an FK to an existing column requires new provisioner logic not present today. |
| Automatic RLS/`owner_id` retrofit across existing tables when app auth is toggled on | Known gap in the underlying platform (confirmed during investigation: `UpdateApp` only flips `auth_email_enabled`, never touches existing tables). Independent of this chat feature; tracked separately, not fixed here. |
| Batched multi-operation plans (propose N changes, confirm once) | Explicitly rejected in favor of one-operation-at-a-time confirmation — smaller blast radius per confirm, and avoids partial-failure handling across heterogeneous operation types. |
| Cross-app foreign keys | Already out of scope for the platform itself (`internal/config/types.go`), not something this chat feature could add. |
| RAG / semantic memory of past edits | Same MVP decision already made for `ai-build-chat`; not revisited here. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Which operations are in scope | Only operations with an existing (or trivially-added, following the established pattern) `*ForUser` handler: add table, add column, add index, add reference (only on a new column), set table RLS mode, toggle app auth | User chose "só o que já existe" over building new destructive-operation handlers | y |
| Entry point | New "Edit with AI" button on `AppDetailsPage`, session bound to that `app_id` from creation | User chose this over a single global drawer where the AI must guess/confirm the target app | y |
| Confirmation model | One operation proposed, confirmed, and applied at a time; session never reaches `completed`, only `abandoned` (restart) or stays `in_progress` | User chose this over a batched AppPlan-style multi-op confirm, to keep blast radius small and avoid partial-failure handling across heterogeneous ops | y |
| RLS retrofit gap | Out of scope; not fixed, not even surfaced with a warning in this feature | User explicitly chose "fora de escopo" over having the chat warn about the gap | y |
| Whether "create new table in existing app" is in scope | Yes, included — reuses `CreateAppTableForUser` unchanged | User confirmed | y |
| Whether app-level auth toggle is in scope | Yes, included — requires adding `UpdateAppForUser` (new, follows the established `*ForUser` pattern: RBAC + audit) and a new MCP tool `orbit_update_app` for REST/MCP parity | User confirmed, accepting the small new-backend-wiring cost | y |
| Confirm handler structure | A new, separate `EditChatConfirm` (and `EditChatTurn`, `GetEditChatSession`, `RestartEditChatSession`) in a new file, not a generalization of the existing `BuildChatConfirm` | User chose isolation over generalization, to avoid risking regression in the already-shipped-and-verified creation flow | y |
| Session/data model | Extend `ai_build_sessions` additively with `mode` (`create`/`edit`) and `target_app_id` (nullable, populated at session creation for edit mode), rather than a new table | Reuses already-tested owner-scoping/IDOR-guard/restart logic; additive migration is low-risk since the table has not yet reached production (`ai-build-chat` is still `[Unreleased]`) | y |
| Tool-calling schema shape | One specific OpenAI tool per operation type (`propose_add_table`, `propose_add_column`, `propose_add_index`, `propose_add_reference`, `propose_set_rls_mode`, `propose_toggle_auth`), each with its own JSON schema, instead of one generic tool with a discriminator field | Preserves per-operation required-field validation that OpenAI's function-calling schema gives for free; a generic tool would push that validation into hand-written Go code | y |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: Add a column to an existing table via chat ⭐ MVP

**User Story**: As a user who already created an app, I want to open a chat scoped to that app and ask the AI to add a new column to one of its tables, so that I don't need to open the manual form and know the exact validation rules myself.

**Why P1**: Smallest vertical slice that proves the whole loop — scoped entry point, persisted edit session, one-operation proposal, immediate confirm-and-apply, chat stays open for the next change. Every other operation in this spec reuses this exact loop with a different tool/handler.

**Acceptance Criteria**:

1. WHEN a user with write access to app X clicks "Edit with AI" on that app's details page THEN the system SHALL open a chat session scoped to app X, creating a new `mode=edit` session with `target_app_id=X` if none is `in_progress` for that (user, app) pair, or reloading the existing one if one is.
2. WHEN the user describes a column to add (e.g. "add an email column") THEN the AI SHALL call `propose_add_column` with a concrete table name, column name, and column type once it has enough information, asking clarifying questions first if it does not.
3. WHEN the user confirms a proposed `add_column` operation THEN the system SHALL call `AddTableColumnForUser` for that app/table with the proposed column, applying the change immediately, and the session SHALL remain `in_progress` after the operation completes.
4. IF the proposed column's name or type fails the same validation `AddTableColumnForUser` already enforces (bad identifier, disallowed type, duplicate column name) THEN the system SHALL surface that specific validation error in the chat and leave the session `in_progress` with the app unmodified.
5. IF the user lacks write access (`CanWrite()`) to app X THEN the system SHALL return an authorization error for every edit-chat endpoint scoped to X, and the "Edit with AI" entry point SHALL NOT be shown for that app in the UI.
6. The system SHALL log every applied edit-chat mutation with audit origin `ai_chat`, identical to the origin already used by `ai-build-chat`.

**Independent Test**: Open "Edit with AI" on an existing app, ask the AI to add a column, confirm, verify the column exists in the table (dashboard or direct query) and the chat is still open and usable for a follow-up request.

---

### P2: Add an index, a new table, or a relationship via chat

**User Story**: As a user editing an app via chat, I want to also ask for a new index, a brand-new table, or a foreign key on a column I'm creating, so the chat covers the common schema-growth operations, not just single columns.

**Why P2**: Same loop as P1, different tools/handlers — each is independently useful but the feature is still coherent without them if P1 alone shipped.

**Acceptance Criteria**:

1. WHEN the user asks for a new index (including a composite or unique index) on an existing table THEN the AI SHALL call `propose_add_index` with the table, index name, target columns, and uniqueness flag, and confirming it SHALL call `AddTableIndexForUser`.
2. WHEN the user asks for a brand-new table inside the already-open app THEN the AI SHALL call `propose_add_table` with a table name and its columns, and confirming it SHALL call `CreateAppTableForUser` for that app.
3. WHEN the user asks for a new column that references another table's row (a foreign key) THEN the AI SHALL call `propose_add_reference` with the new column's name/type and the target table/column, and confirming it SHALL call `AddTableColumnForUser` with `References` populated — never on a column that already exists.
4. IF the user asks to add a foreign key to a column that already exists THEN the system SHALL decline in chat and explain that this requires recreating the column, which this chat does not support.

**Independent Test**: In the same edit session as P1, ask for a new unique index on the column just added, confirm, verify the index exists; separately ask for a brand-new table with two columns, confirm, verify the table exists in the app.

---

### P3: Toggle table RLS mode and app-level auth via chat

**User Story**: As a user editing an app via chat, I want to also flip a table's RLS mode or the app's email/password auth toggle, so the chat covers app-level configuration changes too, not only schema growth.

**Why P3**: Least commonly needed of the three stories, and toggling auth requires new backend wiring (`UpdateAppForUser` + `orbit_update_app` MCP tool) rather than pure reuse — appropriately last in priority.

**Acceptance Criteria**:

1. WHEN the user asks to change a table's RLS mode THEN the AI SHALL call `propose_set_rls_mode` with the table and target mode, and confirming it SHALL call `UpdateTableRLSModeForUser`.
2. WHEN the user asks to enable or disable email/password auth for the app THEN the AI SHALL call `propose_toggle_auth` with the desired boolean, and confirming it SHALL call the new `UpdateAppForUser` handler.
3. The system SHALL expose `UpdateAppForUser` as an MCP tool (`orbit_update_app`) with the same RBAC and audit behavior as the REST/chat path, preserving the established REST/MCP parity pattern.

**Independent Test**: In an existing edit session, ask to enable auth for an app that has none, confirm, verify `auth_email_enabled` flipped; ask to change a table's RLS mode, confirm, verify the mode changed.

---

## Edge Cases

- IF the OpenAI call fails (network/API error) THEN the system SHALL show a generic chat error and leave the session `in_progress`, unchanged from the failure-handling behavior already shipped in `ai-build-chat`.
- IF the user asks about anything outside editing this specific app (general knowledge, unrelated products, prompt-injection attempts) THEN the AI SHALL decline and steer back, using the same off-topic guard already shipped in the `ai-build-chat` system prompt, ported to the edit-chat system prompt.
- IF the user clicks "Recomeçar" mid-conversation THEN the current edit session SHALL be marked `abandoned` and a new `in_progress` edit session SHALL be created for the same (user, app), without requiring any pending operation to be confirmed first.
- IF the user opens "Edit with AI" on app X while they already have an unrelated `in_progress` app-creation session (`mode=create`) open THEN both sessions SHALL coexist independently — session scoping is per (user, app, mode), not global to the user.
- WHEN a proposed operation references a table or column name that does not exist in app X's current schema THEN the system SHALL reject it with the same validation error the underlying handler already produces, without silently guessing or auto-creating the missing entity.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AIEC-01 | P1 | Verify | Verified |
| AIEC-02 | P1 | Verify | Verified |
| AIEC-03 | P1 | Verify | Verified |
| AIEC-04 | P1 | Verify | Verified |
| AIEC-05 | P1 | Verify | Verified |
| AIEC-06 | P1 | Verify | Verified |
| AIEC-07 | P2 | Verify | Verified |
| AIEC-08 | P2 | Verify | Verified |
| AIEC-09 | P2 | Verify | Verified |
| AIEC-10 | P2 | Verify | Verified |
| AIEC-11 | P3 | Verify | Verified |
| AIEC-12 | P3 | Verify | Verified |
| AIEC-13 | P3 | Verify | Verified |
| AIEC-14 | Edge case | Verify | Verified |
| AIEC-15 | Edge case | Verify | Verified |
| AIEC-16 | Edge case | Verify | Verified |
| AIEC-17 | Edge case | Verify | Verified |
| AIEC-18 | Edge case | Verify | Verified |

**ID format:** `AIEC-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 18 total, 18 mapped to this spec's ACs, 0 unmapped.

---

## Success Criteria

- [ ] User can add a column to an existing app's table entirely through chat, without opening the manual form.
- [ ] User can add an index, a new table, and a foreign-key-bearing new column through the same chat loop.
- [ ] User can toggle a table's RLS mode and the app's auth setting through chat.
- [ ] Every mutation triggered by the edit chat is indistinguishable, in audit log and RBAC enforcement, from the same mutation made through the manual dashboard form or MCP.
- [ ] Zero new schema-mutation logic written — every operation calls an existing `*ForUser` handler, except `UpdateAppForUser` which follows the identical established pattern.
