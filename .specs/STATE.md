# STATE

## Decisions

- **AD-001**: `mcp-read-only-tools` implemented via 2 sub-agent batches (T1-T7, T8-T15) + fresh Verifier, all on `develop`, no push. 15 atomic commits (`6df417d`..`230450b`) + 1 doc-closure commit (`bfa33d8`). PASS — spec-anchored check clean, 5/5 injected mutations killed. `.specs/features/mcp-read-only-tools/validation.md` has full evidence.
- **AD-002**: `mcp-safe-mutation-tools` implemented inline (single ~8-task batch, no sub-agents) + fresh Verifier, all on `develop`, no push. 8 atomic commits (`4b54100`..`8e52c1c`) + doc commits (design/tasks approval, 2 independent Verifier passes, spec closure). PASS — 11/11 ACs spec-anchored, 5/5 injected mutations killed. New Handler methods (`AddTableColumnForUser`, `AddTableIndexForUser`, `CreateWebhookForUser`, `SaveEventMappingForUser`) are MCP-tool-only — no new REST HTTP routes wired, a real divergence from the original spec's Assumptions table, corrected during closure. `.specs/features/mcp-safe-mutation-tools/validation.md` has full evidence. Both minor gaps the second Verifier pass flagged (FK-cycle detection via the new incremental column path; superadmin/`CanReadAnyApp` edge case for the 4 new tools) were closed with 5 additional tests, gate re-run clean.

## Handoff

- **Feature**: `mcp-safe-mutation-tools` — **DONE** (verified PASS, gaps closed, `validate_state.py` clean)
- **Phase / Task**: Complete. 8/8 tasks committed, validation.md written (merged from 2 independent Verifier passes), spec.md traceability closed (MSMT-01..11 all Verified), REST-route assumption corrected, both flagged test-coverage gaps closed
- **Completed**: T1-T8 (all), post-Verifier doc fixes, 5 gap-closing tests (`TestAddTableColumnForUser_ReferenceCompletingCycleRejected`, `TestAddTableColumnForUser_SuperadminBypassesMembership`, `TestAddTableIndexForUser_SuperadminBypassesMembership`, `TestCreateWebhookForUser_SuperadminBypassesMembership`, `TestSaveEventMappingForUser_SuperadminBypassesMembership`)
- **In-progress**: none
- **Next step**: Only one item remains open across both MCP-tools features, not blocking:
  1. Commit the Swagger cache-control fix — turns out this was already committed (bundled into `b59c924`, an oddly-titled squashed commit); no action needed, the earlier "uncommitted" note in this file was stale
- **Blockers**: none
- **Uncommitted files**: none — working tree clean, `develop` matches `origin/develop` (confirmed via `git fetch`; the push itself was not performed by this agent in any visible turn — flagged to the user, cause unconfirmed)
- **Branch**: `develop`
