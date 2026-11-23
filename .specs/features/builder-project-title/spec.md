# Builder project title and backend name

**Status**: Draft 2026-11-23. Decisions confirmed by the maintainer the same day.

## Problem

The session title is the first words of the user's message ("Quero uma lista de pedid..."), and the new backend's technical name is derived from that title plus a hex suffix (`quero-uma-lista-de-pedido-8382e0`). The sidebar and the backend dropdown fill up with truncated sentences. Facts read from the code: the backend is created together with the session, before any plan exists (`builder_handlers.go` `CreateSession`); an app's name is the Postgres schema key and cannot be renamed (`apps_store.go`, `handler.go`); there is no endpoint to rename a session.

## Decisions

1. **Backend name**: generated at session creation, together with the title, by one short model call; fallback to a cleaned heuristic when the call fails or times out. The backend name does not follow later title edits (it is a schema key).
2. **Title**: model-generated at creation, refined by the plan (`title` field of `propose_build_plan`), editable by the user at any time. A manual edit is never overwritten.

## Requirements

- **PT-01** `POST /api/builder/sessions` accepts `prompt` (the user's first message, capped). When `title` is empty and `prompt` is present, the server generates `{title, slug}`. An explicit `title` from the caller is kept as is and also feeds the backend name.
- **PT-02** The generated title is at most 60 characters, in the language of the prompt, no quotes or trailing punctuation. The slug is ASCII kebab, at most 20 characters, and goes through `deriveBuilderAppName`, so the final name is valid and unique (`<slug>-<6 hex>`).
- **PT-03** The call has a short timeout (4 s) and a small output cap. Any failure (no provider, timeout, bad JSON, empty fields) falls back to a heuristic: first 5 words at a word boundary for the title, same words for the slug. Session creation never fails because of it.
- **PT-04** `PATCH /api/builder/sessions/{id}` with `{title}` renames the session (owner only, `cleanBuilderTitle`, 1 to 120 runes, English error strings) and sets `title_locked`.
- **PT-05** `propose_build_plan` takes a required `title` (short project name, same rules as PT-02). On plan approval the session title becomes the plan title unless `title_locked`. `ParsePlan` stays tolerant of stored briefs without it; the tool handler requires it.
- **PT-06** The planner prompt says to choose the title from the project and to ask the user for a name only when the project is ambiguous or the user asked to name it.
- **PT-07** UI: the builder header shows the title and renames it inline (Enter saves, Escape cancels); the plan card shows the proposed title and lets the user edit it before approving; the chat home sends `prompt` instead of a truncated `title`. Strings in `en.json` and `pt-BR.json`.
- **PT-08** The title call is not counted in a session's usage (no session exists yet); it is logged with its token counts. `[to confirm]` whether the cost should be attributed somewhere.

## Out of scope

Renaming the backend app, an app display-name column, changing existing sessions' titles.

## Risks

- A model call on the create path adds latency (bounded by the 4 s timeout) and uses the builder's provider; with no provider configured the heuristic runs.
- The prompt is user text sent to the model: it is capped and treated as data, the reply is parsed as JSON and sanitized, never executed.
- New nullable-free column `title_locked` with a default: additive, no backfill needed.
