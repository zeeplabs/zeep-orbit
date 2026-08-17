# STATE

## Decisions

(none yet)

## Handoff

- **Feature**: `mcp-read-only-tools` (`.specs/features/mcp-read-only-tools/`)
- **Phase / Task**: Tasks approved (gate clean, 0 errors / 7 acceptable warnings), about to start Execute — no task started yet
- **Completed**: none
- **In-progress** (file:line): none — paused before Execute began
- **Next step**: Ask the user whether to dispatch sub-agents per batch (Batch 1 = T1-T7, Batch 2 = T8-T15) or execute inline, then start T1 (`orbit_get_app` MCP tool, `internal/mcpserver/tools.go`)
- **Blockers**: none
- **Uncommitted files**: `CHANGELOG.md` (Fixed entry for the `/docs/{app}/openapi.json` cache bug), `internal/docs/handler.go` (added `Cache-Control: no-store` to `HandleSpec`) — both from an earlier, separate bugfix this session, not yet committed per "never commit unless asked"; `.specs/features/mcp-read-only-tools/` and `.specs/features/mcp-safe-mutation-tools/` (untracked, new spec directories — `mcp-safe-mutation-tools` has spec.md only, no design/tasks yet)
- **Branch**: `develop`
