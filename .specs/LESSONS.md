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

## Quarantined (failed when applied - ignore)

A confirmed lesson that recurred alongside failure. Kept for the maintainer to review.

_none_
