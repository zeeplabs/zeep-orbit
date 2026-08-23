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

### L-007 - For navigation/UI-composition features, write e2e tests that click the real trigger (card button, tab trigger) instead of page.goto shortcuts — goto-only tests miss regressions in the actual wiring (nav target, tab switching, in-tab actions).
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `internal/dashboard/ui/e2e` · harmful: 0
- features: app-users-tab
- evidence: AUT-02,AUT-04,AUT-05 (internal/dashboard/ui/e2e)
- last seen: 2026-08-08T19:14:32Z

### L-008 - When a form field's clear action must send an explicit empty value (not omit the key), add a dedicated e2e test that clears the field and asserts the exact empty-value payload — don't rely on backend acceptance of empty values as proof the frontend actually sends them.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `internal/dashboard/ui/src/components/patterns/PhoneInput.tsx` · harmful: 0
- features: app-user-phone-mask
- evidence: AUT-05 (internal/dashboard/ui/src/components/patterns/PhoneInput.tsx)
- last seen: 2026-08-08T20:08:15Z

### L-009 - When an AC requires transactional-abort or unconditional-write behavior with no observable side effect difference, add a test that directly forces the exact scenario (mid-tx failure injection; resubmit-identical-payload) instead of relying on 'the code structurally can't do otherwise'.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · harmful: 0
- features: table-policy-edit
- evidence: AC5/AC8
- last seen: 2026-08-10T17:26:16Z

### L-010 - When a task's Done-when checklist lists fewer scenarios than the Test Coverage Matrix row above it, treat the matrix as authoritative and add the missing scenario to Done-when before implementing, not after.
- signal: `gate_fail` · recurrence: 1 feature(s) · harmful: 0
- features: table-policy-edit
- evidence: tasks.md Test Coverage Matrix line 23 vs T2 Done-when
- last seen: 2026-08-10T17:26:16Z

### L-011 - When a spec AC lists multiple parallel actions (e.g. create/edit/delete/rotate) that must all hit an audit log, verify each action actually has an implementation before marking the AC covered — a missing action is a silent scope drop, not a covered case.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `audit-log` · harmful: 0
- features: inbound-webhooks
- evidence: spec.md P3 AC3 / validation.md WEBHOOK-23 (audit-log)
- last seen: 2026-08-10T21:51:25Z

### L-012 - When a task's required e2e test is merged forward into a later task ('untestable until wired'), confirm the later task's test actually exercises the merged behavior (e.g. the click-to-expand interaction), not just the surrounding happy path.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `test-coverage` · harmful: 0
- features: inbound-webhooks
- evidence: spec.md P2 delivery-log AC2 / validation.md WEBHOOK-19 (test-coverage)
- last seen: 2026-08-10T21:51:25Z

### L-013 - When a store Row type has no json tags, never writeJSON it directly (or a slice of it) -- always map through a response DTO with explicit snake_case tags, and decode handler tests into that same DTO, not the internal Row type, or the test self-masks the exact PascalCase-vs-snake_case bug it should catch.
- signal: `gate_fail` · recurrence: 1 feature(s) · scope: `json-serialization` · harmful: 0
- features: inbound-webhooks
- evidence: internal/dashboard/webhooks_handler.go SaveEventMapping/ListEventMappings (json-serialization)
- last seen: 2026-08-11T12:16:01Z

### L-014 - When a helper is applied at several call sites, assert the behavior at every call site through the public entry point, not just the helper in isolation.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: rls-policy-mode
- evidence: internal/server/handler.go:278,:332 (sensor mutation 6) (routes)
- last seen: 2026-08-12T15:01:13Z

### L-015 - Cover every HTTP verb an acceptance criterion names, not only the read path that is easiest to assert.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: rls-policy-mode
- evidence: spec.md AC P1-2 + Edge Cases (UPDATE/DELETE deny) (routes)
- last seen: 2026-08-12T15:01:13Z

### L-016 - When the spec requires a clear error, assert the error message content, not just that the error is non-nil.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `validation` · harmful: 0
- features: rls-policy-mode
- evidence: internal/dashboard/handler_test.go:114; internal/provisioner/policy_test.go:650 (validation)
- last seen: 2026-08-12T15:01:13Z

### L-017 - Before writing an acceptance criterion that leans on an existing validation, confirm that validation actually exists in the code path it names.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `validation` · harmful: 0
- features: rls-policy-mode
- evidence: spec.md AC P2-2; internal/provisioner/policy.go:231-233 (global policyOperators allowlist, no per-type check) (validation)
- last seen: 2026-08-12T15:32:07Z

