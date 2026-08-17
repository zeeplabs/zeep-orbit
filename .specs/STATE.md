# STATE

## Decisions

- **AD-001**: `mcp-read-only-tools` implemented via 2 sub-agent batches (T1-T7, T8-T15) + fresh Verifier, all on `develop`, no push. 15 atomic commits (`6df417d`..`230450b`) + 1 doc-closure commit (`bfa33d8`). PASS — spec-anchored check clean, 5/5 injected mutations killed. `.specs/features/mcp-read-only-tools/validation.md` has full evidence.

## Handoff

- **Feature**: `mcp-read-only-tools` — **DONE** (verified PASS, `validate_state.py` clean)
- **Phase / Task**: Complete. 15/15 tasks committed, validation.md written, spec.md traceability closed (MROT-01..11 all Verified)
- **Completed**: T1-T15 (all), plus post-Verifier doc fix (spec.md traceability table extended to MROT-11)
- **In-progress**: none
- **Next step**: Two independent items remain, neither started:
  1. Write `design.md` + `tasks.md` for `mcp-safe-mutation-tools` (only `spec.md` exists — covers `orbit_add_table_column`, `orbit_add_table_index`, `orbit_create_webhook`, `orbit_save_webhook_event_mapping`)
  2. Commit the Swagger cache-control fix (`CHANGELOG.md` + `internal/docs/handler.go`) — still uncommitted, deferred pending explicit user request
- **Blockers**: none
- **Uncommitted files**: `CHANGELOG.md` (Fixed entry for the docs cache bug), `internal/docs/handler.go` (`Cache-Control: no-store` in `HandleSpec`) — unrelated bugfix from earlier this session, not committed per "never commit unless asked"; `.specs/features/mcp-safe-mutation-tools/` has only `spec.md` (untracked design/tasks)
- **Branch**: `develop`
