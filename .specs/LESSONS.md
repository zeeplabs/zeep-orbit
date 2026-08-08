# LESSONS - auto-maintained by scripts/lessons.py

> Machine-owned. Do NOT hand-edit. Changes are overwritten on the next `lessons.py` write.
> Canonical state lives in `.specs/lessons.json`. Edit lessons only via the script.
> promote_threshold=2 distinct features · window_days=45 · quarantine_threshold=2

## Confirmed (load these at Specify/Design)

Corroborated across multiple features. Safe to apply as guidance.

_none_

## Candidates (under observation - do NOT load as guidance yet)

Seen once or not yet corroborated. Tracked, not trusted.

### L-001 - When an AC says a login/auth flow embeds a DB-read value in the issued JWT, write the test against the actual login/OAuth handler and decode the returned token — a unit test that calls IssueJWT directly with the value as a literal argument does not prove the handler wiring.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `internal/auth` · harmful: 0
- features: end-user-row-policies
- evidence: ROWPOL-02 (internal/auth)
- last seen: 2026-08-07T17:45:14Z

### L-002 - When a spec claims a role/permission switch is unconditional for an entire request path (e.g. all of /{app}/...), grep every handler under that path for direct pool usage before verifying the AC — a sibling handler added before the feature (e.g. file/storage handlers) can silently keep using the owner role and violate the absolute wording even though the new code is correct.
- signal: `spec_deviation` · recurrence: 1 feature(s) · scope: `internal/server` · harmful: 0
- features: end-user-row-policies
- evidence: internal/server/storage_handler.go (internal/server)
- last seen: 2026-08-07T17:45:14Z

### L-003 - When a UI section is gated behind a feature flag/config toggle, add an e2e test with the flag off asserting the section is absent, not only the happy-path test with it on.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `frontend-e2e` · harmful: 0
- features: enduser-roles-config
- evidence: ROLECFG-08 - validation.md P1 AC8 (frontend-e2e)
- last seen: 2026-08-08T14:25:04Z

### L-004 - For any drawer/modal/dialog with Cancel and Confirm actions, add a test for the Cancel path asserting the underlying mutation was not called, not just the confirm path.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `frontend-e2e` · harmful: 0
- features: enduser-roles-config
- evidence: ROLECFG-14 - validation.md P2 AC6 (frontend-e2e)
- last seen: 2026-08-08T14:25:04Z

### L-005 - When a spec requires that a value outside the current configured options still displays correctly (an orphan value), add a test that seeds data with that out-of-list value before asserting the UI, not only values already inside the list.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `frontend-e2e` · harmful: 0
- features: enduser-roles-config
- evidence: ROLECFG-12/ROLECFG-16 - validation.md P2 AC4 / P3 AC2 (frontend-e2e)
- last seen: 2026-08-08T14:25:04Z

### L-006 - In Playwright e2e tests, a negative assertion (toHaveCount(0)) or a positive assertion on text already present before the action under test (toBeVisible on pre-existing DOM content) can pass vacuously if it resolves before an async render/refetch settles - assert on a stable post-action marker or await the network response instead of relying on the first poll of a negative/pre-existing-content check.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `dashboard-ui,e2e` · harmful: 0
- features: enduser-roles-config
- evidence: internal/dashboard/ui/e2e/enduser-roles.spec.ts:95,137-138 | ROLECFG-08, ROLECFG-14 (dashboard-ui,e2e)
- last seen: 2026-08-08T14:54:00Z

## Quarantined (failed when applied - ignore)

A confirmed lesson that recurred alongside failure. Kept for the maintainer to review.

_none_
