---
name: orbit-usage
description: Use when the user wants to build, integrate, or debug anything that talks to a Zeep Orbit backend — calling its REST API, wiring a frontend to an Orbit app, using an Orbit MCP server's tools, or picking/using one of Orbit's official client SDKs (TypeScript, Python, Go, Rust, Java, PHP). Not for working on Orbit's own internal codebase (schema-per-app provisioning, dashboard backend, etc.) — that's a different, internal-only skill.
---

# Zeep Orbit — consumer skill

Zeep Orbit is a self-hosted BaaS (Backend-as-a-Service): app owners define tables/columns/policies through the Orbit **Dashboard** (or an AI agent driving Orbit's **MCP server**), and Orbit provisions a Postgres schema plus a REST API for that app automatically. This skill is for the *other side*: an app that already exists on Orbit, and you're writing the client (frontend, script, another backend service) that talks to it — or you're an agent with MCP access to an Orbit instance and need to use its tools correctly.

Do not guess endpoint shapes, tool names, or SDK method names — this repo's `references/` are sourced from the actual Orbit codebase. If something you need isn't covered here, say so rather than inventing an endpoint or parameter.

## Load a reference by what the task needs

| Task | Read |
|---|---|
| Calling the REST API directly (curl, fetch, any language without a client) | `references/rest-api.md` |
| Using an official client SDK (TS/Python/Go/Rust/Java/PHP) | `references/clients.md` |
| Wiring a frontend app to an Orbit backend app end-to-end | `references/frontend-integration.md` |
| You (the agent) have MCP tool access to an Orbit instance | `references/mcp-tools.md` |
| Something broke, or a save/update silently didn't take effect | `references/gotchas.md` |

## Core facts (true regardless of which reference you load)

- **Auth model**: every Orbit app exposes its own JWT-based end-user auth (login/signup) *and* the Dashboard issues **Personal Access Tokens (PATs)** for agent/MCP/admin access — these are different tokens for different purposes. Don't mix them up: end-user JWT authenticates as an app user against that app's data/RLS; a PAT authenticates as a human/agent against the Dashboard/MCP management API.
- **Column types** include `text`, `integer`, `bigint`, `numeric`, `boolean`, `uuid`, `timestamptz`, `jsonb`, `enum`. An `enum` column is backed by a Postgres `CHECK` constraint (not a native `ENUM` type) and its allowed values can only be set **at column creation** — changing a column's allowed values later requires a dedicated update path, not a generic "edit column" call. Never assume you can freely retype a column to/from `enum`.
- **Partial-update semantics**: many Orbit update endpoints merge on absent keys (if a field is omitted from the request body, the existing value is kept). If your integration needs to *clear* a field, you must send that field explicitly with an empty value — omitting it is read as "leave it alone."
- **Errors from the Orbit API are in English**, regardless of what language your product's UI is in. Translate/display on your side; don't expect localized error strings from Orbit itself.
- **Row-Level Security (RLS)**: a table's policies determine what an authenticated app-user JWT can read/write. If your client gets unexpected empty results or 403s, check the table's RLS mode/policies before assuming a client bug.

Everything else — exact endpoint paths, exact SDK method signatures, MCP tool names — lives in the references above so this file stays short and doesn't drift.