### L-018 - When a criterion promises unchanged generated output, assert the generated string itself, not only that behavior still passes.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `provisioner` · harmful: 0
- features: rls-policy-mode
- evidence: spec.md AC P1-6 / tasks.md T2 Done-when; no generated-SQL comparison test exists (provisioner)
- last seen: 2026-08-12T15:32:07Z

### L-019 - When migrating a modal's functionality into a new dedicated page, add e2e assertions for the new page's own surface (nav label, route, static content, copy actions) — don't assume the migrated flow's existing test covers it.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `dashboard-ui-e2e` · harmful: 0
- features: mcp-settings-page
- evidence: MCPUI-01..09 (spec.md P1/P2); validation.md AC table (dashboard-ui-e2e)
- last seen: 2026-08-15T23:38:47Z

### L-020 - A single happy-path e2e test that always has data in the list will never exercise empty-state or error-toast branches — add an explicit test case that drives the list to empty and one that forces a mutation failure.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `dashboard-ui-e2e` · harmful: 0
- features: mcp-settings-page
- evidence: MCPUI-13, MCPUI-14 (spec.md P3 edge cases); validation.md AC table (dashboard-ui-e2e)
- last seen: 2026-08-15T23:38:47Z

### L-021 - For any nav-item feature, add an e2e test that clicks the nav link and asserts the resulting URL, not just that the link is visible -- direct page.goto() in beforeEach hides route-path regressions in the nav config from ever being exercised.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `dashboard-ui/e2e` · harmful: 0
- features: mcp-settings-page
- evidence: internal/dashboard/ui/src/components/layout/nav.ts:46 (dashboard-ui/e2e)
- last seen: 2026-08-15T23:56:05Z

### L-022 - When an AC requires a copy-to-clipboard action plus a specific success-toast string, add a test that actually clicks the copy control and asserts the toast text -- reading the copied value from a data attribute without clicking the button leaves the exact bug class (wrong toast message) uncaught by any test.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `dashboard-ui/e2e` · harmful: 0
- features: mcp-settings-page
- evidence: MCPUI-06,MCPUI-09 (dashboard-ui/e2e)
- last seen: 2026-08-15T23:56:05Z

### L-023 - When a test loops the same assertion shape over multiple structurally-different formats (JSON vs TOML, etc.), verify the assertion against each format's real serialization instead of assuming the first format's shape generalizes.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `e2e-testing` · harmful: 0
- features: mcp-settings-page
- evidence: internal/dashboard/ui/e2e/personal-access-tokens.spec.ts:165 (e2e-testing)
- last seen: 2026-08-16T00:12:33Z

### L-024 - An assertion placed after an earlier assertion in the same test function is unverified until the test is actually run end-to-end once — a prior failure in the same test silently prevents later assertions from ever executing, and static code review cannot detect this.
- signal: `gate_fail` · recurrence: 1 feature(s) · scope: `e2e-testing` · harmful: 0
- features: mcp-settings-page
- evidence: internal/dashboard/ui/e2e/personal-access-tokens.spec.ts:165-177 (e2e-testing)
- last seen: 2026-08-16T00:12:33Z

### L-025 - When tasks.md needs more requirement IDs than spec.md's traceability table allocated (e.g. splitting one P3 story into per-tool IDs), extend spec.md's table in the same change instead of letting tasks.md invent IDs the spec never declared.
- signal: `spec_deviation` · recurrence: 1 feature(s) · scope: `.specs/features/*/spec.md` · harmful: 0
- features: mcp-read-only-tools
- evidence: spec.md:119-137 vs tasks.md T11-T15 (.specs/features/*/spec.md)
- last seen: 2026-08-17T18:19:39Z

### L-026 - When a fake/stub replaces a model or history-consuming dependency in a test, assert on the captured input payload (e.g. history/system-prompt content), not just the fake's canned output — otherwise a regression in payload assembly (dropped system prompt, truncated history) is invisible to the suite.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `internal/dashboard/ai_build_chat_handlers.go` · harmful: 0
- features: ai-build-chat
- evidence: AIBC-12 (internal/dashboard/ai_build_chat_handlers.go)
- last seen: 2026-08-23T17:03:42Z

## Quarantined (failed when applied - ignore)

A confirmed lesson that recurred alongside failure. Kept for the maintainer to review.

_none_
