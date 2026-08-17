# STATE

## Decisions

- **AD-001**: `mcp-read-only-tools` implemented via 2 sub-agent batches (T1-T7, T8-T15) + fresh Verifier, all on `develop`, no push. 15 atomic commits (`6df417d`..`230450b`) + 1 doc-closure commit (`bfa33d8`). PASS — spec-anchored check clean, 5/5 injected mutations killed. `.specs/features/mcp-read-only-tools/validation.md` has full evidence.
- **AD-002**: `mcp-safe-mutation-tools` implemented inline (single ~8-task batch, no sub-agents) + fresh Verifier, all on `develop`, no push. 8 atomic commits (`4b54100`..`8e52c1c`) + 3 doc commits (design/tasks approval, Verifier report, spec closure). PASS — 11/11 ACs spec-anchored, 5/5 injected mutations killed. New Handler methods (`AddTableColumnForUser`, `AddTableIndexForUser`, `CreateWebhookForUser`, `SaveEventMappingForUser`) are MCP-tool-only — no new REST HTTP routes wired, a real divergence from the original spec's Assumptions table, corrected during closure. `.specs/features/mcp-safe-mutation-tools/validation.md` has full evidence.

## Handoff

- **Feature**: `mcp-safe-mutation-tools` — **DONE** (verified PASS, `validate_state.py` clean)
- **Phase / Task**: Complete. 8/8 tasks committed, validation.md written, spec.md traceability closed (MSMT-01..11 all Verified), REST-route assumption corrected
- **Completed**: T1-T8 (all), plus post-Verifier doc fix
- **In-progress**: none
- **Next step**: Items open across both MCP-tools features (none blocking, all optional follow-ups):
  1. Commit the Swagger cache-control fix (`CHANGELOG.md` + `internal/docs/handler.go`) — still uncommitted, deferred pending explicit user request
  2. Minor test-coverage gaps flagged by the second Verifier pass on `mcp-safe-mutation-tools` (non-blocking, PASS stands): no dedicated test exercises `config.ValidateTables`' FK-cycle detector via `AddTableColumnForUser`'s new incremental path; no dedicated test covers the superadmin/`CanReadAnyApp` edge case for the 4 new mutation tools
- **Blockers**: none
- **Uncommitted files**: `CHANGELOG.md` (Fixed entry for the docs cache bug), `internal/docs/handler.go` (`Cache-Control: no-store` in `HandleSpec`) — unrelated bugfix from earlier this session, not committed per "never commit unless asked"
- **Branch**: `develop`
