---
name: orbit-internals
description: Use when working ON zeep-orbit's own codebase — the Go backend (schema-per-app provisioning, dashboard handlers, MCP server, REST server) or its dashboard UI. Not for building a client that talks to an already-provisioned Orbit app — that's the orbit-usage skill (separate, consumer-facing, lives in the zeeplabs/zeep-orbit-agent-skills repo).
---

# Zeep Orbit — internal codebase skill

Zeep Orbit is a self-hosted BaaS: a Go backend (`cmd/zeep`, `internal/*`) plus a React/Vite/TS dashboard (`internal/dashboard/ui`), embedded into the Go binary via `go:embed` at build time. Every app gets its own Postgres schema; the Dashboard (or its MCP server) is the only way to define apps/tables/columns/policies — there is no YAML config file to author, that flow was removed for good (AGENTS.md).

This file stays short. Load a reference by what the task touches — don't load all of them for a one-line fix.

## Load a reference by what the task needs

| Task | Read |
|---|---|
| Touching schema provisioning, RLS modes/policies, column types, or `internal/config`/`internal/provisioner` | `references/provisioning.md` |
| Adding/changing a dashboard endpoint, an MCP tool, or a REST route/query filter | `references/handlers-and-mcp.md` |
| Writing or running tests for any backend package | `references/testing.md` |
| Something looks like a repeat of a known past bug, or you're about to hardcode something that smells like an invariant | `references/gotchas.md` |

## Core facts (true regardless of which reference you load)

- **The only correct way to derive an app's Postgres schema name** is `strings.ReplaceAll(appName, "-", "_")` — no `"app_"` prefix, no other pattern. This exact bug shipped to production once (see AGENTS.md, `internal/dashboard/apps_store.go:451`). The transform is implemented in two places that don't share code — `schemaNameForDB` in `internal/dashboard/handler.go:2446` and an inline duplicate inside `Provisioner.Apply` (`internal/provisioner/provisioner.go:36`). Don't "fix" the duplication by having one call the other across the package boundary without checking whether that's actually safe to import — check `references/gotchas.md` first.
- **REST handlers and MCP tools are two thin transports over the same `*dashboard.Handler` methods** (the `*ForUser` functions — `CreateAppForUser`, `UpdateAppForUser`, `CreateAppTableForUser`, `UpdateTableRLSModeForUser`, `CreateTablePolicyForUser`, etc., all in `internal/dashboard`). Never duplicate validation/provisioning/audit logic in a new MCP tool or REST handler — call the existing `*ForUser` method, or add a new one that both transports can share.
- **API error strings are English-only, server-side errors are never leaked raw.** `internal/mcpserver` uses a fixed `errInternal` + server-side `internalErr` logging (`tools.go:38-60`); the same discipline applies to REST 500s (AGENTS.md §4). Typed errors safe to expose (`provisioner.ValidationError`, `provisioner.TypeChangeError`) are the exception, not the default.
- **Nothing here is stored in an in-memory map keyed by process.** This service runs multiple stateless replicas behind a non-sticky load balancer — PATs, sessions, and OAuth state are always resolved against Postgres or a signed stateless token (HMAC-SHA256 with `exp`), never cached in memory. The MCP transport itself runs in `Stateless: true` mode for the same reason (`internal/mcpserver/server.go:21-41`).
- **RLS is a fail-closed, one-way ratchet.** See `references/provisioning.md` for the four `rls` values and what each means before touching anything RLS-related.
